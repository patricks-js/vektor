package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	if err != nil {
		writeError(w, http.StatusBadGateway, "ctl_error", "Transient error communicating with vektor-ctl: "+err.Error())
		return
	}
	if !resp.OK {
		writeError(w, http.StatusBadGateway, "incus_error", resp.Error)
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
	if body.Protocol != "" && body.Protocol != "simplestreams" && body.Protocol != "oci" {
		invalidParams(w, "protocol must be simplestreams or oci")
		return
	}
	if !incus.ValidImageServer(body.Server) {
		invalidParams(w, "server must be a public https:// URL")
		return
	}
	if !incus.ValidImageAlias(body.Image) {
		invalidParams(w, "Invalid image alias")
		return
	}
	for k := range body.Env {
		if !incus.ValidEnvKey(k) {
			invalidParams(w, "Invalid environment variable name")
			return
		}
	}
	for _, p := range body.Proxies {
		if !incus.ValidDeviceName(p.Name) || !incus.ValidProxyAddr(p.Listen) || !incus.ValidProxyAddr(p.Connect) {
			invalidParams(w, "Invalid proxy device configuration")
			return
		}
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

func (a *API) updateIncusInstance(w http.ResponseWriter, r *http.Request) {
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
	var body incus.UpdateInstanceRequest
	if !decodeJSONBody(w, r, &body) {
		return
	}
	payload, err := json.Marshal(body)
	if err != nil {
		invalidParams(w, "Invalid update payload")
		return
	}
	a.executeControl(w, r, ctl.Request{
		Action:  "incus.instance.update",
		Target:  name,
		Payload: payload,
	}, "incus:"+name)
}

func (a *API) execIncusInstance(w http.ResponseWriter, r *http.Request) {
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
	var body incus.ExecRequest
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if body.Command == "" || len(body.Command) > 4096 {
		invalidParams(w, "command must be between 1 and 4096 characters")
		return
	}
	payload, err := json.Marshal(body)
	if err != nil {
		invalidParams(w, "Invalid exec payload")
		return
	}
	auditCmd := body.Command
	if len(auditCmd) > 120 {
		auditCmd = auditCmd[:120] + "…"
	}
	resp, ok := a.runControl(w, r, ctl.Request{
		Action:  "incus.instance.exec",
		Target:  name,
		Payload: payload,
	}, "incus:"+name, func(resp ctl.Response) string {
		return auditCmd + " (" + resp.Message + ")"
	})
	if !ok {
		return
	}
	var result incus.ExecResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		writeError(w, http.StatusBadGateway, "incus_error", "Malformed exec result")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) incusConsoleLog(w http.ResponseWriter, r *http.Request) {
	if _, ok := queryValues(w, r); !ok {
		return
	}
	name := r.PathValue("name")
	if !incus.ValidInstanceName(name) {
		invalidParams(w, "Invalid instance name")
		return
	}
	raw, ok := a.callIncusRead(w, r, ctl.Request{Action: "incus.instance.console", Target: name})
	if !ok {
		return
	}
	var out struct {
		Log string `json:"log"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		writeError(w, http.StatusBadGateway, "incus_error", "Malformed console payload")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) listIncusBackups(w http.ResponseWriter, r *http.Request) {
	if _, ok := queryValues(w, r); !ok {
		return
	}
	name := r.PathValue("name")
	if !incus.ValidInstanceName(name) {
		invalidParams(w, "Invalid instance name")
		return
	}
	raw, ok := a.callIncusRead(w, r, ctl.Request{Action: "incus.backups.list", Target: name})
	if !ok {
		return
	}
	var backups []incus.Backup
	if err := json.Unmarshal(raw, &backups); err != nil {
		writeError(w, http.StatusBadGateway, "incus_error", "Malformed backups payload")
		return
	}
	if backups == nil {
		backups = []incus.Backup{}
	}
	writeJSON(w, http.StatusOK, struct {
		Backups []incus.Backup `json:"backups"`
	}{Backups: backups})
}

func (a *API) createIncusBackup(w http.ResponseWriter, r *http.Request) {
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
	var body incus.CreateBackupRequest
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if !incus.ValidSnapshotName(body.Name) {
		invalidParams(w, "Invalid backup name")
		return
	}
	payload, err := json.Marshal(body)
	if err != nil {
		invalidParams(w, "Invalid backup payload")
		return
	}
	a.executeControl(w, r, ctl.Request{
		Action:  "incus.backup.create",
		Target:  name,
		Payload: payload,
	}, "incus:"+name+"/backup:"+body.Name)
}

func (a *API) deleteIncusBackup(w http.ResponseWriter, r *http.Request) {
	if !requireCSRF(w, r) {
		return
	}
	if _, ok := queryValues(w, r); !ok {
		return
	}
	name := r.PathValue("name")
	backup := r.PathValue("backup")
	if !incus.ValidInstanceName(name) || !incus.ValidSnapshotName(backup) {
		invalidParams(w, "Invalid instance or backup name")
		return
	}
	a.executeControl(w, r, ctl.Request{
		Action: "incus.backup.delete",
		Target: name,
		Signal: backup,
	}, "incus:"+name+"/backup:"+backup)
}

func (a *API) exportIncusBackup(w http.ResponseWriter, r *http.Request) {
	if _, ok := queryValues(w, r); !ok {
		return
	}
	name := r.PathValue("name")
	backup := r.PathValue("backup")
	if !incus.ValidInstanceName(name) || !incus.ValidSnapshotName(backup) {
		invalidParams(w, "Invalid instance or backup name")
		return
	}
	if a.ctl == nil {
		writeError(w, http.StatusServiceUnavailable, "ctl_unavailable", "vektor-ctl helper is not configured")
		return
	}
	stream, err := a.ctl.DoStream(r.Context(), ctl.Request{
		Action: "incus.backup.export",
		Target: name,
		Signal: backup,
	})
	targetLabel := "incus:" + name + "/backup:" + backup
	if errors.Is(err, ctl.ErrUnavailable) {
		a.recordAudit(r, "incus.backup.export", targetLabel, "error", "helper socket unavailable")
		writeError(w, http.StatusServiceUnavailable, "ctl_unavailable", "vektor-ctl.socket is not active on this host")
		return
	}
	if err != nil {
		a.recordAudit(r, "incus.backup.export", targetLabel, "error", err.Error())
		writeError(w, http.StatusBadGateway, "incus_error", err.Error())
		return
	}
	defer stream.Close()

	filename := fmt.Sprintf("%s-%s.tar.gz", name, backup)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.WriteHeader(http.StatusOK)
	written, copyErr := io.Copy(w, stream)
	if copyErr != nil {
		a.recordAudit(r, "incus.backup.export", targetLabel, "error", fmt.Sprintf("stream interrupted after %d bytes: %v", written, copyErr))
		return
	}
	a.recordAudit(r, "incus.backup.export", targetLabel, "ok", fmt.Sprintf("exported %s (%d bytes)", filename, written))
}

func (a *API) addIncusProxy(w http.ResponseWriter, r *http.Request) {
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
	var body incus.ProxyDevice
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if !incus.ValidDeviceName(body.Name) {
		invalidParams(w, "Invalid proxy device name")
		return
	}
	if !incus.ValidProxyAddr(body.Listen) || !incus.ValidProxyAddr(body.Connect) {
		invalidParams(w, "listen and connect must follow proto:host:port (e.g. tcp:0.0.0.0:8080)")
		return
	}
	payload, err := json.Marshal(body)
	if err != nil {
		invalidParams(w, "Invalid proxy payload")
		return
	}
	a.executeControl(w, r, ctl.Request{
		Action:  "incus.proxy.add",
		Target:  name,
		Payload: payload,
	}, "incus:"+name+"/proxy:"+body.Name)
}

func (a *API) removeIncusProxy(w http.ResponseWriter, r *http.Request) {
	if !requireCSRF(w, r) {
		return
	}
	if _, ok := queryValues(w, r); !ok {
		return
	}
	name := r.PathValue("name")
	device := r.PathValue("device")
	if !incus.ValidInstanceName(name) || !incus.ValidDeviceName(device) {
		invalidParams(w, "Invalid instance or proxy device name")
		return
	}
	a.executeControl(w, r, ctl.Request{
		Action: "incus.proxy.remove",
		Target: name,
		Signal: device,
	}, "incus:"+name+"/proxy:"+device)
}
