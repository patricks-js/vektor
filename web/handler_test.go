package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestDashboardRouting(t *testing.T) {
	ui := handler(fstest.MapFS{
		"index.html":         {Data: []byte("<html>Vektor</html>")},
		"favicon.svg":        {Data: []byte("<svg></svg>")},
		"assets/app-hash.js": {Data: []byte("export {}")},
	})
	for _, path := range []string{"/", "/login", "/overview", "/host", "/host/cpu", "/host/memory", "/host/storage", "/host/network", "/host/sensors", "/host/services", "/host/processes", "/settings"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			w.Header().Set("Cache-Control", "no-store")
			ui.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path+"?range=7d", nil))
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Vektor") || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("unexpected document response: %d %v %q", w.Code, w.Header(), w.Body)
			}
			if w.Header().Get("Content-Security-Policy") != policy {
				t.Fatal("unexpected document CSP")
			}
		})
	}

	w := httptest.NewRecorder()
	ui.ServeHTTP(w, httptest.NewRequest(http.MethodHead, "/assets/app-hash.js", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") || w.Body.Len() != 0 {
		t.Fatalf("unexpected asset response: %d %v %q", w.Code, w.Header(), w.Body)
	}
	for _, path := range []string{"/unknown", "/assets", "/assets/missing.js", "/assets/other/missing.js"} {
		w := httptest.NewRecorder()
		ui.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s: expected 404, got %d", path, w.Code)
		}
	}
	w = httptest.NewRecorder()
	ui.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/favicon.svg", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "<svg>") {
		t.Fatalf("unexpected favicon response: %d %q", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	ui.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/login", nil))
	if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("unexpected method result: %d %v", w.Code, w.Header())
	}
}
