package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestIsNewerSemver(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"0.4.0", "0.4.1", true},
		{"0.4.1", "0.4.1", false},
		{"0.4.2", "0.4.1", false},
		{"0.4.1-rc1", "0.4.1", true},
		{"0.4.1", "0.4.1-rc1", false},
		{"0.4.1", "1.0.0", true},
		{"dev", "0.4.1", false},
	}
	for _, tc := range cases {
		if got := isNewerSemver(tc.current, tc.latest); got != tc.want {
			t.Errorf("isNewerSemver(%q, %q) = %v, want %v", tc.current, tc.latest, got, tc.want)
		}
	}
}

func TestUpdateCheckerCachesResult(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v0.5.0","html_url":"https://github.com/patricks-js/vektor/releases/tag/v0.5.0"}`))
	}))
	defer srv.Close()

	checker := newUpdateChecker("0.4.1")
	checker.apiURL = srv.URL

	first := checker.Check(context.Background())
	if !first.UpdateAvailable || first.LatestVersion != "0.5.0" {
		t.Fatalf("unexpected first check: %+v", first)
	}
	second := checker.Check(context.Background())
	if second.LatestVersion != "0.5.0" || hits.Load() != 1 {
		t.Fatalf("expected cached response with 1 hit, got hits=%d info=%+v", hits.Load(), second)
	}
}
