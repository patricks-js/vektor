package history

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"vektor/internal/config"
	"vektor/internal/database"
	"vektor/internal/host"
	"vektor/internal/metrics"
	"vektor/internal/observability"
)

const (
	DefaultMaxQueryPoints = 600
	MaxQueryPoints        = 2000
)

// ErrPointLimit indicates that a populated historical level cannot fit the point budget.
var ErrPointLimit = errors.New("historical resolution exceeds point limit")

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
	db       *database.DB
	registry *host.Registry
	health   *observability.HealthTracker
	config   config.Config
}

// NewQueryService constructs a QueryService using the read-only SQLite pool.
func NewQueryService(
	db *database.DB,
	reg *host.Registry,
	cfg config.Config,
	health *observability.HealthTracker,
) *QueryService {
	return &QueryService{
		db: db, registry: reg, health: health, config: cfg,
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

func (s *QueryService) GetResource(ctx context.Context, id string) (host.Resource, error) {
	return s.registry.Get(ctx, id)
}

func (s *QueryService) ListResourcePage(ctx context.Context, kind string, active *bool, after string, limit int) ([]host.Resource, error) {
	return s.registry.ListPage(ctx, kind, active, after, limit)
}

// ClockFloor returns the most recent durable timestamp set by the historical pipeline.
func (s *QueryService) ClockFloor(ctx context.Context) (time.Time, error) {
	var floor sql.NullInt64
	err := s.db.Reader().QueryRowContext(ctx, `
		SELECT MAX(ts) FROM (
			SELECT MAX(last_sample_ms) AS ts FROM metric_buckets WHERE resolution_s = 30
			  AND bucket_start_ms = (SELECT MAX(bucket_start_ms) FROM metric_buckets WHERE resolution_s = 30)
			UNION ALL SELECT MAX(closed_through_ms) FROM rollup_state
		)
	`).Scan(&floor)
	if err != nil || !floor.Valid {
		return time.Time{}, err
	}
	return time.UnixMilli(floor.Int64), nil
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
		maxPoints = DefaultMaxQueryPoints
	}
	if maxPoints > MaxQueryPoints {
		maxPoints = MaxQueryPoints
	}

	tx, err := s.db.Reader().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return SeriesResult{}, err
	}
	defer tx.Rollback()

	resS, err := resolveStep(ctx, tx, q, maxPoints)
	if err != nil {
		return SeriesResult{}, err
	}

	resMs := int64(resS) * 1000
	alignedFrom := metrics.AlignBucketMs(q.FromMs, resMs)

	rows, err := tx.QueryContext(ctx, `
		SELECT bucket_start_ms, sample_count, value_sum, value_min, value_max, value_last
		FROM metric_buckets
		WHERE resource_id = ?
		  AND metric_key = ?
		  AND resolution_s = ?
		  AND bucket_start_ms >= ?
		  AND bucket_start_ms < ?
		ORDER BY bucket_start_ms ASC
	LIMIT ?
	`, q.ResourceID, q.MetricKey, resS, alignedFrom, q.ToMs, maxPoints+1)
	if err != nil {
		return SeriesResult{}, fmt.Errorf("query metric_buckets: %w", err)
	}
	defer rows.Close()

	cadence := max(s.config.PersistInterval, time.Duration(ResolutionRawS)*time.Second)
	switch def.ResourceKinds[0] {
	case host.KindFilesystem:
		cadence = max(cadence, s.config.SlowFSInterval)
	case host.KindSensor:
		cadence = max(cadence, s.config.SensorInterval)
	}
	expected := max(1, int64(time.Duration(resS)*time.Second/cadence))

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
		if len(points) == maxPoints {
			return SeriesResult{}, fmt.Errorf("%w: query exceeds %d points at %ds resolution; shorten the range or choose a coarser step", ErrPointLimit, maxPoints, resS)
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

func resolveStep(ctx context.Context, tx *sql.Tx, q SeriesQuery, maxPoints int) (int, error) {
	res, err := ParseStep(q.Step)
	if err != nil || res != 0 {
		return res, err
	}
	return autoSelectResolution(ctx, tx, q, maxPoints)
}

// ParseStep returns the requested resolution in seconds, or zero for automatic selection.
func ParseStep(value string) (int, error) {
	step := strings.ToLower(strings.TrimSpace(value))
	switch step {
	case "", "auto":
		return 0, nil
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
		return 0, fmt.Errorf("unsupported step %q (expected auto, 30, 300, or 3600)", value)
	}
}

func autoSelectResolution(ctx context.Context, tx *sql.Tx, q SeriesQuery, maxPoints int) (int, error) {
	var chosen int
	var firstChosen int64
	var hasData bool
	for _, resS := range []int{ResolutionRawS, ResolutionMediumS, ResolutionLongS} {
		resMs := int64(resS) * 1000
		var count int
		var first, firstLastSample sql.NullInt64
		err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*), MIN(bucket_start_ms), MIN(last_sample_ms)
			FROM (
			  SELECT bucket_start_ms, last_sample_ms FROM metric_buckets
			  WHERE resource_id = ? AND metric_key = ? AND resolution_s = ?
			    AND bucket_start_ms >= ? AND bucket_start_ms < ?
			  ORDER BY bucket_start_ms LIMIT ?
			)
		`, q.ResourceID, q.MetricKey, resS, metrics.AlignBucketMs(q.FromMs, resMs), q.ToMs, maxPoints+1).Scan(&count, &first, &firstLastSample)
		if err != nil {
			return 0, err
		}
		hasData = hasData || count > 0
		if count == 0 || count > maxPoints {
			continue
		}
		if chosen == 0 || (firstLastSample.Int64 >= q.FromMs && firstLastSample.Int64 < firstChosen) {
			chosen, firstChosen = resS, first.Int64
		}
	}
	if chosen != 0 {
		return chosen, nil
	}
	if !hasData {
		return ResolutionRawS, nil
	}
	return 0, fmt.Errorf("%w: no populated resolution fits %d points; shorten the range", ErrPointLimit, maxPoints)
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
