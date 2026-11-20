package httpapi

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"vektor/internal/auth"
)

const (
	maxLoginBody = 8 * 1024
	loginWindow  = time.Minute
	perIPLogins  = 5
	totalLogins  = 20
)

func loopbackHost(authority string) bool {
	host, _, err := net.SplitHostPort(authority)
	if err != nil {
		host = strings.Trim(authority, "[]")
	}
	if strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.Unmap().IsLoopback()
}

type loginLimiter struct {
	mu      sync.Mutex
	end     time.Time
	total   int
	perPeer map[netip.Addr]int
}

func (l *loginLimiter) allow(remote string, now time.Time) (bool, int) {
	host, _, _ := net.SplitHostPort(remote)
	ip, _ := netip.ParseAddr(host)
	ip = ip.Unmap().WithZone("")

	l.mu.Lock()
	defer l.mu.Unlock()
	if !now.Before(l.end) {
		l.end = now.Add(loginWindow)
		l.total = 0
		l.perPeer = make(map[netip.Addr]int)
	}
	if l.total >= totalLogins || l.perPeer[ip] >= perIPLogins {
		return false, int((l.end.Sub(now) + time.Second - 1) / time.Second)
	}
	l.total++
	l.perPeer[ip]++
	return true, 0
}

func rateError(w http.ResponseWriter, code string, retrySeconds int) {
	w.Header().Set("Retry-After", strconv.Itoa(retrySeconds))
	writeError(w, http.StatusTooManyRequests, code, "Too many requests, try again later")
}

func (a *API) validOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origins := r.Header.Values("Origin")
	if len(origins) == 0 {
		// A command-line client can omit Origin. Browsers still need JSON for
		// login or a session-bound custom header for logout.
		return true
	}
	if len(origins) != 1 {
		return false
	}
	want := a.cfg.PublicOrigin
	if want == "" {
		scheme := "http://"
		if r.TLS != nil {
			scheme = "https://"
		}
		want = scheme + r.Host
	}
	return origins[0] == want
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		closeWithUnreadBody(w, r)
		invalidParams(w, "Login does not accept query parameters")
		return
	}
	if allowed, retry := a.logins.allow(r.RemoteAddr, time.Now()); !allowed {
		closeWithUnreadBody(w, r)
		rateError(w, "login_rate_limited", retry)
		return
	}
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		closeWithUnreadBody(w, r)
		writeError(w, http.StatusBadRequest, "invalid_request", "Login requires application/json")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxLoginBody)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "Login payload exceeds 8192 bytes")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_request", "Could not read login payload")
		return
	}
	defer clear(data)

	var credentials struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if !utf8.Valid(data) || dec.Decode(&credentials) != nil || dec.Decode(new(any)) != io.EOF ||
		credentials.Username == "" || len(credentials.Username) > 128 ||
		credentials.Password == "" || len(credentials.Password) > auth.MaxPasswordBytes {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid login payload")
		return
	}

	select {
	case a.password <- struct{}{}:
		defer func() { <-a.password }()
	default:
		rateError(w, "login_busy", 1)
		return
	}
	if r.Context().Err() != nil {
		return
	}
	password := []byte(credentials.Password)
	defer clear(password)
	token, session, err := a.auth.Login(r.Context(), credentials.Username, password)
	switch {
	case errors.Is(err, auth.ErrCredentials):
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Invalid username or password")
	case errors.Is(err, auth.ErrSessionLimit):
		rateError(w, "session_limit", 60)
	case err != nil:
		writeFailure(w, err)
	default:
		a.setCookie(w, r, token, session.ExpiresAt, int(a.cfg.SessionTTL/time.Second))
		writeJSON(w, http.StatusOK, session)
	}
}

func (a *API) setCookie(w http.ResponseWriter, r *http.Request, token string, expiry time.Time, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     apiPath,
		Expires:  expiry,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   r.TLS != nil || strings.HasPrefix(a.cfg.PublicOrigin, "https://"),
		SameSite: http.SameSiteStrictMode,
	})
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	if _, ok := queryValues(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, r.Context().Value(identityKey{}).(identity).session)
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if _, ok := queryValues(w, r); !ok {
		return
	}
	id := r.Context().Value(identityKey{}).(identity)
	tokens := r.Header.Values("X-CSRF-Token")
	if len(tokens) != 1 || subtle.ConstantTimeCompare([]byte(tokens[0]), []byte(id.session.CSRFToken)) != 1 {
		writeError(w, http.StatusForbidden, "csrf_denied", "A valid CSRF token is required")
		return
	}
	if err := a.auth.Revoke(r.Context(), id.token); err != nil {
		writeFailure(w, err)
		return
	}
	a.setCookie(w, r, "", time.Unix(1, 0).UTC(), -1)
	writeJSON(w, http.StatusOK, struct {
		OK bool `json:"ok"`
	}{true})
}
