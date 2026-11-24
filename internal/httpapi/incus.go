package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"vektor/internal/ctl"
	"vektor/internal/incus"
)

func (a *API) callIncusRead(w http.ResponseWriter, r *http.Request, req ctl.Request) (json.RawMessage, bool) {
	if a.ctl == nil {
		writeError(w, http.StatusServiceUnavailable, "ctl_unavailable", "vektor-ctl helper is not configured")
		return nil, false
	}
	resp, err := a.ctl.Do(r.Context(), req)
	if errors.Is(err, ctl.ErrUnavailable) {
		writeError(w, http.StatusServiceUnavailable, "ctl_unavailable", "vektor-ctl.socket is not active on this host")
		return nil, false
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "ctl_error", "Failed to communicate with vektor-ctl")
		return nil, false
	}
	if !resp.OK {
		writeError(w, http.StatusBadGateway, "incus_error", resp.Error)
		return nil, false
	}
	return resp.Data, true
}

func (a *API) incusStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := queryValues(w, r); !ok {
		return
	}
	if a.ctl == nil {
		writeJSON(w, http.StatusOK, incus.Status{
			Available: false,
			Reason:    "vektor-ctl helper is not configured",
		})
		return
	}
	resp, err := a.ctl.Do(r.Context(), ctl.Request{Action: "incus.status"})
	if errors.Is(err, ctl.ErrUnavailable) {
		writeJSON(w, http.StatusOK, incus.Status{
			Available: false,
			Reason:    "vektor-ctl.socket is not active on this host",
		})
		return
	}
	if err != nil || !resp.OK {
		reason := "Failed to query Incus"
		if err != nil {
			reason = err.Error()
		} else if resp.Error != "" {
			reason = resp.Error
		}
		writeJSON(w, http.StatusOK, incus.Status{Available: false, Reason: reason})
		return
	}
	var st incus.Status
	if err := json.Unmarshal(resp.Data, &st); err != nil {
		writeJSON(w, http.StatusOK, incus.Status{Available: false, Reason: "Malformed Incus status response"})
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (a *API) listIncusInstances(w http.ResponseWriter, r *http.Request) {
	if _, ok := queryValues(w, r); !ok {
		return
	}
	raw, ok := a.callIncusRead(w, r, ctl.Request{Action: "incus.instances.list"})
	if !ok {
		return
	}
	var instances []incus.Instance
	if err := json.Unmarshal(raw, &instances); err != nil {
		writeError(w, http.StatusBadGateway, "incus_error", "Malformed instances payload")
		return
	}
	if instances == nil {
		instances = []incus.Instance{}
	}
	writeJSON(w, http.StatusOK, struct {
		Instances []incus.Instance `json:"instances"`
	}{Instances: instances})
}

func (a *API) getIncusInstance(w http.ResponseWriter, r *http.Request) {
	if _, ok := queryValues(w, r); !ok {
		return
	}
	name := r.PathValue("name")
	if !incus.ValidInstanceName(name) {
		invalidParams(w, "Invalid instance name")
		return
	}
	raw, ok := a.callIncusRead(w, r, ctl.Request{Action: "incus.instance.get", Target: name})
	if !ok {
		return
	}
	var inst incus.Instance
	if err := json.Unmarshal(raw, &inst); err != nil {
		writeError(w, http.StatusBadGateway, "incus_error", "Malformed instance payload")
		return
	}
	writeJSON(w, http.StatusOK, inst)
}

func (a *API) createIncusInstance(w http.ResponseWriter, r *http.Request) {
	if !requireCSRF(w, r) {
		return
	}
	if _, ok := queryValues(w, r); !ok {
		return
	}
	var body incus.CreateInstanceRequest
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if !incus.ValidInstanceName(body.Name) {
		invalidParams(w, "Invalid instance name (use letters, digits, and hyphens)")
		return
	}
	if body.Type == "" {
		body.Type = "container"
	}
	if body.Type != "container" && body.Type != "virtual-machine" {
		invalidParams(w, "type must be container or virtual-machine")
		return
	}
	if !incus.ValidImageAlias(body.Image) {
		invalidParams(w, "Invalid image alias")
		return
	}
	payload, err := json.Marshal(body)
	if err != nil {
		invalidParams(w, "Invalid request payload")
		return
	}
	a.executeControl(w, r, ctl.Request{
		Action:  "incus.instance.create",
		Target:  body.Name,
		Payload: payload,
	}, "incus:"+body.Name)
}

func (a *API) deleteIncusInstance(w http.ResponseWriter, r *http.Request) {
	if !requireCSRF(w, r) {
		return
	}
	if _, ok := queryValues(w, r); !ok {
		return
	}
	name := r.PathValue("name")
	if !incus.ValidInstanceName(name) {
		invalidParams(w, "Invalid instance name")
		return
	}
	a.executeControl(w, r, ctl.Request{
		Action: "incus.instance.delete",
		Target: name,
	}, "incus:"+name)
}

func (a *API) incusInstanceState(w http.ResponseWriter, r *http.Request) {
	if !requireCSRF(w, r) {
		return
	}
	if _, ok := queryValues(w, r); !ok {
		return
	}
	name := r.PathValue("name")
	if !incus.ValidInstanceName(name) {
		invalidParams(w, "Invalid instance name")
		return
	}
	var body incus.StateActionRequest
	if !decodeJSONBody(w, r, &body) {
		return
	}
	action := "incus.instance." + body.Action
	req := ctl.Request{Action: action, Target: name}
	if err := ctl.ValidateRequest(req); err != nil {
		invalidParams(w, "Unsupported instance state action")
		return
	}
	a.executeControl(w, r, req, "incus:"+name)
}

func (a *API) listIncusSnapshots(w http.ResponseWriter, r *http.Request) {
	if _, ok := queryValues(w, r); !ok {
		return
	}
	name := r.PathValue("name")
	if !incus.ValidInstanceName(name) {
		invalidParams(w, "Invalid instance name")
		return
	}
	raw, ok := a.callIncusRead(w, r, ctl.Request{Action: "incus.snapshots.list", Target: name})
	if !ok {
		return
	}
	var snaps []incus.Snapshot
	if err := json.Unmarshal(raw, &snaps); err != nil {
		writeError(w, http.StatusBadGateway, "incus_error", "Malformed snapshots payload")
		return
	}
	if snaps == nil {
		snaps = []incus.Snapshot{}
	}
	writeJSON(w, http.StatusOK, struct {
		Snapshots []incus.Snapshot `json:"snapshots"`
	}{Snapshots: snaps})
}

func (a *API) createIncusSnapshot(w http.ResponseWriter, r *http.Request) {
	if !requireCSRF(w, r) {
		return
	}
	if _, ok := queryValues(w, r); !ok {
		return
	}
	name := r.PathValue("name")
	if !incus.ValidInstanceName(name) {
		invalidParams(w, "Invalid instance name")
		return
	}
	var body incus.CreateSnapshotRequest
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if !incus.ValidSnapshotName(body.Name) {
		invalidParams(w, "Invalid snapshot name")
		return
	}
	payload, err := json.Marshal(body)
	if err != nil {
		invalidParams(w, "Invalid snapshot payload")
		return
	}
	a.executeControl(w, r, ctl.Request{
		Action:  "incus.snapshot.create",
		Target:  name,
		Payload: payload,
	}, "incus:"+name+"/"+body.Name)
}

func (a *API) restoreIncusSnapshot(w http.ResponseWriter, r *http.Request) {
	if !requireCSRF(w, r) {
		return
	}
	if _, ok := queryValues(w, r); !ok {
		return
	}
	name := r.PathValue("name")
	snap := r.PathValue("snap")
	if !incus.ValidInstanceName(name) || !incus.ValidSnapshotName(snap) {
		invalidParams(w, "Invalid instance or snapshot name")
		return
	}
	a.executeControl(w, r, ctl.Request{
		Action: "incus.snapshot.restore",
		Target: name,
		Signal: snap,
	}, "incus:"+name+"/"+snap)
}

func (a *API) deleteIncusSnapshot(w http.ResponseWriter, r *http.Request) {
	if !requireCSRF(w, r) {
		return
	}
	if _, ok := queryValues(w, r); !ok {
		return
	}
	name := r.PathValue("name")
	snap := r.PathValue("snap")
	if !incus.ValidInstanceName(name) || !incus.ValidSnapshotName(snap) {
		invalidParams(w, "Invalid instance or snapshot name")
		return
	}
	a.executeControl(w, r, ctl.Request{
		Action: "incus.snapshot.delete",
		Target: name,
		Signal: snap,
	}, "incus:"+name+"/"+snap)
}

func (a *API) listIncusResources(w http.ResponseWriter, r *http.Request) {
	if _, ok := queryValues(w, r); !ok {
		return
	}
	raw, ok := a.callIncusRead(w, r, ctl.Request{Action: "incus.resources.list"})
	if !ok {
		return
	}
	var res incus.Resources
	if err := json.Unmarshal(raw, &res); err != nil {
		writeError(w, http.StatusBadGateway, "incus_error", "Malformed resources payload")
		return
	}
	writeJSON(w, http.StatusOK, res)
}
