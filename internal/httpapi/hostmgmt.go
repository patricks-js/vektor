package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"vektor/internal/hostmgmt"
)

func (a *API) ensureHostMgmt(w http.ResponseWriter) bool {
	if a.mgmt == nil {
		writeError(w, http.StatusServiceUnavailable, "hostmgmt_unavailable", "Host management is not configured")
		return false
	}
	return true
}

func (a *API) processes(w http.ResponseWriter, r *http.Request) {
	if !a.ensureHostMgmt(w) {
		return
	}
	q, ok := queryValues(w, r, "sort", "limit")
	if !ok {
		return
	}
	sortBy := q.Get("sort")
	if sortBy != "" && sortBy != "cpu" && sortBy != "memory" {
		invalidParams(w, "sort must be cpu or memory")
		return
	}
	limit := 50
	if q.Has("limit") {
		var err error
		limit, err = strconv.Atoi(q.Get("limit"))
		if err != nil || limit < 1 || limit > 200 {
			invalidParams(w, "limit must be between 1 and 200")
			return
		}
	}
	items, err := a.mgmt.ListProcesses(r.Context(), sortBy, limit)
	if err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Processes []hostmgmt.Process `json:"processes"`
	}{items})
}

func (a *API) services(w http.ResponseWriter, r *http.Request) {
	if !a.ensureHostMgmt(w) {
		return
	}
	q, ok := queryValues(w, r, "state")
	if !ok {
		return
	}
	state := q.Get("state")
	if state != "" && state != "all" && state != "active" && state != "failed" && state != "inactive" {
		invalidParams(w, "state must be all, active, failed, or inactive")
		return
	}
	items, err := a.mgmt.ListServices(r.Context(), state)
	if errors.Is(err, hostmgmt.ErrPermission) {
		writeError(w, http.StatusForbidden, "journald_permission_denied", "vektor user lacks systemd-journal group permission to read logs")
		return
	}
	if errors.Is(err, hostmgmt.ErrUnavailable) {
		writeError(w, http.StatusServiceUnavailable, "systemd_unavailable", "systemd is not available on this host")
		return
	}
	if err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Services []hostmgmt.Service `json:"services"`
	}{items})
}

func (a *API) serviceLogs(w http.ResponseWriter, r *http.Request) {
	if !a.ensureHostMgmt(w) {
		return
	}
	q, ok := queryValues(w, r, "lines", "priority")
	if !ok {
		return
	}
	unit := r.PathValue("name")
	if !hostmgmt.ValidUnitName(unit) {
		invalidParams(w, "Invalid service unit name")
		return
	}
	lines := 100
	if q.Has("lines") {
		var err error
		lines, err = strconv.Atoi(q.Get("lines"))
		if err != nil || lines < 1 || lines > 500 {
			invalidParams(w, "lines must be between 1 and 500")
			return
		}
	}
	priority := q.Get("priority")
	if priority != "" && priority != "all" && priority != "info" && priority != "warning" && priority != "err" {
		invalidParams(w, "priority must be all, info, warning, or err")
		return
	}
	entries, err := a.mgmt.ServiceLogs(r.Context(), unit, lines, priority)
	if errors.Is(err, hostmgmt.ErrInvalidUnit) {
		invalidParams(w, "Invalid service unit name")
		return
	}
	if errors.Is(err, hostmgmt.ErrPermission) {
		writeError(w, http.StatusForbidden, "journald_permission_denied", "vektor user lacks systemd-journal group permission to read logs")
		return
	}
	if errors.Is(err, hostmgmt.ErrUnavailable) {
		writeError(w, http.StatusServiceUnavailable, "journald_unavailable", "journald is not available on this host")
		return
	}
	if err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Unit    string              `json:"unit"`
		Entries []hostmgmt.LogEntry `json:"entries"`
	}{unit, entries})
}
