// Package httpapi exposes authenticated monitoring over net/http.
package httpapi

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"path"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"vektor/internal/alerts"
	"vektor/internal/audit"
	"vektor/internal/auth"
	"vektor/internal/config"
	"vektor/internal/ctl"
	"vektor/internal/database"
	"vektor/internal/diagnostics"
	"vektor/internal/history"
	"vektor/internal/hostmgmt"
	"vektor/internal/metrics/scheduler"
)

const (
	apiPath       = "/api/v1"
	streamPath    = apiPath + "/stream/live"
	loginPath     = apiPath + "/auth/login"
	logoutPath    = apiPath + "/auth/logout"
	sessionCookie = "vektor_session"

	requestTimeout = 8 * time.Second
	writeTimeout   = 10 * time.Second
	maxConnections = 128
)

type API struct {
	cfg      config.HTTPConfig
	local    bool
	queries  *history.QueryService
	auth     *auth.Store
	alerts   *alerts.Store
	notifier *alerts.Notifier
	updates  *updateChecker
	mgmt     *hostmgmt.Manager
	ctl      *ctl.Client
	audit    *audit.Store
	diag     *diagnostics.Runner
	hub      *scheduler.LiveHub
	mux      *http.ServeMux
	ui       http.Handler
	requests chan struct{}
	streams  chan struct{}
	password chan struct{}
	logins   loginLimiter
}

func New(cfg config.HTTPConfig, queries *history.QueryService, sessions *auth.Store, hub *scheduler.LiveHub) *API {
	a := &API{
		cfg: cfg, queries: queries, auth: sessions, hub: hub,
		local:    cfg.PublicOrigin == "" && loopbackHost(cfg.ListenAddr),
		mux:      http.NewServeMux(),
		requests: make(chan struct{}, 8),
		streams:  make(chan struct{}, cfg.MaxLiveClients),
		password: make(chan struct{}, 1),
	}
	a.route(http.MethodGet, apiPath+"/host", true, a.host)
	a.route(http.MethodGet, apiPath+"/resources", true, a.resources)
	a.route(http.MethodGet, apiPath+"/resources/{id}", true, a.resource)
	a.route(http.MethodGet, apiPath+"/metrics", true, a.series)
	a.route(http.MethodGet, apiPath+"/health", true, a.health)
	a.route(http.MethodGet, apiPath+"/diagnostics", true, a.diagnostics)
	a.route(http.MethodGet, streamPath, true, a.live)
	a.route(http.MethodPost, loginPath, false, a.login)
	a.route(http.MethodPost, logoutPath, true, a.logout)
	a.route(http.MethodGet, apiPath+"/auth/me", true, a.me)
	a.routeMethods(apiPath+"/alerts/rules", true, map[string]http.HandlerFunc{
		http.MethodGet:  a.listAlertRules,
		http.MethodPost: a.createAlertRule,
	})
	a.routeMethods(apiPath+"/alerts/rules/{id}", true, map[string]http.HandlerFunc{
		http.MethodPut:    a.updateAlertRule,
		http.MethodDelete: a.deleteAlertRule,
	})
	a.routeMethods(apiPath+"/alerts/channels", true, map[string]http.HandlerFunc{
		http.MethodGet:  a.listAlertChannels,
		http.MethodPost: a.createAlertChannel,
	})
	a.routeMethods(apiPath+"/alerts/channels/{id}", true, map[string]http.HandlerFunc{
		http.MethodDelete: a.deleteAlertChannel,
	})
	a.route(http.MethodPost, apiPath+"/alerts/channels/test", true, a.testAlertChannel)
	a.route(http.MethodGet, apiPath+"/alerts/events", true, a.listAlertEvents)
	a.route(http.MethodGet, apiPath+"/update", true, a.update)
	a.route(http.MethodGet, apiPath+"/processes", true, a.processes)
	a.route(http.MethodPost, apiPath+"/processes/{pid}/signal", true, a.processSignal)
	a.route(http.MethodGet, apiPath+"/services", true, a.services)
	a.route(http.MethodGet, apiPath+"/services/{name}/logs", true, a.serviceLogs)
	a.route(http.MethodPost, apiPath+"/services/{name}/action", true, a.serviceAction)
	a.route(http.MethodPost, apiPath+"/host/power", true, a.hostPower)
	a.route(http.MethodGet, apiPath+"/audit", true, a.listAudit)
	a.route(http.MethodGet, apiPath+"/incus/status", true, a.incusStatus)
	a.route(http.MethodGet, apiPath+"/incus/resources", true, a.listIncusResources)
	a.routeMethods(apiPath+"/incus/instances", true, map[string]http.HandlerFunc{
		http.MethodGet:  a.listIncusInstances,
		http.MethodPost: a.createIncusInstance,
	})
	a.routeMethods(apiPath+"/incus/instances/{name}", true, map[string]http.HandlerFunc{
		http.MethodGet:    a.getIncusInstance,
		http.MethodPatch:  a.updateIncusInstance,
		http.MethodDelete: a.deleteIncusInstance,
	})
	a.route(http.MethodPost, apiPath+"/incus/instances/{name}/state", true, a.incusInstanceState)
	a.route(http.MethodPost, apiPath+"/incus/instances/{name}/exec", true, a.execIncusInstance)
	a.route(http.MethodGet, apiPath+"/incus/instances/{name}/console", true, a.incusConsoleLog)
	a.routeMethods(apiPath+"/incus/instances/{name}/snapshots", true, map[string]http.HandlerFunc{
		http.MethodGet:  a.listIncusSnapshots,
		http.MethodPost: a.createIncusSnapshot,
	})
	a.route(http.MethodPost, apiPath+"/incus/instances/{name}/snapshots/{snap}/restore", true, a.restoreIncusSnapshot)
	a.route(http.MethodDelete, apiPath+"/incus/instances/{name}/snapshots/{snap}", true, a.deleteIncusSnapshot)
	a.routeMethods(apiPath+"/incus/instances/{name}/backups", true, map[string]http.HandlerFunc{
		http.MethodGet:  a.listIncusBackups,
		http.MethodPost: a.createIncusBackup,
	})
	a.route(http.MethodDelete, apiPath+"/incus/instances/{name}/backups/{backup}", true, a.deleteIncusBackup)
	a.route(http.MethodGet, apiPath+"/incus/instances/{name}/backups/{backup}/export", true, a.exportIncusBackup)
	a.route(http.MethodPost, apiPath+"/incus/instances/{name}/proxies", true, a.addIncusProxy)
	a.route(http.MethodDelete, apiPath+"/incus/instances/{name}/proxies/{device}", true, a.removeIncusProxy)
	a.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if a.ui != nil && r.URL.Path != "/api" && !strings.HasPrefix(r.URL.Path, "/api/") {
			a.ui.ServeHTTP(w, r)
			return
		}
		writeError(w, http.StatusNotFound, "not_found", "Endpoint not found")
	})
	return a
}

// WithUI mounts a static dashboard before starting the server.
func (a *API) WithUI(handler http.Handler) *API {
	a.ui = handler
	return a
}

// WithAlerts attaches the alert store and webhook notifier.
func (a *API) WithAlerts(store *alerts.Store, notifier *alerts.Notifier) *API {
	a.alerts = store
	a.notifier = notifier
	return a
}

// WithVersion enables release update checks against GitHub Releases.
func (a *API) WithVersion(current string) *API {
	a.updates = newUpdateChecker(current)
	return a
}

// WithHostMgmt attaches read-only host operational visibility (processes, services, logs).
func (a *API) WithHostMgmt(mgmt *hostmgmt.Manager) *API {
	a.mgmt = mgmt
	return a
}

// WithDiagnostics attaches the on-demand diagnostic runner for /api/v1/diagnostics.
func (a *API) WithDiagnostics(runner *diagnostics.Runner) *API {
	a.diag = runner
	return a
}

// WithControl attaches the privileged vektor-ctl IPC client and SQLite audit store.
func (a *API) WithControl(client *ctl.Client, auditStore *audit.Store) *API {
	a.ctl = client
	a.audit = auditStore
	return a
}

// Server binds request lifetime to ctx, including streams during daemon shutdown.
func (a *API) Server(ctx context.Context) *http.Server {
	var connections atomic.Int32
	return &http.Server{
		Handler:           a,
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 * 1024,
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
		HTTP2: &http.HTTP2Config{
			MaxConcurrentStreams:          16,
			MaxReceiveBufferPerConnection: 128 * 1024,
			MaxReceiveBufferPerStream:     16 * 1024,
		},
		ErrorLog: log.New(io.Discard, "", 0),
		ConnState: func(c net.Conn, state http.ConnState) {
			switch state {
			case http.StateNew:
				if connections.Add(1) > maxConnections {
					_ = c.Close()
				}
			case http.StateClosed, http.StateHijacked:
				connections.Add(-1)
			}
		},
	}
}

func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Bounds ordinary responses and the initial SSE response.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(writeTimeout))
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	if r.TLS != nil {
		h.Set("Strict-Transport-Security", "max-age=31536000")
	}
	if a.local && !loopbackHost(r.Host) {
		closeWithUnreadBody(w, r)
		writeError(w, http.StatusForbidden, "host_denied", "Host is not allowed")
		return
	}

	// Route on the canonical path so the mux cannot redirect malformed API requests.
	if route := r.URL.Path; route == "" || path.Clean(route) != route {
		writeError(w, http.StatusBadRequest, "invalid_path", "Invalid request path")
		return
	}
	a.mux.ServeHTTP(w, r)
}

type identityKey struct{}

type identity struct {
	token       string
	session     auth.Session
	revocations <-chan struct{}
}

func (a *API) route(method, path string, private bool, handle http.HandlerFunc) {
	a.routeMethods(path, private, map[string]http.HandlerFunc{method: handle})
}

func (a *API) routeMethods(routePath string, private bool, handlers map[string]http.HandlerFunc) {
	allowed := make([]string, 0, len(handlers))
	for m := range handlers {
		allowed = append(allowed, m)
	}
	slices.Sort(allowed)
	allowHeader := strings.Join(allowed, ", ")

	a.mux.HandleFunc(routePath, func(w http.ResponseWriter, r *http.Request) {
		handle, ok := handlers[r.Method]
		if !ok {
			w.Header().Set("Allow", allowHeader)
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
			return
		}
		isStream := routePath == streamPath && r.Method == http.MethodGet
		acceptsBody := (r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch) && routePath != logoutPath
		if len(r.URL.RawQuery) > 2048 || (!acceptsBody && r.ContentLength != 0) {
			w.Header().Set("Connection", "close")
			writeError(w, http.StatusBadRequest, "invalid_request", "Unexpected body or oversized query")
			return
		}
		isMutation := r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch || r.Method == http.MethodDelete
		if isMutation && !a.validOrigin(r) {
			closeWithUnreadBody(w, r)
			writeError(w, http.StatusForbidden, "origin_denied", "Origin is not allowed")
			return
		}

		slots := a.requests
		if isStream {
			slots = a.streams
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			closeWithUnreadBody(w, r)
			rateError(w, "capacity_exceeded", 1)
			return
		}

		timeout := requestTimeout
		if strings.HasPrefix(routePath, apiPath+"/incus/") {
			if isMutation {
				if r.Method == http.MethodPost && (routePath == apiPath+"/incus/instances" || routePath == apiPath+"/incus/instances/{name}/backups") {
					timeout = 10*time.Minute + 10*time.Second
				} else {
					timeout = 75 * time.Second
				}
			} else if strings.HasSuffix(routePath, "/export") {
				timeout = 10*time.Minute + 10*time.Second
			} else {
				timeout = 15 * time.Second
			}
			_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(timeout + 5*time.Second))
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		id := identity{}
		if private {
			cookie, err := r.Cookie(sessionCookie)
			if err != nil {
				writeFailure(w, auth.ErrSession)
				return
			}
			id.token = cookie.Value
			if isStream {
				id.revocations = a.auth.Revocations()
			}
			id.session, err = a.auth.Authenticate(ctx, id.token)
			if err != nil {
				writeFailure(w, err)
				return
			}
		}
		if isStream {
			cancel()
			ctx = r.Context()
		}
		if private {
			ctx = context.WithValue(ctx, identityKey{}, id)
		}
		handle(w, r.WithContext(ctx))
	})
}

func closeWithUnreadBody(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength != 0 && r.ProtoMajor == 1 {
		w.Header().Set("Connection", "close")
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	payload, err := json.Marshal(body)
	if err != nil {
		status = http.StatusInternalServerError
		payload = []byte(`{"code":"internal_error","message":"Internal server error"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(payload)
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, apiError{Code: code, Message: message})
}

func writeFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrSession):
		writeError(w, http.StatusUnauthorized, "authentication_required", "A valid session is required")
	case database.IsUnavailable(err):
		writeError(w, http.StatusServiceUnavailable, "database_unavailable", "Database temporarily unavailable")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "Internal server error")
	}
}
