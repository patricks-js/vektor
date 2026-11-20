//go:build !webui

package web

import "net/http"

// Handler returns nil in API-only builds.
func Handler() http.Handler { return nil }
