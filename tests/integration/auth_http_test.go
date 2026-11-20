package integration

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"vektor/internal/auth"
	"vektor/internal/config"
	"vektor/internal/database"
)

func TestHTTPBootstrapConcurrent(t *testing.T) {
	f := newAPIFixture(t, nil, false, false)
	checkReply(t, f.login(t, auth.Username, testPassword, nil), 401, "invalid_credentials")
	setup := f.srv.URL + "/api/v1/auth/bootstrap"
	checkReply(t, exchange(t, f.srv.Client(), request(t, "POST", setup, nil, "", nil)), 404, "not_found")

	ctx := context.Background()
	results := make(chan error, 2)
	for range 2 {
		go func() { results <- f.sessions.Bootstrap(ctx, []byte(testPassword)) }()
	}
	first, second := <-results, <-results
	if !((first == nil && errors.Is(second, auth.ErrAlreadyBootstrapped)) ||
		(second == nil && errors.Is(first, auth.ErrAlreadyBootstrapped))) {
		t.Fatalf("expected exactly one bootstrap, got %v and %v", first, second)
	}
	var count, saltLen, hashLen int
	if err := f.db.Reader().QueryRowContext(ctx, `SELECT COUNT(*), length(salt), length(password_hash) FROM admin_credentials`).
		Scan(&count, &saltLen, &hashLen); err != nil || count != 1 || saltLen != 16 || hashLen != 32 {
		t.Fatalf("unexpected credential rows or lengths: %d %d %d (%v)", count, saltLen, hashLen, err)
	}
	f.loginOK(t)
}

func TestHTTPSessionsOriginCSRFExpiryAndRevocation(t *testing.T) {
	f := newAPIFixture(t, nil, false, true)
	ctx := context.Background()
	checkReply(t, f.login(t, auth.Username, testPassword, map[string]string{"Origin": "https://foreign.example"}), 403, "origin_denied")
	checkReply(t, f.login(t, auth.Username, testPassword, map[string]string{"Sec-Fetch-Site": "cross-site"}), 403, "origin_denied")
	checkReply(t, f.login(t, auth.Username, "incorrect", nil), 401, "invalid_credentials")
	checkReply(t, f.login(t, "other", testPassword, nil), 401, "invalid_credentials")

	reply := f.login(t, auth.Username, testPassword, map[string]string{
		"Origin": f.srv.URL, "X-Forwarded-Proto": "https", "X-Forwarded-Host": "foreign.example",
	})
	checkReply(t, reply, 200, "")
	if len(reply.cookies) != 1 {
		t.Fatalf("unexpected number of cookies: %d", len(reply.cookies))
	}
	cookie := reply.cookies[0]
	if cookie.Name != "vektor_session" || cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode ||
		cookie.Path != "/api/v1" || cookie.Domain != "" || cookie.MaxAge != 86400 || cookie.Expires.IsZero() {
		t.Fatal("unexpected local cookie policy")
	}
	if strings.Contains(string(reply.body), cookie.Value) {
		t.Fatal("session token was included in the JSON")
	}

	me := f.get(t, "/api/v1/auth/me", cookie)
	checkReply(t, me, 200, "")
	var current auth.Session
	if err := json.Unmarshal(me.body, &current); err != nil || current.CSRFToken == "" {
		t.Fatalf("could not read session: %v", err)
	}
	var digest []byte
	raw, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil || len(raw) != 32 {
		t.Fatal("invalid opaque session token")
	}
	if err := f.db.Reader().QueryRowContext(ctx, `SELECT token_hash FROM admin_sessions`).Scan(&digest); err != nil ||
		len(digest) != 32 {
		t.Fatalf("invalid stored digest: %v", err)
	}
	wantDigest := sha256.Sum256(raw)
	if string(digest) != string(wantDigest[:]) {
		t.Fatal("SQLite did not store the digest of the opaque secret")
	}

	checkReply(t, f.logout(t, cookie, nil), 403, "csrf_denied")
	valid := map[string]string{"X-CSRF-Token": current.CSRFToken}
	checkReply(t, f.logout(t, cookie, map[string]string{
		"Origin": "https://foreign.example", "X-CSRF-Token": current.CSRFToken,
	}), 403, "origin_denied")
	checkReply(t, f.logout(t, cookie, map[string]string{"X-CSRF-Token": "wrong"}), 403, "csrf_denied")
	checkReply(t, f.get(t, "/api/v1/auth/me", cookie), 200, "")

	second, secondSession := f.loginOK(t)
	checkReply(t, f.logout(t, cookie, map[string]string{"X-CSRF-Token": secondSession.CSRFToken}), 403, "csrf_denied")
	loggedOut := f.logout(t, cookie, valid)
	checkReply(t, loggedOut, 200, "")
	if len(loggedOut.cookies) != 1 || loggedOut.cookies[0].MaxAge >= 0 || loggedOut.cookies[0].Path != cookie.Path {
		t.Fatal("logout did not clear the scoped cookie")
	}
	checkReply(t, f.get(t, "/api/v1/auth/me", cookie), 401, "authentication_required")
	checkReply(t, f.get(t, "/api/v1/auth/me", second), 200, "")
	if _, err := f.db.Writer().ExecContext(ctx, `UPDATE admin_sessions SET expires_at_ms = ?`, time.Now().UnixMilli()-1); err != nil {
		t.Fatal(err)
	}
	checkReply(t, f.get(t, "/api/v1/auth/me", second), 401, "authentication_required")

	third, _ := f.loginOK(t)
	var count int
	if err := f.db.Reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_sessions`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("expired sessions not pruned: count=%d err=%v", count, err)
	}
	if err := f.sessions.RevokeAll(ctx); err != nil {
		t.Fatal(err)
	}
	checkReply(t, f.get(t, "/api/v1/auth/me", third), 401, "authentication_required")
}

func TestHTTPCookiesTLSAndProxy(t *testing.T) {
	t.Run("direct TLS", func(t *testing.T) {
		f := newAPIFixture(t, nil, true, true)
		reply := f.login(t, auth.Username, testPassword, map[string]string{"Origin": f.srv.URL})
		checkReply(t, reply, 200, "")
		if len(reply.cookies) != 1 || !reply.cookies[0].Secure || !reply.cookies[0].HttpOnly ||
			reply.headers.Get("Strict-Transport-Security") == "" || reply.headers.Get("Cache-Control") != "no-store" {
			t.Fatal("expected Secure cookie and HTTPS security headers")
		}
	})
	t.Run("explicit proxy origin", func(t *testing.T) {
		const origin = "https://monitor.example"
		f := newAPIFixture(t, func(cfg *config.Config) { cfg.HTTP.PublicOrigin = origin }, false, true)
		checkReply(t, f.login(t, auth.Username, testPassword, map[string]string{"Origin": f.srv.URL}), 403, "origin_denied")
		reply := f.login(t, auth.Username, testPassword, map[string]string{"Origin": origin})
		checkReply(t, reply, 200, "")
		if len(reply.cookies) != 1 || !reply.cookies[0].Secure || reply.headers.Get("Access-Control-Allow-Origin") != "" {
			t.Fatal("expected Secure cookie in explicitly configured proxy mode and no CORS access")
		}
	})
}

func TestHTTPLoginRateLimitAndPayload(t *testing.T) {
	f := newAPIFixture(t, nil, false, true)
	target := f.srv.URL + "/api/v1/auth/login"
	for _, tc := range []struct {
		body, contentType string
		status            int
		code              string
	}{
		{"username=admin&password=test", "application/x-www-form-urlencoded", 400, "invalid_request"},
		{strings.Repeat("x", 8193), "application/json", 413, "payload_too_large"},
		{`{"username":"admin","password":"incorrect"} {}`, "application/json", 400, "invalid_request"},
	} {
		reply := exchange(t, f.srv.Client(), request(t, "POST", target, nil, tc.body, map[string]string{"Content-Type": tc.contentType}))
		checkReply(t, reply, tc.status, tc.code)
	}
	for i := range 2 {
		checkReply(t, f.login(t, auth.Username, "incorrect", map[string]string{
			"X-Forwarded-For": "198.51.100." + string(rune('1'+i)),
		}), 401, "invalid_credentials")
	}
	reply := f.login(t, auth.Username, testPassword, map[string]string{"X-Forwarded-For": "198.51.100.9"})
	checkReply(t, reply, 429, "login_rate_limited")
	if reply.headers.Get("Retry-After") == "" {
		t.Fatal("rate limiter did not expose Retry-After")
	}
}

func TestHTTPLoopbackRejectsForeignHost(t *testing.T) {
	f := newAPIFixture(t, nil, false, false)
	req := request(t, "POST", f.srv.URL+"/api/v1/auth/login", nil, `{}`, map[string]string{
		"Content-Type": "application/json", "Origin": "http://foreign.example",
	})
	req.Host = "foreign.example"
	checkReply(t, exchange(t, f.srv.Client(), req), 403, "host_denied")
}

func TestHTTPConcurrentLogin(t *testing.T) {
	f := newAPIFixture(t, nil, false, true)
	const requests = 12
	start := make(chan struct{})
	results := make(chan int, requests)
	var wg sync.WaitGroup
	for range requests {
		req := request(t, "POST", f.srv.URL+"/api/v1/auth/login", nil,
			`{"username":"admin","password":"`+testPassword+`"}`, map[string]string{"Content-Type": "application/json"})
		wg.Go(func() {
			<-start
			response, err := f.srv.Client().Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			_, _ = io.Copy(io.Discard, response.Body)
			response.Body.Close()
			results <- response.StatusCode
		})
	}
	close(start)
	wg.Wait()
	close(results)
	var success, limited int
	for status := range results {
		switch status {
		case 200:
			success++
		case 429:
			limited++
		default:
			t.Errorf("unexpected concurrent response %d", status)
		}
	}
	var count int
	if err := f.db.Reader().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM admin_sessions`).Scan(&count); err != nil ||
		count != success || success < 1 || limited < 1 || success+limited != requests {
		t.Fatalf("successful=%d limited=%d sessions=%d err=%v", success, limited, count, err)
	}
}

func TestHTTPDatabaseBusyCanRecover(t *testing.T) {
	f := newAPIFixture(t, nil, false, true)
	cookie, session := f.loginOK(t)
	ctx := context.Background()
	external, err := database.Open(ctx, f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer external.Close()
	tx, err := external.Writer().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM admin_sessions WHERE 0`); err != nil {
		t.Fatal(err)
	}

	headers := map[string]string{"X-CSRF-Token": session.CSRFToken}
	checkReply(t, f.get(t, "/api/v1/host", cookie), 200, "")
	checkReply(t, f.logout(t, cookie, headers), 503, "database_unavailable")
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	checkReply(t, f.logout(t, cookie, headers), 200, "")
	checkReply(t, f.get(t, "/api/v1/auth/me", cookie), 401, "authentication_required")
}
