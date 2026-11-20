package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"vektor/internal/auth"
	"vektor/internal/config"
	"vektor/internal/database"
	"vektor/internal/history"
	"vektor/internal/host"
	"vektor/internal/httpapi"
	"vektor/internal/metrics"
	"vektor/internal/metrics/scheduler"
	"vektor/internal/observability"
)

const testPassword = "an integration password"

type apiFixture struct {
	cfg      config.Config
	db       *database.DB
	registry *host.Registry
	health   *observability.HealthTracker
	sessions *auth.Store
	hub      *scheduler.LiveHub
	srv      *httptest.Server
	cancel   context.CancelFunc
}

func newAPIFixture(t *testing.T, configure func(*config.Config), useTLS, bootstrap bool) *apiFixture {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.SQLiteBusyTimeoutMs = 100
	if configure != nil {
		configure(&cfg)
	}
	db, err := database.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	health := observability.NewHealthTracker()
	reg := host.NewRegistry(db)
	nowMs := time.Now().UnixMilli()
	if err := reg.EnsureHost(ctx, host.Inventory{
		Hostname: "vektor-test", CPUThreads: 4, MemoryTotalBytes: 8192, Kernel: "test-kernel",
		Arch: "amd64", OSName: "Ubuntu", UpdatedAtMs: nowMs,
	}, nowMs); err != nil {
		t.Fatal(err)
	}

	sessions := auth.NewStore(db, cfg.HTTP.SessionTTL)
	if bootstrap {
		if err := sessions.Bootstrap(ctx, []byte(testPassword)); err != nil {
			t.Fatal(err)
		}
	}
	hub := scheduler.NewLiveHub(health, nil, nil)
	api := httpapi.New(cfg.HTTP, history.NewQueryService(db, reg, cfg, health), sessions, hub)
	srv := httptest.NewUnstartedServer(api)
	srv.Config = api.Server(ctx)
	if useTLS {
		srv.EnableHTTP2 = true
		srv.Config.TLSConfig.NextProtos = []string{"h2", "http/1.1"}
		srv.TLS = srv.Config.TLSConfig
		srv.StartTLS()
	} else {
		srv.Start()
	}
	t.Cleanup(func() {
		cancel()
		srv.Close()
	})
	return &apiFixture{cfg, db, reg, health, sessions, hub, srv, cancel}
}

func request(t *testing.T, method, target string, cookie *http.Cookie, body string, headers map[string]string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, target, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

type apiReply struct {
	status  int
	headers http.Header
	cookies []*http.Cookie
	body    []byte
}

func exchange(t *testing.T, client *http.Client, req *http.Request) apiReply {
	t.Helper()
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return apiReply{resp.StatusCode, resp.Header, resp.Cookies(), body}
}

func checkReply(t *testing.T, reply apiReply, status int, code string) {
	t.Helper()
	if reply.status != status || reply.headers.Get("Content-Type") != "application/json" {
		t.Fatalf("status=%d type=%q, want %d application/json", reply.status, reply.headers.Get("Content-Type"), status)
	}
	if code == "" {
		return
	}
	var item struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(reply.body, &item); err != nil || item.Code != code || item.Message == "" {
		t.Fatalf("error code=%q, want %q, decode error=%v", item.Code, code, err)
	}
}

func (f *apiFixture) get(t *testing.T, path string, cookie *http.Cookie) apiReply {
	t.Helper()
	return exchange(t, f.srv.Client(), request(t, "GET", f.srv.URL+path, cookie, "", nil))
}

func (f *apiFixture) login(t *testing.T, username, password string, headers map[string]string) apiReply {
	t.Helper()
	body, err := json.Marshal(map[string]string{"username": username, "password": password})
	if err != nil {
		t.Fatal(err)
	}
	req := request(t, "POST", f.srv.URL+"/api/v1/auth/login", nil, string(body), headers)
	req.Header.Set("Content-Type", "application/json")
	return exchange(t, f.srv.Client(), req)
}

func (f *apiFixture) loginOK(t *testing.T) (*http.Cookie, auth.Session) {
	t.Helper()
	reply := f.login(t, auth.Username, testPassword, nil)
	checkReply(t, reply, 200, "")
	var s auth.Session
	if err := json.Unmarshal(reply.body, &s); err != nil || s.Username != auth.Username || s.CSRFToken == "" || s.ExpiresAt.Before(time.Now()) {
		t.Fatalf("invalid session response (decode error %v)", err)
	}
	if len(reply.cookies) != 1 {
		t.Fatalf("expected one session cookie, got %d", len(reply.cookies))
	}
	return reply.cookies[0], s
}

func (f *apiFixture) logout(t *testing.T, cookie *http.Cookie, headers map[string]string) apiReply {
	t.Helper()
	return exchange(t, f.srv.Client(), request(t, "POST", f.srv.URL+"/api/v1/auth/logout", cookie, "", headers))
}

func TestHTTPRESTInventoryAndResources(t *testing.T) {
	f := newAPIFixture(t, nil, false, true)
	ctx := context.Background()
	for _, path := range []string{"/host", "/resources", "/resources/host:local", "/metrics", "/health", "/stream/live", "/auth/me"} {
		checkReply(t, f.get(t, "/api/v1"+path, nil), 401, "authentication_required")
	}
	checkReply(t, f.get(t, "/api/v1/unknown", nil), 404, "not_found")
	badMethod := exchange(t, f.srv.Client(), request(t, "POST", f.srv.URL+"/api/v1/host", nil, "", nil))
	checkReply(t, badMethod, 405, "method_not_allowed")
	if badMethod.headers.Get("Allow") != "GET" {
		t.Fatal("expected Allow: GET")
	}

	cookie, _ := f.loginOK(t)
	got := f.get(t, "/api/v1/host", cookie)
	checkReply(t, got, 200, "")
	var inventory struct {
		history.HostResponse
		Uptime *int64 `json:"uptime_seconds"`
	}
	if err := json.Unmarshal(got.body, &inventory); err != nil || inventory.Resource.ID != host.HostResourceID ||
		inventory.Inventory.Hostname != "vektor-test" || inventory.Inventory.OSName != "Ubuntu" ||
		inventory.Inventory.CPUThreads != 4 || inventory.Inventory.MemoryTotalBytes != 8192 || inventory.Uptime == nil || *inventory.Uptime < 0 {
		t.Fatalf("unexpected inventory: %+v, err=%v", inventory, err)
	}

	nowMs := time.Now().UnixMilli()
	a := host.DiscoveredResource{ExternalKey: "virtual:one", DisplayName: "one"}
	b := host.DiscoveredResource{ExternalKey: "virtual:two", DisplayName: "two"}
	rows, err := f.registry.Reconcile(ctx, host.KindNIC, []host.DiscoveredResource{a, b}, nowMs)
	if err != nil {
		t.Fatal(err)
	}
	activeID, inactiveID := rows[0].ID, rows[1].ID
	a.DisplayName = "renamed"
	if _, err := f.registry.Reconcile(ctx, host.KindNIC, []host.DiscoveredResource{a}, nowMs+5); err != nil {
		t.Fatal(err)
	}

	type resourcePage struct {
		Resources []host.Resource `json:"resources"`
		Cursor    string          `json:"next_cursor"`
	}
	page := func(query string) resourcePage {
		t.Helper()
		reply := f.get(t, "/api/v1/resources?kind=nic"+query, cookie)
		checkReply(t, reply, 200, "")
		var p resourcePage
		if err := json.Unmarshal(reply.body, &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if p := page(""); len(p.Resources) != 1 || p.Resources[0].ID != activeID || p.Resources[0].DisplayName != "renamed" {
		t.Fatalf("active page: %+v", p)
	}
	if p := page("&state=inactive"); len(p.Resources) != 1 || p.Resources[0].ID != inactiveID || p.Resources[0].Active {
		t.Fatalf("inactive page: %+v", p)
	}
	first := page("&state=all&limit=1")
	second := page("&state=all&limit=1&cursor=" + url.QueryEscape(first.Cursor))
	if len(first.Resources) != 1 || len(second.Resources) != 1 || first.Cursor == "" ||
		first.Resources[0].ID >= second.Resources[0].ID || second.Cursor != "" {
		t.Fatalf("pagination: %+v then %+v", first, second)
	}
	detail := f.get(t, "/api/v1/resources/"+inactiveID, cookie)
	checkReply(t, detail, 200, "")
	var resource host.Resource
	if err := json.Unmarshal(detail.body, &resource); err != nil || resource.ID != inactiveID || resource.Active {
		t.Fatalf("inactive detail: %+v (%v)", resource, err)
	}
	checkReply(t, f.get(t, "/api/v1/resources/unknown:1", cookie), 404, "resource_not_found")
	checkReply(t, f.get(t, "/api/v1/resources?limit=101", cookie), 400, "invalid_parameters")
	checkReply(t, f.get(t, "/api/v1/resources?state=anything", cookie), 400, "invalid_parameters")

	f.health.RecordCollect(time.Now())
	checkReply(t, f.get(t, "/api/v1/health", cookie), 200, "")
	f.health.RecordDBError(errors.New("SQLite error at /private/state/vektor.db"))
	f.health.SetClockError("unexpected internal value /private/clock")
	degraded := f.get(t, "/api/v1/health", cookie)
	checkReply(t, degraded, 503, "database_unavailable")
	var info struct {
		Diagnostics history.HealthResponse `json:"diagnostics"`
	}
	if err := json.Unmarshal(degraded.body, &info); err != nil || info.Diagnostics.LastCollectAt == nil ||
		info.Diagnostics.DBOK || !info.Diagnostics.DBPingOK || strings.Contains(string(degraded.body), "/private/") {
		t.Fatalf("health details missing or unredacted: decode error=%v", err)
	}

	if err := f.db.Reader().Close(); err != nil {
		t.Fatal(err)
	}
	checkReply(t, f.get(t, "/api/v1/host", cookie), 503, "database_unavailable")
}

func TestHTTPHistoricalSeries(t *testing.T) {
	f := newAPIFixture(t, nil, false, true)
	cookie, _ := f.loginOK(t)
	base := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC).UnixMilli()
	for _, row := range []struct {
		step, offset, n, value int
	}{
		{30, 0, 1, 0}, {30, 60, 1, 40}, {30, 330, 1, 90},
		{300, 0, 2, 20}, {300, 300, 1, 90},
	} {
		ts := base + int64(row.offset)*1000
		if _, err := f.db.Writer().ExecContext(context.Background(),
			`INSERT INTO metric_buckets VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			host.HostResourceID, metrics.KeyLoad1m, row.step, ts, row.n, row.n*row.value, row.value, row.value, row.value, ts+1000); err != nil {
			t.Fatal(err)
		}
	}
	baseQuery := fmt.Sprintf("/api/v1/metrics?resource_id=%s&metric=%s&from=%d&to=%d",
		host.HostResourceID, metrics.KeyLoad1m, base, base+360_000)
	for _, tc := range []struct {
		path   string
		status int
		code   string
	}{
		{strings.Replace(baseQuery, "load.1m", "bogus", 1), 400, "invalid_parameters"},
		{strings.Replace(baseQuery, host.HostResourceID, "host:missing", 1), 404, "resource_not_found"},
		{strings.Replace(baseQuery, "load.1m", "cpu.core.busy_pct", 1), 400, "invalid_parameters"},
		{strings.Replace(baseQuery, fmt.Sprint(base), "not-a-time", 1), 400, "invalid_parameters"},
		{strings.Replace(baseQuery, fmt.Sprint(base+360_000), fmt.Sprint(base), 1), 400, "invalid_parameters"},
		{strings.Replace(baseQuery, fmt.Sprint(base+360_000), fmt.Sprint(base+91*24*3600_000), 1), 422, "range_limit"},
		{baseQuery + "&max_points=2001", 400, "invalid_parameters"},
		{baseQuery + "&max_points=0", 400, "invalid_parameters"},
		{baseQuery + "&step=42", 400, "invalid_parameters"},
		{baseQuery + "&step=auto&step=30", 400, "invalid_parameters"},
		{baseQuery + "&step=30&max_points=2", 422, "point_limit"},
		{baseQuery + "&step=auto&max_points=1", 422, "point_limit"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			checkReply(t, f.get(t, tc.path, cookie), tc.status, tc.code)
		})
	}
	query := func(target string) history.SeriesResult {
		t.Helper()
		result := f.get(t, target, cookie)
		checkReply(t, result, 200, "")
		var series history.SeriesResult
		if err := json.Unmarshal(result.body, &series); err != nil {
			t.Fatal(err)
		}
		return series
	}
	raw := query(baseQuery + "&step=auto")
	if raw.ResolutionS != 30 || raw.Unit != "load" || raw.FromMs != base || raw.ToMs != base+360_000 ||
		len(raw.Points) != 3 || raw.Points[0].Avg != 0 || raw.Points[1].BucketStartMs != base+60_000 ||
		raw.Points[0].Coverage != 1 || raw.Points[0].SampleCount != 1 || raw.Points[0].ExpectedSamples != 1 {
		t.Fatalf("raw gaps or zero lost: %+v", raw)
	}
	coarse := query(baseQuery + "&max_points=2")
	if coarse.ResolutionS != 300 || len(coarse.Points) != 2 || coarse.Points[0].ExpectedSamples != 10 ||
		coarse.Points[0].Coverage != 0.2 || coarse.Points[0].Avg != 20 {
		t.Fatalf("unexpected auto resolution or coverage: %+v", coarse)
	}
	explicit := query("/api/v1/metrics?resource_id=host:local&metric=load.1m&from=2026-11-20T12:00:00Z&to=2026-11-20T12:06:00Z&step=5m")
	if explicit.ResolutionS != coarse.ResolutionS || len(explicit.Points) != 2 {
		t.Fatalf("unexpected explicit resolution: %+v", explicit)
	}
	empty := query(strings.Replace(baseQuery, "load.1m", "load.5m", 1) + "&step=auto")
	if empty.ResolutionS != 30 || empty.Points == nil || len(empty.Points) != 0 {
		t.Fatalf("empty series has no explicit empty point array: %+v", empty)
	}
}
