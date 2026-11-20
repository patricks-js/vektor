package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"vektor/internal/config"
	"vektor/internal/history"
	"vektor/internal/host"
	"vektor/internal/metrics"
	"vektor/internal/metrics/collector"
	"vektor/internal/metrics/tracker"
	"vektor/internal/observability"
)

// Deps bundles collaborators required by Scheduler.
type Deps struct {
	Config          config.Config
	Clock           metrics.Clock
	Logger          *slog.Logger
	Health          *observability.HealthTracker
	Registry        *host.Registry
	Discovery       *collector.Discovery
	Normalizer      *tracker.Normalizer
	FastCollectors  []metrics.Collector
	FSCollector     *collector.FilesystemCollector
	SensorCollector *collector.SensorCollector
	RawWriter       *history.RawWriter
	Maintenance     *history.MaintenanceRunner
	LiveHub         *LiveHub
	BootIDProvider  func() (string, error)
}

// Scheduler coordinates discovery, fast/slow collection, rate normalization, persistence,
// on-demand live streaming, rollups, and retention without unbounded goroutines.
type Scheduler struct {
	deps       Deps
	fastSem    chan struct{}
	inFlightMu sync.Mutex
	inFlight   map[string]bool

	liveNotifyCh chan struct{}
}

// New constructs a Scheduler and wires LiveHub on-demand wakeup hooks when LiveHub is provided.
func New(deps Deps) *Scheduler {
	if deps.Clock == nil {
		deps.Clock = metrics.NewRealClock()
	}
	if deps.Logger == nil {
		deps.Logger = observability.NewLogger(nil, slog.LevelInfo)
	}
	if deps.BootIDProvider == nil {
		deps.BootIDProvider = func() (string, error) {
			return collector.ReadBootID(deps.Config.ProcRoot)
		}
	}
	maxConc := deps.Config.MaxFastConcurrency
	if maxConc <= 0 {
		maxConc = 4
	}
	s := &Scheduler{
		deps:         deps,
		fastSem:      make(chan struct{}, maxConc),
		inFlight:     make(map[string]bool),
		liveNotifyCh: make(chan struct{}, 1),
	}
	if deps.LiveHub != nil {
		deps.LiveHub.SetHooks(s.signalLiveChange, s.signalLiveChange)
	}
	return s
}

func (s *Scheduler) signalLiveChange() {
	select {
	case s.liveNotifyCh <- struct{}{}:
	default:
	}
}

// Run executes initial discovery and then runs all scheduling loops until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) error {
	if err := s.RunDiscoveryOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
		s.deps.Logger.Warn("initial discovery completed with errors", slog.String("error", err.Error()))
	}

	var wg sync.WaitGroup
	wg.Add(6)

	go func() {
		defer wg.Done()
		s.runTickerLoop(ctx, s.deps.Config.PersistInterval, s.RunPersistOnce)
	}()
	go func() {
		defer wg.Done()
		s.runTickerLoop(ctx, s.deps.Config.DiscoveryInterval, s.RunDiscoveryOnce)
	}()
	go func() {
		defer wg.Done()
		if s.deps.FSCollector == nil {
			return
		}
		_ = s.RunSlowFSOnce(ctx)
		s.runTickerLoop(ctx, s.deps.Config.SlowFSInterval, s.RunSlowFSOnce)
	}()
	go func() {
		defer wg.Done()
		if s.deps.SensorCollector == nil || !s.deps.Config.IncludeSensors {
			return
		}
		_ = s.RunSensorsOnce(ctx)
		s.runTickerLoop(ctx, s.deps.Config.SensorInterval, s.RunSensorsOnce)
	}()
	go func() {
		defer wg.Done()
		s.runTickerLoop(ctx, s.deps.Config.RollupInterval, s.RunMaintenanceOnce)
	}()
	go func() {
		defer wg.Done()
		s.runLiveLoop(ctx)
	}()

	wg.Wait()
	return ctx.Err()
}

// RunOnce executes discovery, slow probes, fast collection, normalization, and raw persistence once.
func (s *Scheduler) RunOnce(ctx context.Context) error {
	var errs []error
	if err := s.RunDiscoveryOnce(ctx); err != nil {
		errs = append(errs, err)
	}
	if s.deps.FSCollector != nil {
		if err := s.RunSlowFSOnce(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if s.deps.SensorCollector != nil && s.deps.Config.IncludeSensors {
		if err := s.RunSensorsOnce(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if err := s.RunPersistOnce(ctx); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (s *Scheduler) runTickerLoop(ctx context.Context, interval time.Duration, fn func(context.Context) error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := fn(ctx); err != nil && !errors.Is(err, context.Canceled) {
				s.deps.Logger.Debug("scheduled cycle returned error", slog.String("error", err.Error()))
			}
		}
	}
}

func (s *Scheduler) runLiveLoop(ctx context.Context) {
	if s.deps.LiveHub == nil {
		return
	}
	var ticker *time.Ticker
	var tickCh <-chan time.Time
	active := false

	stopLive := func() {
		if !active {
			return
		}
		ticker.Stop()
		ticker = nil
		tickCh = nil
		active = false
		if s.deps.Normalizer != nil {
			s.deps.Normalizer.ClearProfile(metrics.ProfileLive)
		}
	}
	defer stopLive()

	syncState := func() {
		subs := s.deps.LiveHub.SubscriberCount()
		if subs > 0 && !active {
			ticker = time.NewTicker(s.deps.Config.LiveInterval)
			tickCh = ticker.C
			active = true
			// Seed initial live baseline immediately upon first subscriber.
			_ = s.RunLiveOnce(ctx)
		} else if subs == 0 && active {
			stopLive()
		}
	}

	syncState()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.liveNotifyCh:
			syncState()
		case <-tickCh:
			if s.deps.LiveHub.SubscriberCount() == 0 {
				stopLive()
				continue
			}
			_ = s.RunLiveOnce(ctx)
		}
	}
}

// RunDiscoveryOnce collects static inventory and reconciles discovered resources into Registry and Topology.
func (s *Scheduler) RunDiscoveryOnce(ctx context.Context) error {
	now := s.deps.Clock.Now()
	inv := host.CollectInventory(s.deps.Config.ProcRoot, s.deps.Config.EtcRoot, now)
	if s.deps.BootIDProvider != nil {
		if bid, err := s.deps.BootIDProvider(); err == nil && bid != "" {
			inv.BootID = bid
		}
	}
	if s.deps.Registry != nil {
		if err := s.deps.Registry.EnsureHost(ctx, inv, now.UnixMilli()); err != nil {
			return err
		}
	}
	if s.deps.Discovery != nil && s.deps.Registry != nil {
		err := s.deps.Discovery.ReconcileAll(ctx, s.deps.Registry, now.UnixMilli(), s.deps.Config.IncludePerCoreCPU, inv.CPUThreads)
		if s.deps.Normalizer != nil {
			s.deps.Normalizer.SetNICIncludeFunc(s.deps.Discovery.Topology().IncludeNICInHostAggregate)
		}
		return err
	}
	return nil
}

// RunSlowFSOnce probes local filesystems under SlowCollectorTimeout.
func (s *Scheduler) RunSlowFSOnce(ctx context.Context) error {
	if s.deps.FSCollector == nil {
		return nil
	}
	cCtx, cancel := context.WithTimeout(ctx, s.deps.Config.SlowCollectorTimeout)
	defer cancel()
	snap, err := s.deps.FSCollector.Collect(cCtx)
	for _, w := range snap.Warnings {
		s.deps.Logger.Warn("slow filesystem probe warning", slog.String("collector", "filesystem"), slog.String("warning", w.Error()))
		if s.deps.Health != nil {
			s.deps.Health.RecordCollectorError("filesystem")
		}
	}
	if err != nil && s.deps.Health != nil {
		s.deps.Health.RecordCollectorError("filesystem")
	}
	return err
}

// RunSensorsOnce probes hwmon temperature channels under SlowCollectorTimeout.
func (s *Scheduler) RunSensorsOnce(ctx context.Context) error {
	if s.deps.SensorCollector == nil {
		return nil
	}
	cCtx, cancel := context.WithTimeout(ctx, s.deps.Config.SlowCollectorTimeout)
	defer cancel()
	snap, err := s.deps.SensorCollector.Collect(cCtx)
	for _, w := range snap.Warnings {
		s.deps.Logger.Warn("sensor probe warning", slog.String("collector", "sensors"), slog.String("warning", w.Error()))
		if s.deps.Health != nil {
			s.deps.Health.RecordCollectorError("sensors")
		}
	}
	if err != nil && s.deps.Health != nil {
		s.deps.Health.RecordCollectorError("sensors")
	}
	return err
}

// RunPersistOnce runs fast collectors + cached slow snapshots, normalizes under ProfilePersist, and writes to SQLite.
func (s *Scheduler) RunPersistOnce(ctx context.Context) error {
	bootID := s.readBootID()
	snaps := s.runFastCollectors(ctx)
	if s.deps.FSCollector != nil {
		if fsSnap := s.deps.FSCollector.CachedSnapshot(); len(fsSnap.Samples) > 0 {
			snaps = append(snaps, fsSnap)
		}
	}
	if s.deps.SensorCollector != nil && s.deps.Config.IncludeSensors {
		if senSnap := s.deps.SensorCollector.CachedSnapshot(); len(senSnap.Samples) > 0 {
			snaps = append(snaps, senSnap)
		}
	}

	var allPoints []metrics.MetricPoint
	for _, snap := range snaps {
		if s.deps.Normalizer != nil {
			res := s.deps.Normalizer.Normalize(metrics.ProfilePersist, bootID, snap)
			s.handleWarnings(res.Warnings)
			allPoints = append(allPoints, res.Points...)
		}
	}
	if s.deps.Health != nil {
		s.deps.Health.RecordCollect(s.deps.Clock.Now())
	}

	if s.deps.RawWriter != nil && len(allPoints) > 0 {
		if _, err := s.deps.RawWriter.WriteBatch(ctx, allPoints); err != nil {
			return err
		}
	}
	return nil
}

// RunLiveOnce collects fast metrics, normalizes under ProfileLive, and publishes to LiveHub without writing SQLite.
func (s *Scheduler) RunLiveOnce(ctx context.Context) error {
	if s.deps.LiveHub == nil || s.deps.LiveHub.SubscriberCount() == 0 {
		return nil
	}
	bootID := s.readBootID()
	snaps := s.runFastCollectors(ctx)

	var allPoints []metrics.MetricPoint
	var warnMsgs []string
	for _, snap := range snaps {
		for _, w := range snap.Warnings {
			warnMsgs = append(warnMsgs, w.Error())
		}
		if s.deps.Normalizer != nil {
			res := s.deps.Normalizer.Normalize(metrics.ProfileLive, bootID, snap)
			s.handleWarnings(res.Warnings)
			for _, w := range res.Warnings {
				warnMsgs = append(warnMsgs, w.Error())
			}
			allPoints = append(allPoints, res.Points...)
		}
	}
	if s.deps.Health != nil {
		s.deps.Health.RecordCollect(s.deps.Clock.Now())
	}
	s.deps.LiveHub.Publish(LiveSnapshot{
		CollectedAt: s.deps.Clock.Now(),
		Points:      allPoints,
		Warnings:    warnMsgs,
	})
	return nil
}

// RunMaintenanceOnce executes Rollup -> Retention and evicts stale tracker baselines.
func (s *Scheduler) RunMaintenanceOnce(ctx context.Context) error {
	now := s.deps.Clock.Now()
	if s.deps.Normalizer != nil {
		s.deps.Normalizer.EvictStale(now, s.deps.Config.CounterEvictAfter)
	}
	if s.deps.Maintenance != nil {
		_, _, err := s.deps.Maintenance.RunCycle(ctx, now.UnixMilli())
		return err
	}
	return nil
}

func (s *Scheduler) readBootID() string {
	if s.deps.BootIDProvider == nil {
		return ""
	}
	id, err := s.deps.BootIDProvider()
	if err != nil {
		return ""
	}
	return id
}

func (s *Scheduler) handleWarnings(warnings []tracker.Warning) {
	for _, w := range warnings {
		if w.Code == tracker.WarningWallClockJump && s.deps.Health != nil {
			s.deps.Health.RecordWallClockJump()
		}
		s.deps.Logger.Warn("normalization warning",
			slog.String("code", string(w.Code)),
			slog.String("resource_id", w.ResourceID),
			slog.String("key", w.Key),
			slog.String("message", w.Message),
		)
	}
}

func (s *Scheduler) runFastCollectors(ctx context.Context) []metrics.RawSnapshot {
	if len(s.deps.FastCollectors) == 0 {
		return nil
	}

	type colResult struct {
		name string
		snap metrics.RawSnapshot
		err  error
		dur  time.Duration
	}

	timeout := s.deps.Config.CollectorTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	cycleCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resCh := make(chan colResult, len(s.deps.FastCollectors))
	launched := 0

	for _, col := range s.deps.FastCollectors {
		name := col.Name()
		if !s.tryAcquireInFlight(name) {
			s.deps.Logger.Warn("collector skipped because previous execution is still in flight",
				slog.String("collector", name),
			)
			if s.deps.Health != nil {
				s.deps.Health.RecordCollectorError(name)
			}
			continue
		}

		launched++
		go func(c metrics.Collector, colName string) {
			defer s.releaseInFlight(colName)

			select {
			case s.fastSem <- struct{}{}:
			case <-cycleCtx.Done():
				resCh <- colResult{name: colName, err: cycleCtx.Err()}
				return
			}
			defer func() { <-s.fastSem }()

			start := time.Now()
			snap, err := c.Collect(cycleCtx)
			resCh <- colResult{
				name: colName,
				snap: snap,
				err:  err,
				dur:  time.Since(start),
			}
		}(col, name)
	}

	var out []metrics.RawSnapshot
	received := 0
	for received < launched {
		select {
		case r := <-resCh:
			received++
			for _, w := range r.snap.Warnings {
				s.deps.Logger.Warn("collector partial warning",
					slog.String("collector", r.name),
					slog.String("warning", w.Error()),
				)
			}
			if r.err != nil {
				s.deps.Logger.Warn("collector failed",
					slog.String("collector", r.name),
					slog.Duration("duration", r.dur),
					slog.String("error", r.err.Error()),
				)
				if s.deps.Health != nil {
					s.deps.Health.RecordCollectorError(r.name)
				}
				// Include partial samples/CPUSamples even when a non-fatal error occurred if present.
				if len(r.snap.Samples) > 0 || len(r.snap.CPUSamples) > 0 {
					out = append(out, r.snap)
				}
				continue
			}
			out = append(out, r.snap)
		case <-cycleCtx.Done():
			s.deps.Logger.Warn("fast collector cycle timeout reached with remaining collectors still running",
				slog.Int("pending", launched-received),
			)
			return out
		}
	}
	return out
}

func (s *Scheduler) tryAcquireInFlight(name string) bool {
	s.inFlightMu.Lock()
	defer s.inFlightMu.Unlock()
	if s.inFlight[name] {
		return false
	}
	s.inFlight[name] = true
	return true
}

func (s *Scheduler) releaseInFlight(name string) {
	s.inFlightMu.Lock()
	delete(s.inFlight, name)
	s.inFlightMu.Unlock()
}
