package integration

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"vektor/internal/config"
	"vektor/internal/history"
	"vektor/internal/host"
	"vektor/internal/metrics"
	"vektor/internal/metrics/scheduler"
	"vektor/internal/metrics/tracker"
)

func eventually(t *testing.T, timeout time.Duration, condition func() bool, what string) {
	t.Helper()
	finish := time.Now().Add(timeout)
	for time.Now().Before(finish) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

type sseClient struct {
	resp   *http.Response
	reader *bufio.Reader
	cancel context.CancelFunc
}

func (f *apiFixture) openStream(t *testing.T, client *http.Client, cookie *http.Cookie) *sseClient {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	req := request(t, "GET", f.srv.URL+"/api/v1/stream/live", cookie, "", nil).WithContext(ctx)
	resp, err := client.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	stream := &sseClient{resp, bufio.NewReader(resp.Body), cancel}
	t.Cleanup(stream.close)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" ||
		resp.Header.Get("X-Accel-Buffering") != "no" || stream.line(t) != ": connected" || stream.line(t) != "" {
		t.Fatalf("unexpected stream start, status %d", resp.StatusCode)
	}
	return stream
}

func (s *sseClient) close() {
	s.cancel()
	_ = s.resp.Body.Close()
}

func (s *sseClient) line(t *testing.T) string {
	t.Helper()
	line, err := s.reader.ReadString('\n')
	if err != nil {
		t.Fatalf("reading SSE: %v", err)
	}
	return strings.TrimSuffix(line, "\n")
}

type streamSnapshot struct {
	CollectedAt time.Time `json:"collected_at"`
	Points      []struct {
		ResourceID string  `json:"resource_id"`
		Metric     string  `json:"metric"`
		Value      float64 `json:"value"`
	} `json:"points"`
}

func (s *sseClient) snapshot(t *testing.T) streamSnapshot {
	t.Helper()
	if got := s.line(t); got != "event: snapshot" {
		t.Fatalf("unexpected SSE event %q", got)
	}
	data, ok := strings.CutPrefix(s.line(t), "data: ")
	var snap streamSnapshot
	if err := json.Unmarshal([]byte(data), &snap); err != nil || !ok || snap.Points == nil || snap.CollectedAt.IsZero() || s.line(t) != "" {
		t.Fatalf("invalid SSE JSON or framing: %v", err)
	}
	return snap
}

func (s *sseClient) readUntilClosed(t *testing.T) {
	t.Helper()
	finished := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, s.reader)
		finished <- err
	}()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("stream did not close cleanly: %v", err)
		}
	case <-time.After(5 * time.Second):
		s.close()
		<-finished
		t.Fatal("stream remained open")
	}
}

type streamCollector struct {
	clk   metrics.Clock
	calls atomic.Int64
	gate  <-chan struct{}
}

func (*streamCollector) Name() string { return "integration_test" }

func (c *streamCollector) Collect(ctx context.Context) (metrics.RawSnapshot, error) {
	n := c.calls.Add(1)
	if n == 1 && c.gate != nil {
		select {
		case <-c.gate:
		case <-ctx.Done():
			return metrics.RawSnapshot{}, ctx.Err()
		}
	}
	now, mono := c.clk.Now(), c.clk.Monotonic()
	return metrics.RawSnapshot{
		CollectedAt: now, Monotonic: mono,
		Samples: []metrics.RawSample{
			metrics.NewGaugeSample(host.HostResourceID, metrics.KeyLoad1m, float64(n), now, mono),
		},
	}, nil
}

func (f *apiFixture) startScheduler(t *testing.T, gate <-chan struct{}) *streamCollector {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	clk := metrics.NewRealClock()
	col := &streamCollector{clk: clk, gate: gate}
	sched := scheduler.New(scheduler.Deps{
		Config: f.cfg, Clock: clk, ClockSynchronized: func() bool { return true },
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Health: f.health,
		Normalizer: tracker.NewNormalizer(nil), FastCollectors: []metrics.Collector{col},
		RawWriter: history.NewRawWriter(f.db, f.cfg, f.health, nil, clk),
		LiveHub:   f.hub, BootIDProvider: func() (string, error) { return "test-boot", nil },
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = sched.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return col
}

func TestHTTPSSEOnDemandSharedCollectionAndNoSQLiteWrites(t *testing.T) {
	before := runtime.NumGoroutine()
	t.Run("connect, disconnect and reconnect", func(t *testing.T) {
		f := newAPIFixture(t, func(c *config.Config) {
			c.PersistInterval = time.Hour
			c.LiveInterval = 120 * time.Millisecond
			c.HTTP.MaxLiveClients = 2
		}, false, true)
		cookie, _ := f.loginOK(t)
		conn, err := f.db.Reader().Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		version := func() int {
			t.Helper()
			var v int
			if err := conn.QueryRowContext(context.Background(), `PRAGMA data_version`).Scan(&v); err != nil {
				t.Fatal(err)
			}
			return v
		}
		startVersion := version()

		gate := make(chan struct{})
		col := f.startScheduler(t, gate)
		time.Sleep(2 * f.cfg.LiveInterval)
		if col.calls.Load() != 0 {
			t.Fatal("collection started with no subscribers")
		}
		fast := f.openStream(t, f.srv.Client(), cookie)
		eventually(t, 2*time.Second, func() bool { return col.calls.Load() == 1 }, "first subscriber to activate live")
		second := f.openStream(t, f.srv.Client(), cookie)
		checkReply(t, f.get(t, "/api/v1/stream/live", cookie), 429, "capacity_exceeded")
		close(gate)
		a, b := fast.snapshot(t), second.snapshot(t)
		if !a.CollectedAt.Equal(b.CollectedAt) || len(a.Points) != 1 || len(b.Points) != 1 ||
			a.Points[0] != b.Points[0] || a.Points[0].Metric != metrics.KeyLoad1m || a.Points[0].Value != 1 {
			t.Fatalf("expected one shared collection, got %+v and %+v", a, b)
		}

		fast.close()
		eventually(t, 2*time.Second, func() bool { return f.hub.SubscriberCount() == 1 }, "first disconnect")
		_ = second.snapshot(t)
		second.close()
		eventually(t, 2*time.Second, func() bool { return f.hub.SubscriberCount() == 0 }, "last disconnect")
		// Allow an in-flight cycle to finish before checking that the loop stays idle.
		time.Sleep(f.cfg.LiveInterval)
		calls := col.calls.Load()
		time.Sleep(2 * f.cfg.LiveInterval)
		if col.calls.Load() != calls {
			t.Fatal("live collection continued after the last disconnect")
		}

		for range 12 {
			stream := f.openStream(t, f.srv.Client(), cookie)
			if snap := stream.snapshot(t); len(snap.Points) != 1 || snap.Points[0].Value <= float64(calls) {
				t.Fatal("live collection did not resume after reconnect")
			}
			stream.close()
			eventually(t, 2*time.Second, func() bool { return f.hub.SubscriberCount() == 0 }, "repeated disconnect cleanup")
		}
		if f.health.Snapshot().ActiveLiveSubscribers != 0 || version() != startVersion {
			t.Fatal("streaming left subscribers or committed changes to SQLite")
		}
	})
	eventually(t, 3*time.Second, func() bool { return runtime.NumGoroutine() <= before+2 }, "HTTP and SSE goroutines to exit")
}

func TestHTTPSSEPersistenceAndLogout(t *testing.T) {
	f := newAPIFixture(t, func(c *config.Config) {
		c.PersistInterval = 100 * time.Millisecond
		c.LiveInterval = 80 * time.Millisecond
	}, false, true)
	cookie, s := f.loginOK(t)
	f.startScheduler(t, nil)

	var latest int64
	checkPersist := func() bool {
		var ts int64
		err := f.db.Reader().QueryRowContext(context.Background(),
			`SELECT MAX(last_sample_ms) FROM metric_buckets WHERE resource_id = ? AND metric_key = ?`,
			host.HostResourceID, metrics.KeyLoad1m).Scan(&ts)
		if err == nil && ts > latest {
			latest = ts
			return true
		}
		return false
	}
	eventually(t, 3*time.Second, checkPersist, "persistent writes without subscribers")
	stream := f.openStream(t, f.srv.Client(), cookie)
	_ = stream.snapshot(t)
	eventually(t, 3*time.Second, checkPersist, "persistent writes during streaming")
	checkReply(t, f.logout(t, cookie, map[string]string{"X-CSRF-Token": s.CSRFToken}), 200, "")
	stream.readUntilClosed(t)
	eventually(t, 2*time.Second, func() bool { return f.hub.SubscriberCount() == 0 }, "logout to remove live subscriber")
	eventually(t, 3*time.Second, checkPersist, "persistent writes after streaming")
}

func TestHTTPSSENonReadingClient(t *testing.T) {
	f := newAPIFixture(t, func(c *config.Config) {
		c.PersistInterval = 100 * time.Millisecond
		c.LiveInterval = time.Hour
	}, false, true)
	cookie, _ := f.loginOK(t)
	f.startScheduler(t, nil)
	dialer := net.Dialer{Timeout: time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := dialer.DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			if err := conn.(*net.TCPConn).SetReadBuffer(4096); err != nil {
				conn.Close()
				return nil, err
			}
			return conn, nil
		},
	}
	t.Cleanup(transport.CloseIdleConnections)
	_ = f.openStream(t, &http.Client{Transport: transport}, cookie) // Leave the response unread.

	point := metrics.MetricPoint{
		ResourceID: "nic:" + strings.Repeat("a", 120), Key: metrics.KeyNetRxBytesPerSec,
		TimestampMs: time.Now().UnixMilli(), Value: 123, Valid: true,
	}
	points := make([]metrics.MetricPoint, 8192)
	for i := range points {
		points[i] = point
	}
	eventually(t, 2*time.Second, func() bool {
		f.hub.Publish(scheduler.LiveSnapshot{CollectedAt: time.Now(), Points: points})
		return f.health.Snapshot().DroppedLiveEvents > 0
	}, "SSE backpressure to fill the slow subscriber's queue")
	writeBefore := f.health.Snapshot().LastWriteAt

	fast := f.openStream(t, f.srv.Client(), cookie)
	f.hub.Publish(scheduler.LiveSnapshot{CollectedAt: time.Now(), Points: points[:1]})
	if snap := fast.snapshot(t); len(snap.Points) != 1 || snap.Points[0].Value != 123 {
		t.Fatal("slow SSE client prevented delivery to a fast client")
	}
	eventually(t, 2*time.Second, func() bool {
		written := f.health.Snapshot().LastWriteAt
		return written != nil && (writeBefore == nil || written.After(*writeBefore)) && f.hub.SubscriberCount() == 2
	}, "persistent storage to advance while the slow stream remains blocked")
	eventually(t, 5*time.Second, func() bool { return f.hub.SubscriberCount() == 1 }, "write deadline to remove blocked subscriber")
}

func TestHTTPSSEHeartbeatAndShutdown(t *testing.T) {
	f := newAPIFixture(t, nil, true, true)
	cookie, _ := f.loginOK(t)
	stream := f.openStream(t, f.srv.Client(), cookie)
	if stream.resp.ProtoMajor != 2 || stream.line(t) != ": heartbeat" || stream.line(t) != "" {
		t.Fatal("expected an HTTP/2 SSE heartbeat after the ordinary write deadline")
	}
	f.cancel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := f.srv.Config.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	stream.readUntilClosed(t)
	if f.hub.SubscriberCount() != 0 {
		t.Fatal("shutdown retained a subscriber")
	}
}

func TestHTTPSSESessionExpiry(t *testing.T) {
	f := newAPIFixture(t, func(cfg *config.Config) { cfg.HTTP.SessionTTL = time.Second }, false, true)
	cookie, _ := f.loginOK(t)
	stream := f.openStream(t, f.srv.Client(), cookie)
	stream.readUntilClosed(t)
	checkReply(t, f.get(t, "/api/v1/stream/live", cookie), 401, "authentication_required")
}
