package httpapi

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"unicode/utf8"

	"vektor/internal/alerts"
)

const maxAlertBody = 16 * 1024

func requireCSRF(w http.ResponseWriter, r *http.Request) bool {
	id := r.Context().Value(identityKey{}).(identity)
	tokens := r.Header.Values("X-CSRF-Token")
	if len(tokens) != 1 || subtle.ConstantTimeCompare([]byte(tokens[0]), []byte(id.session.CSRFToken)) != 1 {
		writeError(w, http.StatusForbidden, "csrf_denied", "A valid CSRF token is required")
		return false
	}
	return true
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		closeWithUnreadBody(w, r)
		writeError(w, http.StatusBadRequest, "invalid_request", "Request requires application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAlertBody)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "Payload exceeds limit")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid_request", "Could not read request payload")
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if !utf8.Valid(data) || dec.Decode(dst) != nil || dec.Decode(new(any)) != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON payload")
		return false
	}
	return true
}

func writeAlertError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, alerts.ErrInvalid):
		invalidParams(w, "Invalid alert configuration")
	case errors.Is(err, alerts.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Alert resource not found")
	case errors.Is(err, alerts.ErrLimit):
		writeError(w, http.StatusUnprocessableEntity, "limit_reached", "Alert resource limit reached")
	default:
		writeFailure(w, err)
	}
}

func (a *API) ensureAlerts(w http.ResponseWriter) bool {
	if a.alerts == nil {
		writeError(w, http.StatusServiceUnavailable, "alerts_unavailable", "Alerts are not configured")
		return false
	}
	return true
}

func (a *API) listAlertRules(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAlerts(w) {
		return
	}
	if _, ok := queryValues(w, r); !ok {
		return
	}
	rules, err := a.alerts.ListRules(r.Context())
	if err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Rules []alerts.Rule `json:"rules"`
	}{rules})
}

func (a *API) createAlertRule(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAlerts(w) || !requireCSRF(w, r) {
		return
	}
	if _, ok := queryValues(w, r); !ok {
		return
	}
	var in alerts.RuleInput
	if !decodeJSONBody(w, r, &in) {
		return
	}
	rule, err := a.alerts.CreateRule(r.Context(), in)
	if err != nil {
		writeAlertError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, rule)
}

func (a *API) updateAlertRule(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAlerts(w) || !requireCSRF(w, r) {
		return
	}
	if _, ok := queryValues(w, r); !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		invalidParams(w, "Invalid alert rule ID")
		return
	}
	var in alerts.RuleInput
	if !decodeJSONBody(w, r, &in) {
		return
	}
	rule, err := a.alerts.UpdateRule(r.Context(), id, in)
	if err != nil {
		writeAlertError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

func (a *API) deleteAlertRule(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAlerts(w) || !requireCSRF(w, r) {
		return
	}
	if _, ok := queryValues(w, r); !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		invalidParams(w, "Invalid alert rule ID")
		return
	}
	if err := a.alerts.DeleteRule(r.Context(), id); err != nil {
		writeAlertError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		OK bool `json:"ok"`
	}{true})
}

func (a *API) listAlertChannels(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAlerts(w) {
		return
	}
	if _, ok := queryValues(w, r); !ok {
		return
	}
	channels, err := a.alerts.ListChannels(r.Context())
	if err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Channels []alerts.Channel `json:"channels"`
	}{channels})
}

func (a *API) createAlertChannel(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAlerts(w) || !requireCSRF(w, r) {
		return
	}
	if _, ok := queryValues(w, r); !ok {
		return
	}
	var in alerts.ChannelInput
	if !decodeJSONBody(w, r, &in) {
		return
	}
	ch, err := a.alerts.CreateChannel(r.Context(), in)
	if err != nil {
		writeAlertError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, ch)
}

func (a *API) deleteAlertChannel(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAlerts(w) || !requireCSRF(w, r) {
		return
	}
	if _, ok := queryValues(w, r); !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		invalidParams(w, "Invalid alert channel ID")
		return
	}
	if err := a.alerts.DeleteChannel(r.Context(), id); err != nil {
		writeAlertError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		OK bool `json:"ok"`
	}{true})
}

func (a *API) testAlertChannel(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAlerts(w) || !requireCSRF(w, r) {
		return
	}
	if a.notifier == nil {
		writeError(w, http.StatusServiceUnavailable, "alerts_unavailable", "Alert notifier is not configured")
		return
	}
	if _, ok := queryValues(w, r); !ok {
		return
	}
	var in struct {
		URL string `json:"url"`
	}
	if !decodeJSONBody(w, r, &in) {
		return
	}
	if err := alerts.ValidateWebhookURL(in.URL); err != nil {
		invalidParams(w, "Invalid webhook URL")
		return
	}
	if err := a.notifier.TestURL(r.Context(), in.URL); err != nil {
		writeError(w, http.StatusBadGateway, "webhook_failed", "Webhook delivery failed")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		OK bool `json:"ok"`
	}{true})
}

func (a *API) listAlertEvents(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAlerts(w) {
		return
	}
	q, ok := queryValues(w, r, "limit")
	if !ok {
		return
	}
	limit := 50
	if q.Has("limit") {
		var err error
		limit, err = strconv.Atoi(q.Get("limit"))
		if err != nil || limit < 1 || limit > alerts.MaxEvents {
			invalidParams(w, "limit must be between 1 and 200")
			return
		}
	}
	events, err := a.alerts.ListEvents(r.Context(), limit)
	if err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Events []alerts.Event `json:"events"`
	}{events})
}
