// Package web serves the optional embedded Vektor dashboard.
package web

import (
	"io/fs"
	"net/http"
	"strings"
)

const policy = "default-src 'none'; script-src 'self'; style-src 'self'; font-src 'self'; img-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

func handler(dist fs.FS) http.Handler {
	files := http.FileServerFS(dist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Security-Policy", policy)
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			name := strings.TrimPrefix(r.URL.Path, "/")
			if strings.Count(name, "/") != 1 {
				http.NotFound(w, r)
				return
			}
			info, err := fs.Stat(dist, name)
			if err != nil || !info.Mode().IsRegular() {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			files.ServeHTTP(w, r)
			return
		}

		switch r.URL.Path {
		case "/", "/login", "/overview", "/host", "/host/cpu", "/host/memory", "/host/storage", "/host/network", "/host/sensors", "/host/services", "/host/processes", "/settings":
			// FileServer serves index.html for the root without redirecting the browser.
			copy := r.Clone(r.Context())
			copy.URL.Path = "/"
			copy.URL.RawPath = ""
			files.ServeHTTP(w, copy)
		default:
			http.NotFound(w, r)
		}
	})
}
