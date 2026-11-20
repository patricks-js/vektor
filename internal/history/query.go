package history

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"vektor/internal/config"
	"vektor/internal/database"
	"vektor/internal/host"
	"vektor/internal/metrics"
	"vektor/internal/observability"
)

const (
	defaultMaxQueryPoints = 600
	hardMaxQueryPoints    = 2000
)

// SeriesQuery defines parameters for querying aggregated time-series buckets.
type SeriesQuery struct {
	ResourceID string
	MetricKey  string
	FromMs     int64
	ToMs       int64
	Step       string // "", "auto", "30", "300", "3600" (or "30s", "5m", "1h")
	MaxPoints  int
}

// SeriesPoint represents one non-interpolated historical bucket in a SeriesResult.
type SeriesPoint struct {
	BucketStartMs   int64   `json:"bucket_start_ms"`
	Avg             float64 `json:"avg"`
	Min             float64 `json:"min"`
	Max             float64 `json:"max"`
	Last            float64 `json:"last"`
	SampleCount     int64   `json:"sample_count"`
	ExpectedSamples int64   `json:"expected_samples"`
	Coverage        float64 `json:"coverage"`
}

// SeriesResult matches the Section 8 REST contract payload for historical time series.
type SeriesResult struct {
	ResourceID  string        `json:"resource_id"`
	MetricKey   string        `json:"metric_key"`
	Unit        string        `json:"unit"`
	ResolutionS int           `json:"resolution_s"`
	FromMs      int64         `json:"from_ms"`
	ToMs        int64         `json:"to_ms"`
	Points      []SeriesPoint `json:"points"`
}

// HostResponse combines the host resource identity and decoded inventory.
type HostResponse struct {
	Resource  host.Resource  `json:"resource"`
	Inventory host.Inventory `json:"inventory"`
}

// HealthResponse merges runtime telemetry, live DB ping status, and SQLite disk footprint.
type HealthResponse struct {
	observability.HealthSnapshot
	DBPingOK    bool   `json:"db_ping_ok"`
	DBPingError string `json:"db_ping_error,omitempty"`
	DBBytes     int64  `json:"db_bytes"`
	WALBytes    int64  `json:"wal_bytes"`
	SHMBytes    int64  `json:"shm_bytes"`
}

// QueryService serves read-only host, resource, metric series, and health queries.
type QueryService struct {
	db             *database.DB
	registry       *host.Registry
	health         *observability.HealthTracker
	clock          metrics.Clock
	persistStepS   int64
	rawRetentionMs int64
}

// NewQueryService constructs a QueryService using the read-only SQLite pool.
func NewQueryService(
	db *database.DB,
	reg *host.Registry,
	cfg config.Config,
	health *observability.HealthTracker,
	clock metrics.Clock,
) *QueryService {
	if clock == nil {
		clock = metrics.NewRealClock()
	}
	persistS := int64(cfg.PersistInterval.Seconds())
	if persistS <= 0 {
		persistS = ResolutionRawS
	}
	return &QueryService{
		db:             db,
		registry:       reg,
		health:         health,
		clock:          clock,
		persistStepS:   persistS,
		rawRetentionMs: cfg.RawRetention.Milliseconds(),
	}
}

// GetHost returns the local host resource and its latest inventory metadata.
func (s *QueryService) GetHost(ctx context.Context) (HostResponse, error) {
	res, inv, err := s.registry.GetHost(ctx)
	if err != nil {
		return HostResponse{}, err
	}
	return HostResponse{
		Resource:  res,
		Inventory: inv,
	}, nil
}

// ListResources lists active (or all) tracked resources optionally filtered by kind.
func (s *QueryService) ListResources(ctx context.Context, kind string, includeInactive bool) ([]host.Resource, error) {
	return s.registry.List(ctx, kind, includeInactive)
}

// QuerySeries returns historical metric buckets without interpolating or zero-filling gaps.
func (s *QueryService) QuerySeries(ctx context.Context, q SeriesQuery) (SeriesResult, error) {
	if strings.TrimSpace(q.ResourceID) == "" {
		return SeriesResult{}, errors.New("resource_id must not be empty")
	}
	def, ok := metrics.LookupMetric(q.MetricKey)
	if !ok {
		return SeriesResult{}, fmt.Errorf("unknown metric_key %q", q.MetricKey)
	}
	if q.ToMs <= q.FromMs {
		return SeriesResult{}, fmt.Errorf("invalid range: to_ms (%d) must be > from_ms (%d)", q.ToMs, q.FromMs)
	}

	maxPoints := q.MaxPoints
	if maxPoints <= 0 {
		maxPoints = defaultMaxQueryPoints
	}
	if maxPoints > hardMaxQueryPoints {
		maxPoints = hardMaxQueryPoints
	}

	resS, err := s.resolveStep(ctx, q, maxPoints)
	if err != nil {
		return SeriesResult{}, err
	}

	resMs := int64(resS) * 1000
	alignedFrom := metrics.AlignBucketMs(q.FromMs, resMs)

	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT bucket_start_ms, sample_count, value_sum, value_min, value_max, value_last
		FROM metric_buckets
		WHERE resource_id = ?
		  AND metric_key = ?
		  AND resolution_s = ?
		  AND bucket_start_ms >= ?
		  AND bucket_start_ms < ?
		ORDER BY bucket_start_ms ASC
	`, q.ResourceID, q.MetricKey, resS, alignedFrom, q.ToMs)
	if err != nil {
		return SeriesResult{}, fmt.Errorf("query metric_buckets: %w", err)
	}
	defer rows.Close()

	expected := int64(resS) / s.persistStepS
	if expected < 1 {
		expected = 1
	}

	points := make([]SeriesPoint, 0)
	for rows.Next() {
		var bucketStartMs, count int64
		var sum, minV, maxV, lastV float64
		if err := rows.Scan(&bucketStartMs, &count, &sum, &minV, &maxV, &lastV); err != nil {
			return SeriesResult{}, err
		}
		if count <= 0 {
			continue
		}
		cov := math.Min(1.0, float64(count)/float64(expected))
		points = append(points, SeriesPoint{
			BucketStartMs:   bucketStartMs,
			Avg:             sum / float64(count),
			Min:             minV,
			Max:             maxV,
			Last:            lastV,
			SampleCount:     count,
			ExpectedSamples: expected,
			Coverage:        cov,
		})
	}
	if err := rows.Err(); err != nil {
		return SeriesResult{}, err
	}

	return SeriesResult{
		ResourceID:  q.ResourceID,
		MetricKey:   q.MetricKey,
		Unit:        def.Unit,
		ResolutionS: resS,
		FromMs:      q.FromMs,
		ToMs:        q.ToMs,
		Points:      points,
	}, nil
}

func (s *QueryService) resolveStep(ctx context.Context, q SeriesQuery, maxPoints int) (int, error) {
	step := strings.ToLower(strings.TrimSpace(q.Step))
	switch step {
	case "", "auto":
		return s.autoSelectResolution(ctx, q, maxPoints)
	case "30", "30s":
		return ResolutionRawS, nil
	case "300", "300s", "5m":
		return ResolutionMediumS, nil
	case "3600", "3600s", "1h":
		return ResolutionLongS, nil
	default:
		if v, err := strconv.Atoi(step); err == nil {
			if _, err := resolutionDurationMs(v); err == nil {
				return v, nil
			}
		}
		return 0, fmt.Errorf("unsupported step %q (expected auto, 30, 300, or 3600)", q.Step)
	}
}

func (s *QueryService) autoSelectResolution(ctx context.Context, q SeriesQuery, maxPoints int) (int, error) {
	tiers := []int{ResolutionRawS, ResolutionMediumS, ResolutionLongS}

	type tierInfo struct {
		resS    int
		hasMin  bool
		minMs   int64
		fitsMax bool
	}

	nowMs := s.clock.Now().UnixMilli()
	rawHorizonMs := nowMs - s.rawRetentionMs
	spanMs := q.ToMs - q.FromMs

	infos := make([]tierInfo, len(tiers))
	for i, resS := range tiers {
		resMs := int64(resS) * 1000
		estBuckets := spanMs/resMs + 1
		fits := estBuckets <= int64(maxPoints)

		minMs, hasMin, err := s.earliestBucket(ctx, q.ResourceID, q.MetricKey, resS)
		if err != nil {
			return 0, err
		}
		infos[i] = tierInfo{
			resS:    resS,
			hasMin:  hasMin,
			minMs:   minMs,
			fitsMax: fits,
		}
	}

	// Never pretend 30s resolution covers a query starting prior to raw retention when a coarser tier fits
	// and holds data for the earlier portion of the window.
	for i, info := range infos {
		if !info.fitsMax {
			continue
		}
		if info.resS == ResolutionRawS && s.rawRetentionMs > 0 && q.FromMs < rawHorizonMs {
			if infos[1].hasMin || infos[2].hasMin {
				continue
			}
		}
		// If a coarser tier has earlier data than this finer tier (meaning finer data expired/rolled off),
		// fall back to the coarser tier rather than hiding historical coverage.
		finerCoveredFrom := !info.hasMin || info.minMs <= q.FromMs+int64(info.resS)*1000
		if !finerCoveredFrom {
			coarserHasEarlier := false
			for j := i + 1; j < len(infos); j++ {
				if infos[j].hasMin && (!info.hasMin || infos[j].minMs < info.minMs) {
					coarserHasEarlier = true
					break
				}
			}
			if coarserHasEarlier {
				continue
			}
		}
		return info.resS, nil
	}

	return ResolutionLongS, nil
}

func (s *QueryService) earliestBucket(ctx context.Context, resID, metricKey string, resS int) (int64, bool, error) {
	var minMs sql.NullInt64
	err := s.db.Reader().QueryRowContext(ctx, `
		SELECT MIN(bucket_start_ms)
		FROM metric_buckets
		WHERE resource_id = ? AND metric_key = ? AND resolution_s = ?
	`, resID, metricKey, resS).Scan(&minMs)
	if err != nil {
		return 0, false, err
	}
	if !minMs.Valid {
		return 0, false, nil
	}
	return minMs.Int64, true, nil
}

// GetHealth checks live DB connectivity, reads file sizes, and merges HealthTracker counters.
func (s *QueryService) GetHealth(ctx context.Context) (HealthResponse, error) {
	var snap observability.HealthSnapshot
	if s.health != nil {
		snap = s.health.Snapshot()
	} else {
		snap = observability.HealthSnapshot{
			Status: observability.StatusOK,
			DBOK:   true,
		}
	}

	resp := HealthResponse{
		HealthSnapshot: snap,
		DBPingOK:       true,
	}

	if err := s.db.Ping(ctx); err != nil {
		resp.DBPingOK = false
		resp.DBPingError = err.Error()
		resp.DBOK = false
		resp.Status = observability.StatusDegraded
	}

	if dbB, walB, shmB, err := s.db.FileSizes(); err == nil {
		resp.DBBytes = dbB
		resp.WALBytes = walB
		resp.SHMBytes = shmB
	}

	return resp, nil
}
