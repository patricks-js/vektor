package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"golang.org/x/sys/unix"

	"vektor/internal/history"
	"vektor/internal/host"
	"vektor/internal/metrics"
)

const (
	maxHistoryRange = 90 * 24 * time.Hour
	defaultPageSize = 50
	maxPageSize     = 100
)

func queryValues(w http.ResponseWriter, r *http.Request, allowed ...string) (url.Values, bool) {
	values, err := url.ParseQuery(r.URL.RawQuery)
	for key, val := range values {
		if len(val) != 1 || !slices.Contains(allowed, key) {
			invalidParams(w, "Unknown or repeated query parameter")
			return nil, false
		}
	}
	if err != nil {
		invalidParams(w, "Malformed query string")
		return nil, false
	}
	return values, true
}

func invalidParams(w http.ResponseWriter, message string) {
	writeError(w, http.StatusBadRequest, "invalid_parameters", message)
}

func (a *API) host(w http.ResponseWriter, r *http.Request) {
	if _, ok := queryValues(w, r); !ok {
		return
	}
	result, err := a.queries.GetHost(r.Context())
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusServiceUnavailable, "inventory_unavailable", "Host inventory is not available yet")
		return
	}
	if err != nil {
		writeFailure(w, err)
		return
	}
	// CLOCK_BOOTTIME provides current uptime with one inexpensive syscall.
	var uptime unix.Timespec
	var seconds *int64
	if unix.ClockGettime(unix.CLOCK_BOOTTIME, &uptime) == nil {
		value := int64(uptime.Sec)
		seconds = &value
	}
	writeJSON(w, http.StatusOK, struct {
		history.HostResponse
		UptimeSeconds *int64 `json:"uptime_seconds,omitempty"`
	}{result, seconds})
}

func validKind(kind string) bool {
	switch kind {
	case "", host.KindHost, host.KindCPUCore, host.KindDisk, host.KindFilesystem, host.KindNIC, host.KindSensor:
		return true
	}
	return false
}

func validID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, ch := range id {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' ||
			ch == ':' || ch == '_' || ch == '-') {
			return false
		}
	}
	return true
}

func (a *API) resources(w http.ResponseWriter, r *http.Request) {
	q, ok := queryValues(w, r, "kind", "state", "limit", "cursor")
	if !ok {
		return
	}
	kind, cursor := q.Get("kind"), q.Get("cursor")
	if !validKind(kind) || (q.Has("cursor") && !validID(cursor)) {
		invalidParams(w, "Invalid resource kind or cursor")
		return
	}
	var active *bool
	switch q.Get("state") {
	case "", "active":
		value := true
		active = &value
	case "inactive":
		value := false
		active = &value
	case "all":
	default:
		invalidParams(w, "state must be active, inactive, or all")
		return
	}
	limit := defaultPageSize
	if q.Has("limit") {
		var err error
		limit, err = strconv.Atoi(q.Get("limit"))
		if err != nil || limit < 1 || limit > maxPageSize {
			invalidParams(w, "limit must be between 1 and 100")
			return
		}
	}

	items, err := a.queries.ListResourcePage(r.Context(), kind, active, cursor, limit+1)
	if err != nil {
		writeFailure(w, err)
		return
	}
	var next string
	if len(items) > limit {
		items = items[:limit]
		next = items[limit-1].ID
	}
	if items == nil {
		items = []host.Resource{}
	}
	writeJSON(w, http.StatusOK, struct {
		Resources  []host.Resource `json:"resources"`
		NextCursor string          `json:"next_cursor,omitempty"`
	}{items, next})
}

func (a *API) resource(w http.ResponseWriter, r *http.Request) {
	if _, ok := queryValues(w, r); !ok {
		return
	}
	id := r.PathValue("id")
	if !validID(id) {
		invalidParams(w, "Invalid resource ID")
		return
	}
	resource, err := a.queries.GetResource(r.Context(), id)
	if err != nil {
		writeResourceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resource)
}

func writeResourceError(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "resource_not_found", "Resource not found")
		return
	}
	writeFailure(w, err)
}

func (a *API) series(w http.ResponseWriter, r *http.Request) {
	q, ok := queryValues(w, r, "resource_id", "metric", "from", "to", "step", "max_points")
	if !ok {
		return
	}
	id, key := q.Get("resource_id"), q.Get("metric")
	if !validID(id) {
		invalidParams(w, "Invalid resource_id")
		return
	}
	def, ok := metrics.LookupMetric(key)
	if !ok {
		invalidParams(w, "Unknown metric")
		return
	}
	from, fromOK := parseTime(q.Get("from"))
	to, toOK := parseTime(q.Get("to"))
	if !fromOK || !toOK || to <= from {
		invalidParams(w, "from and to must be UTC instants with from earlier than to")
		return
	}
	if to-from > maxHistoryRange.Milliseconds() {
		writeError(w, http.StatusUnprocessableEntity, "range_limit", "Time range exceeds 90 days")
		return
	}
	step := q.Get("step")
	if _, err := history.ParseStep(step); err != nil {
		invalidParams(w, "Unsupported step")
		return
	}
	maxPoints := history.DefaultMaxQueryPoints
	if q.Has("max_points") {
		var err error
		maxPoints, err = strconv.Atoi(q.Get("max_points"))
		if err != nil || maxPoints < 1 || maxPoints > history.MaxQueryPoints {
			invalidParams(w, "max_points must be between 1 and 2000")
			return
		}
	}

	res, err := a.queries.GetResource(r.Context(), id)
	if err != nil {
		writeResourceError(w, err)
		return
	}
	if !slices.Contains(def.ResourceKinds, res.Kind) {
		invalidParams(w, "Metric is not available for this resource kind")
		return
	}
	result, err := a.queries.QuerySeries(r.Context(), history.SeriesQuery{
		ResourceID: id, MetricKey: key, FromMs: from, ToMs: to, Step: step, MaxPoints: maxPoints,
	})
	switch {
	case errors.Is(err, history.ErrPointLimit):
		writeError(w, http.StatusUnprocessableEntity, "point_limit", "No requested historical resolution fits the point limit")
	case err != nil:
		writeFailure(w, err)
	default:
		writeJSON(w, http.StatusOK, result)
	}
}

func parseTime(value string) (int64, bool) {
	const lastYearMs = 253402300799999 // 9999-12-31T23:59:59.999Z
	ms, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		t, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return 0, false
		}
		ms = t.UnixMilli()
	}
	return ms, ms >= 0 && ms <= lastYearMs
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	if _, ok := queryValues(w, r); !ok {
		return
	}
	result, err := a.queries.GetHealth(r.Context())
	if err != nil {
		writeFailure(w, err)
		return
	}
	if result.LastDBError != "" {
		result.LastDBError = "A database write or maintenance operation failed"
	}
	if result.DBPingError != "" {
		result.DBPingError = "Database temporarily unavailable"
	}
	if result.ClockError != "" {
		result.ClockError = "Wall clock is not trusted; historical writes are paused"
	}
	if !result.DBOK || !result.DBPingOK {
		writeJSON(w, http.StatusServiceUnavailable, struct {
			apiError
			Diagnostics history.HealthResponse `json:"diagnostics"`
		}{apiError{"database_unavailable", "Database temporarily unavailable"}, result})
		return
	}
	writeJSON(w, http.StatusOK, result)
}
