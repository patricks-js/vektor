package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"vektor/internal/config"
	"vektor/internal/history"
	"vektor/internal/host"
	"vektor/internal/metrics"
	"vektor/internal/metrics/collector"
	"vektor/internal/metrics/tracker"
	"vektor/internal/observability"
)

const (
	maxWallClockSkew = 5 * time.Second
	clockRecovery    = 30 * time.Second
)

var ErrClockUntrusted = errors.New("wall clock untrusted")

// Deps bundles collaborators required by Scheduler.
type Deps struct {
	Config            config.Config
	Clock             metrics.Clock
	Logger            *slog.Logger
	Health            *observability.HealthTracker
	Registry          *host.Registry
	Discovery         *collector.Discovery
	Normalizer        *tracker.Normalizer
	FastCollectors    []metrics.Collector
	FSCollector       *collector.FilesystemCollector
	SensorCollector   *collector.SensorCollector
	RawWriter         *history.RawWriter
	Maintenance       *history.MaintenanceRunner
	LiveHub           *LiveHub
	BootIDProvider    func() (string, error)
	ClockSynchronized func() bool
	ClockFloor        time.Time
}

// Scheduler coordinates discovery, fast/slow collection, rate normalization, persistence,
// on-demand live streaming, rollups, and retention without unbounded goroutines.
type Scheduler struct {
	deps       Deps
	fastSem    chan struct{}
	inFlightMu sync.Mutex
	inFlight   map[string]*fastFlight

	discoveryMu      sync.Mutex
	clockMu          sync.Mutex
	clockWall        time.Time
	clockMono        time.Duration
	clockElapsed     func() time.Duration
	clockFloor       time.Time
	clockError       string
	clockStableSince time.Time
	clockStableMono  time.Duration
	bootID           atomic.Pointer[string]

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
	maxConc := deps.Config.MaxFastConcurrency
	if maxConc <= 0 {
		maxConc = 4
	}
	elapsed := deps.Clock.Monotonic
	if clock, ok := deps.Clock.(*metrics.RealClock); ok {
		elapsed = clock.Elapsed
		if deps.ClockSynchronized == nil {
			deps.ClockSynchronized = clock.Synchronized
		}
	}
	if deps.ClockSynchronized == nil {
		deps.ClockSynchronized = func() bool { return true }
	}
	s := &Scheduler{
		deps:         deps,
		fastSem:      make(chan struct{}, maxConc),
		inFlight:     make(map[string]*fastFlight),
		liveNotifyCh: make(chan struct{}, 1),
		clockWall:    deps.Clock.Now(),
		clockMono:    elapsed(),
		clockElapsed: elapsed,
		clockFloor:   deps.ClockFloor,
	}
	if deps.Discovery != nil && deps.Normalizer != nil {
		deps.Normalizer.SetNICIncludeFunc(deps.Discovery.Topology().IncludeNICInHostAggregate)
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
	initialDiscoveryDone := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(6)

	go func() {
		defer wg.Done()
		s.runTickerLoop(ctx, s.deps.Config.PersistInterval, s.RunPersistOnce)
	}()
	go func() {
		defer wg.Done()
		if err := s.RunDiscoveryOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			s.deps.Logger.Warn("initial discovery completed with errors", slog.String("error", err.Error()))
		}
		close(initialDiscoveryDone)
		s.runTickerLoop(ctx, s.deps.Config.DiscoveryInterval, s.RunDiscoveryOnce)
	}()
	go func() {
		defer wg.Done()
		if s.deps.FSCollector == nil {
			return
		}
		select {
		case <-initialDiscoveryDone:
		case <-ctx.Done():
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
		select {
		case <-initialDiscoveryDone:
		case <-ctx.Done():
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
	if err := ctx.Err(); err != nil {
		return err
	}
	if !s.discoveryMu.TryLock() {
		return errors.New("previous discovery is still in flight")
	}
	probeCtx, cancel := context.WithTimeout(ctx, s.collectorTimeout())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		err := s.discover(probeCtx)
		s.discoveryMu.Unlock()
		done <- err
	}()
	var err error
	select {
	case err = <-done:
	case <-probeCtx.Done():
		err = probeCtx.Err()
	}
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, ErrClockUntrusted) && s.deps.Health != nil {
		s.deps.Health.RecordCollectorErrorDetail("discovery", err)
	}
	return err
}

func (s *Scheduler) discover(ctx context.Context) error {
	inv := host.CollectInventory(s.deps.Config.ProcRoot, s.deps.Config.EtcRoot, s.deps.Clock.Now())
	if s.deps.BootIDProvider != nil {
		if bid, err := s.deps.BootIDProvider(); err == nil && bid != "" {
			inv.BootID = bid
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if inv.BootID != "" {
		s.bootID.Store(&inv.BootID)
	}
	now, err := s.trustedTime()
	if err != nil {
		return err
	}
	inv.UpdatedAtMs = now.UnixMilli()
	if s.deps.Registry != nil {
		if err := s.deps.Registry.EnsureHost(ctx, inv, now.UnixMilli()); err != nil {
			return err
		}
	}
	if s.deps.Discovery != nil && s.deps.Registry != nil {
		return s.deps.Discovery.ReconcileAll(ctx, s.deps.Registry, now.UnixMilli(), s.deps.Config.IncludePerCoreCPU)
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
			s.deps.Health.RecordCollectorErrorDetail("filesystem", w)
		}
	}
	if err != nil && s.deps.Health != nil {
		s.deps.Health.RecordCollectorErrorDetail("filesystem", err)
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
			s.deps.Health.RecordCollectorErrorDetail("sensors", w)
		}
	}
	if err != nil && s.deps.Health != nil {
		s.deps.Health.RecordCollectorErrorDetail("sensors", err)
	}
	return err
}

// RunPersistOnce runs fast collectors + cached slow snapshots, normalizes under ProfilePersist, and writes to SQLite.
func (s *Scheduler) RunPersistOnce(ctx context.Context) error {
	bootID := s.readBootID()
	snaps := s.runFastCollectors(ctx, metrics.ProfilePersist)
	now, err := s.trustedTime()
	if err != nil {
		if s.deps.Normalizer != nil {
			s.deps.Normalizer.ClearProfile(metrics.ProfilePersist)
		}
		return err
	}
	mono := s.deps.Clock.Monotonic()
	snaps = slices.DeleteFunc(snaps, func(snap metrics.RawSnapshot) bool {
		return !snap.CollectedAt.IsZero() && wallClockJump(snap.CollectedAt, now, mono-snap.Monotonic)
	})
	appendCached := func(snap metrics.RawSnapshot) {
		// A probe can finish while the wall clock is wrong and remain cached after it recovers.
		snap.Samples = slices.DeleteFunc(snap.Samples, func(sample metrics.RawSample) bool {
			return wallClockJump(sample.Time, now, mono-sample.Monotonic)
		})
		if len(snap.Samples) > 0 {
			snaps = append(snaps, snap)
		}
	}
	if s.deps.FSCollector != nil {
		appendCached(s.deps.FSCollector.CachedSnapshot())
	}
	if s.deps.SensorCollector != nil && s.deps.Config.IncludeSensors {
		appendCached(s.deps.SensorCollector.CachedSnapshot())
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
	snaps := s.runFastCollectors(ctx, metrics.ProfileLive)

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
	now, err := s.trustedTime()
	if err != nil {
		return err
	}
	if s.deps.Normalizer != nil {
		s.deps.Normalizer.EvictStale(now, s.deps.Config.CounterEvictAfter)
	}
	if s.deps.Maintenance != nil {
		_, _, err := s.deps.Maintenance.RunCycle(ctx, now.UnixMilli())
		return err
	}
	return nil
}

func (s *Scheduler) trustedTime() (time.Time, error) {
	s.clockMu.Lock()
	defer s.clockMu.Unlock()
	now, mono := s.deps.Clock.Now(), s.clockElapsed()
	jumped := wallClockJump(s.clockWall, now, mono-s.clockMono)
	s.clockWall, s.clockMono = now, mono

	if jumped && s.deps.Health != nil {
		s.deps.Health.RecordWallClockJump()
	}
	var reason string
	switch {
	case !s.deps.ClockSynchronized():
		reason = "Linux does not report a synchronized wall clock"
		s.clockStableSince = time.Time{}
	case now.Add(maxWallClockSkew).Before(s.clockFloor):
		reason = "wall clock is behind previously trusted history"
		s.clockStableSince = time.Time{}
	case jumped || s.clockError != "":
		if jumped || s.clockStableSince.IsZero() ||
			wallClockJump(s.clockStableSince, now, mono-s.clockStableMono) {
			s.clockStableSince, s.clockStableMono = now, mono
		}
		if mono-s.clockStableMono < clockRecovery {
			reason = "waiting for the synchronized wall clock to stabilize"
		}
	}
	if s.clockError != reason {
		if reason == "" {
			s.deps.Logger.Info("trusted wall clock restored")
		} else {
			s.deps.Logger.Warn("historical writes and maintenance paused", slog.String("reason", reason))
		}
		s.clockError = reason
		if s.deps.Health != nil {
			s.deps.Health.SetClockError(reason)
		}
	}
	if reason != "" {
		return time.Time{}, fmt.Errorf("%w: %s", ErrClockUntrusted, reason)
	}
	if now.After(s.clockFloor) {
		s.clockFloor = now
	}
	return now, nil
}

func wallClockJump(before, after time.Time, elapsed time.Duration) bool {
	offset := after.Sub(before) - elapsed
	return offset > maxWallClockSkew || offset < -maxWallClockSkew
}

func (s *Scheduler) readBootID() string {
	if id := s.bootID.Load(); id != nil {
		return *id
	}
	return ""
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

type fastResult struct {
	name string
	snap metrics.RawSnapshot
	err  error
	dur  time.Duration
}

type fastFlight struct {
	profile metrics.Profile
	done    chan struct{}
	result  fastResult
}

func (s *Scheduler) collectorTimeout() time.Duration {
	if s.deps.Config.CollectorTimeout <= 0 {
		return 5 * time.Second
	}
	return s.deps.Config.CollectorTimeout
}

func (s *Scheduler) runFastCollectors(ctx context.Context, profile metrics.Profile) []metrics.RawSnapshot {
	if len(s.deps.FastCollectors) == 0 {
		return nil
	}

	cycleCtx, cancel := context.WithTimeout(ctx, s.collectorTimeout())
	defer cancel()

	resCh := make(chan fastResult, len(s.deps.FastCollectors))
	launched := 0

	for _, col := range s.deps.FastCollectors {
		name := col.Name()
		flight := s.startOrShareCollector(ctx, col, name, profile)
		if flight == nil {
			s.deps.Logger.Warn("collector skipped because previous execution is still in flight",
				slog.String("collector", name),
			)
			if s.deps.Health != nil {
				s.deps.Health.RecordCollectorErrorDetail(name, errors.New("previous execution still in flight"))
			}
			continue
		}

		launched++
		go func() {
			select {
			case <-flight.done:
				resCh <- flight.result
			case <-cycleCtx.Done():
			}
		}()
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
					s.deps.Health.RecordCollectorErrorDetail(r.name, r.err)
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

func (s *Scheduler) startOrShareCollector(ctx context.Context, col metrics.Collector, name string, profile metrics.Profile) *fastFlight {
	s.inFlightMu.Lock()
	defer s.inFlightMu.Unlock()
	if flight := s.inFlight[name]; flight != nil {
		if flight.profile == profile {
			return nil
		}
		return flight
	}

	flight := &fastFlight{profile: profile, done: make(chan struct{}), result: fastResult{name: name}}
	s.inFlight[name] = flight
	go func() {
		defer func() {
			s.inFlightMu.Lock()
			delete(s.inFlight, name)
			close(flight.done)
			s.inFlightMu.Unlock()
		}()

		probeCtx, cancel := context.WithTimeout(ctx, s.collectorTimeout())
		defer cancel()
		select {
		case s.fastSem <- struct{}{}:
			defer func() { <-s.fastSem }()
		case <-probeCtx.Done():
			flight.result.err = probeCtx.Err()
			return
		}
		start := time.Now()
		flight.result.snap, flight.result.err = col.Collect(probeCtx)
		flight.result.dur = time.Since(start)
	}()
	return flight
}
