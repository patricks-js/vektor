package httpapi

import (
	"errors"
	"net/http"
	"os"
	"strconv"

	"vektor/internal/audit"
	"vektor/internal/ctl"
	"vektor/internal/hostmgmt"
)

func (a *API) recordAudit(r *http.Request, action, target, status, detail string) {
	if a.audit == nil {
		return
	}
	actor := "admin"
	if id, ok := r.Context().Value(identityKey{}).(identity); ok && id.session.Username != "" {
		actor = id.session.Username
	}
	_, _ = a.audit.Record(r.Context(), actor, action, target, status, detail)
}

func (a *API) executeControl(w http.ResponseWriter, r *http.Request, req ctl.Request, targetLabel string) {
	if a.ctl == nil {
		writeError(w, http.StatusServiceUnavailable, "ctl_unavailable", "vektor-ctl helper is not configured")
		return
	}
	resp, err := a.ctl.Do(r.Context(), req)
	if errors.Is(err, ctl.ErrInvalidAction) {
		invalidParams(w, "Invalid or disallowed action")
		return
	}
	if errors.Is(err, ctl.ErrUnavailable) {
		a.recordAudit(r, req.Action, targetLabel, "error", "helper socket unavailable")
		writeError(w, http.StatusServiceUnavailable, "ctl_unavailable", "vektor-ctl.socket is not active on this host")
		return
	}
	if err != nil {
		a.recordAudit(r, req.Action, targetLabel, "error", err.Error())
		writeError(w, http.StatusBadGateway, "ctl_error", "Failed to communicate with vektor-ctl")
		return
	}
	if !resp.OK {
		a.recordAudit(r, req.Action, targetLabel, "error", resp.Error)
		writeError(w, http.StatusUnprocessableEntity, "action_failed", resp.Error)
		return
	}
	a.recordAudit(r, req.Action, targetLabel, "ok", resp.Message)
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) serviceAction(w http.ResponseWriter, r *http.Request) {
	if !requireCSRF(w, r) {
		return
	}
	if _, ok := queryValues(w, r); !ok {
		return
	}
	unit := r.PathValue("name")
	if !hostmgmt.ValidUnitName(unit) {
		invalidParams(w, "Invalid service unit name")
		return
	}
	var body struct {
		Action string `json:"action"`
	}
	if !decodeJSONBody(w, r, &body) {
		return
	}
	action := "service." + body.Action
	req := ctl.Request{Action: action, Target: unit}
	if err := ctl.ValidateRequest(req); err != nil {
		invalidParams(w, "Unsupported or disallowed service action")
		return
	}
	a.executeControl(w, r, req, unit)
}

func (a *API) processSignal(w http.ResponseWriter, r *http.Request) {
	if !requireCSRF(w, r) {
		return
	}
	if _, ok := queryValues(w, r); !ok {
		return
	}
	pidStr := r.PathValue("pid")
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 1 || pid == os.Getpid() {
		invalidParams(w, "Invalid or protected PID")
		return
	}
	var body struct {
		Signal string `json:"signal"`
	}
	if !decodeJSONBody(w, r, &body) {
		return
	}
	req := ctl.Request{Action: "process.signal", Target: pidStr, Signal: body.Signal}
	if err := ctl.ValidateRequest(req); err != nil {
		invalidParams(w, "signal must be TERM or KILL")
		return
	}
	a.executeControl(w, r, req, "pid:"+pidStr)
}

func (a *API) hostPower(w http.ResponseWriter, r *http.Request) {
	if !requireCSRF(w, r) {
		return
	}
	if _, ok := queryValues(w, r); !ok {
		return
	}
	var body struct {
		Action string `json:"action"`
	}
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if body.Action != "reboot" && body.Action != "poweroff" {
		invalidParams(w, "action must be reboot or poweroff")
		return
	}
	req := ctl.Request{Action: "host." + body.Action, Target: "host:local"}
	a.executeControl(w, r, req, "host:local")
}

func (a *API) listAudit(w http.ResponseWriter, r *http.Request) {
	if a.audit == nil {
		writeJSON(w, http.StatusOK, struct {
			Entries []audit.Entry `json:"entries"`
		}{[]audit.Entry{}})
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
		if err != nil || limit < 1 || limit > audit.MaxEntries {
			invalidParams(w, "limit must be between 1 and 500")
			return
		}
	}
	entries, err := a.audit.List(r.Context(), limit)
	if err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Entries []audit.Entry `json:"entries"`
	}{entries})
}
