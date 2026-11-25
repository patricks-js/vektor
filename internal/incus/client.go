package incus

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	defaultImagesServer = "https://images.linuxcontainers.org"
	defaultOCIServer    = "https://registry-1.docker.io"
	maxResponseBytes    = 8 << 20 // 8 MiB
)

type Client struct {
	socketPath string
	httpClient *http.Client
}

func NewClient(socketPath string) *Client {
	if strings.TrimSpace(socketPath) == "" {
		socketPath = DefaultSocketPath
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		},
		DisableCompression: true,
	}
	return &Client{
		socketPath: socketPath,
		httpClient: &http.Client{Transport: transport},
	}
}

type apiEnvelope struct {
	Type       string          `json:"type"`
	Status     string          `json:"status"`
	StatusCode int             `json:"status_code"`
	Operation  string          `json:"operation"`
	ErrorCode  int             `json:"error_code"`
	Error      string          `json:"error"`
	Metadata   json.RawMessage `json:"metadata"`
}

func (c *Client) do(ctx context.Context, method, endpoint string, body any) (apiEnvelope, error) {
	var bodyReader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return apiEnvelope{}, err
		}
		bodyReader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://unix"+endpoint, bodyReader)
	if err != nil {
		return apiEnvelope{}, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "no such file or directory") || strings.Contains(err.Error(), "connection refused") {
			return apiEnvelope{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		return apiEnvelope{}, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return apiEnvelope{}, err
	}
	var env apiEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return apiEnvelope{}, fmt.Errorf("incus malformed response (HTTP %d): %w", resp.StatusCode, err)
	}
	if env.Type == "error" || resp.StatusCode >= 400 {
		msg := env.Error
		if msg == "" {
			msg = fmt.Sprintf("incus request failed (HTTP %d)", resp.StatusCode)
		}
		return env, errors.New(msg)
	}
	return env, nil
}

func (c *Client) doRaw(ctx context.Context, method, endpoint string, maxBytes int64) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, "http://unix"+endpoint, nil)
	if err != nil {
		return "", 0, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "no such file or directory") || strings.Contains(err.Error(), "connection refused") {
			return "", 0, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		return "", 0, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return "", resp.StatusCode, err
	}
	return string(raw), resp.StatusCode, nil
}

func (c *Client) waitOperationMetadata(ctx context.Context, opPath string) (json.RawMessage, error) {
	if opPath == "" {
		return nil, nil
	}
	if !strings.HasPrefix(opPath, "/") {
		opPath = "/1.0/operations/" + url.PathEscape(opPath)
	}
	// timeout=-1 lets the Go context govern the operation wait deadline (e.g. large VM image pulls).
	env, err := c.do(ctx, http.MethodGet, opPath+"/wait?timeout=-1", nil)
	if err != nil {
		return nil, err
	}
	var op struct {
		Status     string          `json:"status"`
		StatusCode int             `json:"status_code"`
		Err        string          `json:"err"`
		Metadata   json.RawMessage `json:"metadata"`
	}
	if len(env.Metadata) > 0 && json.Unmarshal(env.Metadata, &op) == nil {
		if op.Err != "" {
			return nil, errors.New(op.Err)
		}
		if op.StatusCode >= 400 {
			return nil, fmt.Errorf("incus operation failed: %s", op.Status)
		}
		return op.Metadata, nil
	}
	return nil, nil
}

func (c *Client) waitOperation(ctx context.Context, opPath string) error {
	_, err := c.waitOperationMetadata(ctx, opPath)
	return err
}

func (c *Client) Status(ctx context.Context) (Status, error) {
	env, err := c.do(ctx, http.MethodGet, "/1.0", nil)
	if err != nil {
		if errors.Is(err, ErrUnavailable) {
			return Status{
				Available: false,
				Reason:    fmt.Sprintf("Incus socket not reachable at %s", c.socketPath),
			}, nil
		}
		return Status{Available: false, Reason: err.Error()}, nil
	}
	var srv struct {
		APIExtensions []string `json:"api_extensions"`
		Environment   struct {
			ServerVersion string `json:"server_version"`
			KernelVersion string `json:"kernel_version"`
			Driver        string `json:"driver"`
			Storage       string `json:"storage"`
		} `json:"environment"`
	}
	_ = json.Unmarshal(env.Metadata, &srv)

	ociSupported := false
	for _, ext := range srv.APIExtensions {
		if ext == "instance_oci" {
			ociSupported = true
			break
		}
	}

	st := Status{
		Available:      true,
		ServerVersion:  srv.Environment.ServerVersion,
		KernelVersion:  srv.Environment.KernelVersion,
		Drivers:        splitPipe(srv.Environment.Driver),
		StorageDrivers: splitPipe(srv.Environment.Storage),
		OCISupported:   ociSupported,
	}

	if list, err := c.ListInstances(ctx); err == nil {
		st.InstanceCount = len(list)
		for _, inst := range list {
			if strings.EqualFold(inst.Status, "Running") {
				st.RunningCount++
			}
			if inst.Type == "virtual-machine" {
				st.VMCount++
			} else {
				st.ContainerCount++
				if inst.OCI {
					st.OCICount++
				}
			}
		}
	}
	return st, nil
}

type rawInstance struct {
	Name            string                       `json:"name"`
	Type            string                       `json:"type"`
	Status          string                       `json:"status"`
	Description     string                       `json:"description"`
	Architecture    string                       `json:"architecture"`
	Ephemeral       bool                         `json:"ephemeral"`
	Stateful        bool                         `json:"stateful"`
	Profiles        []string                     `json:"profiles"`
	Config          map[string]string            `json:"config"`
	ExpandedCfg     map[string]string            `json:"expanded_config"`
	Devices         map[string]map[string]string `json:"devices"`
	ExpandedDevices map[string]map[string]string `json:"expanded_devices"`
	CreatedAt       time.Time                    `json:"created_at"`
	LastUsedAt      time.Time                    `json:"last_used_at"`
	Snapshots       []json.RawMessage            `json:"snapshots"`
	State           *rawInstanceState            `json:"state"`
}

type rawInstanceState struct {
	Status    string `json:"status"`
	PID       int64  `json:"pid"`
	Processes int64  `json:"processes"`
	CPU       struct {
		Usage int64 `json:"usage"`
	} `json:"cpu"`
	Memory struct {
		Usage     int64 `json:"usage"`
		UsagePeak int64 `json:"usage_peak"`
	} `json:"memory"`
	Disk map[string]struct {
		Usage int64 `json:"usage"`
	} `json:"disk"`
	Network map[string]struct {
		Addresses []struct {
			Family  string `json:"family"`
			Address string `json:"address"`
			Scope   string `json:"scope"`
		} `json:"addresses"`
	} `json:"network"`
}

func mapRawInstance(r rawInstance) Instance {
	cfg := r.ExpandedCfg
	if len(cfg) == 0 {
		cfg = r.Config
	}
	instType := r.Type
	if instType == "" {
		instType = "container"
	}
	isOCI := cfg["oci.entrypoint"] != "" || cfg["oci.cwd"] != "" || cfg["oci.uid"] != "" || strings.EqualFold(cfg["image.type"], "oci")
	var envMap map[string]string
	for k, v := range r.Config {
		if strings.HasPrefix(k, "environment.") {
			if envMap == nil {
				envMap = make(map[string]string)
			}
			envMap[strings.TrimPrefix(k, "environment.")] = v
		}
	}
	devs := r.ExpandedDevices
	if len(devs) == 0 {
		devs = r.Devices
	}
	var proxies []ProxyDevice
	for devName, d := range devs {
		if d["type"] == "proxy" {
			proxies = append(proxies, ProxyDevice{
				Name:    devName,
				Listen:  d["listen"],
				Connect: d["connect"],
			})
		}
	}
	sort.Slice(proxies, func(i, j int) bool { return proxies[i].Name < proxies[j].Name })

	inst := Instance{
		Name:           r.Name,
		Type:           instType,
		OCI:            isOCI,
		Status:         r.Status,
		Description:    r.Description,
		Architecture:   r.Architecture,
		Ephemeral:      r.Ephemeral,
		Stateful:       r.Stateful,
		Profiles:       nonNilStrings(r.Profiles),
		CPULimit:       cfg["limits.cpu"],
		MemoryLimit:    cfg["limits.memory"],
		Autostart:      strings.EqualFold(cfg["boot.autostart"], "true") || cfg["boot.autostart"] == "1",
		Env:            envMap,
		Proxies:        proxies,
		IPv4:           []string{},
		IPv6:           []string{},
		SnapshotsCount: len(r.Snapshots),
	}
	if !r.CreatedAt.IsZero() {
		inst.CreatedAt = r.CreatedAt.UTC().Format(time.RFC3339)
	}
	if !r.LastUsedAt.IsZero() {
		inst.LastUsedAt = r.LastUsedAt.UTC().Format(time.RFC3339)
	}
	if r.State != nil {
		if r.State.Status != "" {
			inst.Status = r.State.Status
		}
		inst.PID = r.State.PID
		inst.Processes = r.State.Processes
		inst.CPUUsageNs = r.State.CPU.Usage
		inst.MemoryUsageBytes = r.State.Memory.Usage
		inst.MemoryPeakBytes = r.State.Memory.UsagePeak
		if rootDisk, ok := r.State.Disk["root"]; ok {
			inst.DiskUsageBytes = rootDisk.Usage
		} else {
			for _, d := range r.State.Disk {
				inst.DiskUsageBytes += d.Usage
			}
		}
		for iface, netState := range r.State.Network {
			if iface == "lo" {
				continue
			}
			for _, addr := range netState.Addresses {
				if addr.Scope != "global" {
					continue
				}
				if addr.Family == "inet" {
					inst.IPv4 = append(inst.IPv4, addr.Address)
				} else if addr.Family == "inet6" {
					inst.IPv6 = append(inst.IPv6, addr.Address)
				}
			}
		}
		sort.Strings(inst.IPv4)
		sort.Strings(inst.IPv6)
	}
	return inst
}

func (c *Client) ListInstances(ctx context.Context) ([]Instance, error) {
	env, err := c.do(ctx, http.MethodGet, "/1.0/instances?recursion=2", nil)
	if err != nil {
		return nil, err
	}
	var raw []rawInstance
	if err := json.Unmarshal(env.Metadata, &raw); err != nil {
		return nil, err
	}
	out := make([]Instance, 0, len(raw))
	for _, r := range raw {
		out = append(out, mapRawInstance(r))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (c *Client) GetInstance(ctx context.Context, name string) (Instance, error) {
	if !ValidInstanceName(name) {
		return Instance{}, ErrInvalidName
	}
	env, err := c.do(ctx, http.MethodGet, "/1.0/instances/"+url.PathEscape(name)+"?recursion=1", nil)
	if err != nil {
		return Instance{}, err
	}
	var raw rawInstance
	if err := json.Unmarshal(env.Metadata, &raw); err != nil {
		return Instance{}, err
	}
	return mapRawInstance(raw), nil
}

func (c *Client) ChangeState(ctx context.Context, name string, action string) error {
	if !ValidInstanceName(name) {
		return ErrInvalidName
	}
	force := false
	incusAction := action
	switch action {
	case "start", "stop", "restart", "freeze", "unfreeze":
	case "force_stop":
		incusAction = "stop"
		force = true
	default:
		return fmt.Errorf("unsupported instance action %q", action)
	}
	payload := map[string]any{
		"action":   incusAction,
		"timeout":  30,
		"force":    force,
		"stateful": false,
	}
	env, err := c.do(ctx, http.MethodPut, "/1.0/instances/"+url.PathEscape(name)+"/state", payload)
	if err != nil {
		return err
	}
	return c.waitOperation(ctx, env.Operation)
}

func (c *Client) CreateInstance(ctx context.Context, req CreateInstanceRequest) error {
	if !ValidInstanceName(req.Name) {
		return ErrInvalidName
	}
	instType := req.Type
	if instType == "" {
		instType = "container"
	}
	if instType != "container" && instType != "virtual-machine" {
		return fmt.Errorf("invalid instance type %q", instType)
	}
	if !ValidImageAlias(req.Image) {
		return errors.New("invalid image alias")
	}

	alias := strings.TrimSpace(req.Image)
	protocol := strings.TrimSpace(req.Protocol)
	server := strings.TrimSpace(req.Server)
	if !ValidImageServer(server) {
		return errors.New("image server must be a public https:// URL")
	}

	var source map[string]any
	switch {
	case protocol == "oci" || strings.HasPrefix(alias, "docker:") || strings.HasPrefix(alias, "ghcr:"):
		if strings.HasPrefix(alias, "docker:") {
			alias = strings.TrimPrefix(alias, "docker:")
			if server == "" {
				server = defaultOCIServer
			}
		} else if strings.HasPrefix(alias, "ghcr:") {
			alias = strings.TrimPrefix(alias, "ghcr:")
			if server == "" {
				server = "https://ghcr.io"
			}
		}
		if server == "" {
			server = defaultOCIServer
		}
		// Docker Hub official images require the library/ prefix for Incus OCI pulls.
		if server == defaultOCIServer && !strings.Contains(alias, "/") {
			alias = "library/" + alias
		}
		instType = "container"
		source = map[string]any{
			"type":     "image",
			"mode":     "pull",
			"protocol": "oci",
			"server":   server,
			"alias":    alias,
		}
	case strings.HasPrefix(alias, "local:"):
		source = map[string]any{
			"type":  "image",
			"alias": strings.TrimPrefix(alias, "local:"),
		}
	default:
		if strings.HasPrefix(alias, "images:") {
			alias = strings.TrimPrefix(alias, "images:")
		}
		if server == "" {
			server = defaultImagesServer
		}
		source = map[string]any{
			"type":     "image",
			"mode":     "pull",
			"protocol": "simplestreams",
			"server":   server,
			"alias":    alias,
		}
	}

	cfg := map[string]string{}
	if strings.TrimSpace(req.CPULimit) != "" {
		cfg["limits.cpu"] = strings.TrimSpace(req.CPULimit)
	}
	if strings.TrimSpace(req.MemoryLimit) != "" {
		cfg["limits.memory"] = strings.TrimSpace(req.MemoryLimit)
	}
	for k, v := range req.Env {
		if !ValidEnvKey(k) {
			return fmt.Errorf("invalid environment variable name %q", k)
		}
		cfg["environment."+k] = v
	}

	devices := map[string]map[string]string{}
	for _, p := range req.Proxies {
		if !ValidDeviceName(p.Name) || !ValidProxyAddr(p.Listen) || !ValidProxyAddr(p.Connect) {
			return fmt.Errorf("invalid proxy device %q", p.Name)
		}
		devices[p.Name] = map[string]string{
			"type":    "proxy",
			"listen":  p.Listen,
			"connect": p.Connect,
		}
	}

	profiles := req.Profiles
	if len(profiles) == 0 {
		profiles = []string{"default"}
	}

	payload := map[string]any{
		"name":        req.Name,
		"type":        instType,
		"description": strings.TrimSpace(req.Description),
		"profiles":    profiles,
		"config":      cfg,
		"source":      source,
	}
	if len(devices) > 0 {
		payload["devices"] = devices
	}
	env, err := c.do(ctx, http.MethodPost, "/1.0/instances", payload)
	if err != nil {
		return err
	}
	if err := c.waitOperation(ctx, env.Operation); err != nil {
		return err
	}
	if req.Start {
		if err := c.ChangeState(ctx, req.Name, "start"); err != nil {
			return fmt.Errorf("instance created, but failed to start: %w", err)
		}
	}
	return nil
}

func (c *Client) DeleteInstance(ctx context.Context, name string) error {
	if !ValidInstanceName(name) {
		return ErrInvalidName
	}
	env, err := c.do(ctx, http.MethodDelete, "/1.0/instances/"+url.PathEscape(name), nil)
	if err != nil {
		return err
	}
	return c.waitOperation(ctx, env.Operation)
}

func (c *Client) UpdateInstance(ctx context.Context, name string, req UpdateInstanceRequest) error {
	if !ValidInstanceName(name) {
		return ErrInvalidName
	}
	payload := map[string]any{}
	if req.Description != nil {
		payload["description"] = strings.TrimSpace(*req.Description)
	}
	cfg := map[string]string{}
	if req.CPULimit != nil {
		cfg["limits.cpu"] = strings.TrimSpace(*req.CPULimit)
	}
	if req.MemoryLimit != nil {
		cfg["limits.memory"] = strings.TrimSpace(*req.MemoryLimit)
	}
	if req.Autostart != nil {
		if *req.Autostart {
			cfg["boot.autostart"] = "true"
		} else {
			cfg["boot.autostart"] = "false"
		}
	}
	for k, v := range req.Env {
		if !ValidEnvKey(k) {
			return fmt.Errorf("invalid environment variable name %q", k)
		}
		cfg["environment."+k] = v
	}
	for _, k := range req.UnsetEnv {
		if !ValidEnvKey(k) {
			return fmt.Errorf("invalid environment variable name %q", k)
		}
		cfg["environment."+k] = ""
	}
	if len(cfg) > 0 {
		payload["config"] = cfg
	}
	if len(payload) == 0 {
		return errors.New("no fields to update")
	}
	env, err := c.do(ctx, http.MethodPatch, "/1.0/instances/"+url.PathEscape(name), payload)
	if err != nil {
		return err
	}
	return c.waitOperation(ctx, env.Operation)
}

func (c *Client) Exec(ctx context.Context, name string, req ExecRequest) (ExecResult, error) {
	if !ValidInstanceName(name) {
		return ExecResult{}, ErrInvalidName
	}
	cmdStr := strings.TrimSpace(req.Command)
	if cmdStr == "" || len(cmdStr) > 4096 {
		return ExecResult{}, errors.New("command must be between 1 and 4096 characters")
	}
	payload := map[string]any{
		"command":            []string{"/bin/sh", "-c", cmdStr},
		"wait-for-websocket": false,
		"interactive":        false,
		"record-output":      true,
		"environment":        map[string]string{"TERM": "xterm-256color"},
	}
	if cwd := strings.TrimSpace(req.Cwd); cwd != "" {
		payload["cwd"] = cwd
	}
	env, err := c.do(ctx, http.MethodPost, "/1.0/instances/"+url.PathEscape(name)+"/exec", payload)
	if err != nil {
		return ExecResult{}, err
	}
	metaRaw, err := c.waitOperationMetadata(ctx, env.Operation)
	if err != nil {
		return ExecResult{}, err
	}
	var meta struct {
		Return int               `json:"return"`
		Output map[string]string `json:"output"`
	}
	_ = json.Unmarshal(metaRaw, &meta)

	res := ExecResult{ExitCode: meta.Return}
	if outPath := meta.Output["1"]; outPath != "" {
		if text, status, err := c.doRaw(ctx, http.MethodGet, outPath, 256*1024); err == nil && status < 400 {
			res.Stdout = text
		}
		_, _, _ = c.doRaw(ctx, http.MethodDelete, outPath, 1024)
	}
	if errPath := meta.Output["2"]; errPath != "" {
		if text, status, err := c.doRaw(ctx, http.MethodGet, errPath, 256*1024); err == nil && status < 400 {
			res.Stderr = text
		}
		_, _, _ = c.doRaw(ctx, http.MethodDelete, errPath, 1024)
	}
	return res, nil
}

func (c *Client) ConsoleLog(ctx context.Context, name string) (string, error) {
	if !ValidInstanceName(name) {
		return "", ErrInvalidName
	}
	text, status, err := c.doRaw(ctx, http.MethodGet, "/1.0/instances/"+url.PathEscape(name)+"/console", 256*1024)
	if err != nil {
		return "", err
	}
	if status == http.StatusNotFound {
		return "", nil
	}
	if status >= 400 {
		return "", fmt.Errorf("console log unavailable (HTTP %d)", status)
	}
	return text, nil
}

func (c *Client) ListBackups(ctx context.Context, instanceName string) ([]Backup, error) {
	if !ValidInstanceName(instanceName) {
		return nil, ErrInvalidName
	}
	env, err := c.do(ctx, http.MethodGet, "/1.0/instances/"+url.PathEscape(instanceName)+"/backups?recursion=1", nil)
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Name             string    `json:"name"`
		CreatedAt        time.Time `json:"created_at"`
		ExpiresAt        time.Time `json:"expires_at"`
		InstanceOnly     bool      `json:"instance_only"`
		OptimizedStorage bool      `json:"optimized_storage"`
	}
	if err := json.Unmarshal(env.Metadata, &raw); err != nil {
		return nil, err
	}
	out := make([]Backup, 0, len(raw))
	for _, b := range raw {
		item := Backup{
			Name:         b.Name,
			InstanceOnly: b.InstanceOnly,
			Optimized:    b.OptimizedStorage,
		}
		if !b.CreatedAt.IsZero() {
			item.CreatedAt = b.CreatedAt.UTC().Format(time.RFC3339)
		}
		if !b.ExpiresAt.IsZero() {
			item.ExpiresAt = b.ExpiresAt.UTC().Format(time.RFC3339)
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt > out[j].CreatedAt
	})
	return out, nil
}

func (c *Client) CreateBackup(ctx context.Context, instanceName string, req CreateBackupRequest) error {
	if !ValidInstanceName(instanceName) || !ValidSnapshotName(req.Name) {
		return ErrInvalidName
	}
	payload := map[string]any{
		"name":              req.Name,
		"instance_only":     req.InstanceOnly,
		"optimized_storage": req.OptimizedStorage,
	}
	env, err := c.do(ctx, http.MethodPost, "/1.0/instances/"+url.PathEscape(instanceName)+"/backups", payload)
	if err != nil {
		return err
	}
	return c.waitOperation(ctx, env.Operation)
}

func (c *Client) DeleteBackup(ctx context.Context, instanceName, backupName string) error {
	if !ValidInstanceName(instanceName) || !ValidSnapshotName(backupName) {
		return ErrInvalidName
	}
	env, err := c.do(ctx, http.MethodDelete, "/1.0/instances/"+url.PathEscape(instanceName)+"/backups/"+url.PathEscape(backupName), nil)
	if err != nil {
		return err
	}
	return c.waitOperation(ctx, env.Operation)
}

func (c *Client) ExportBackup(ctx context.Context, instanceName, backupName string) (io.ReadCloser, error) {
	if !ValidInstanceName(instanceName) || !ValidSnapshotName(backupName) {
		return nil, ErrInvalidName
	}
	endpoint := "/1.0/instances/" + url.PathEscape(instanceName) + "/backups/" + url.PathEscape(backupName) + "/export"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix"+endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "no such file or directory") || strings.Contains(err.Error(), "connection refused") {
			return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		return nil, err
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		var env apiEnvelope
		if json.Unmarshal(raw, &env) == nil && env.Error != "" {
			return nil, errors.New(env.Error)
		}
		return nil, fmt.Errorf("backup export failed (HTTP %d)", resp.StatusCode)
	}
	return resp.Body, nil
}

func (c *Client) AddProxy(ctx context.Context, instanceName string, proxy ProxyDevice) error {
	if !ValidInstanceName(instanceName) || !ValidDeviceName(proxy.Name) {
		return ErrInvalidName
	}
	if !ValidProxyAddr(proxy.Listen) || !ValidProxyAddr(proxy.Connect) {
		return errors.New("proxy listen and connect must follow proto:host:port (e.g. tcp:0.0.0.0:8080)")
	}
	payload := map[string]any{
		"devices": map[string]any{
			proxy.Name: map[string]string{
				"type":    "proxy",
				"listen":  proxy.Listen,
				"connect": proxy.Connect,
			},
		},
	}
	env, err := c.do(ctx, http.MethodPatch, "/1.0/instances/"+url.PathEscape(instanceName), payload)
	if err != nil {
		return err
	}
	return c.waitOperation(ctx, env.Operation)
}

func (c *Client) RemoveProxy(ctx context.Context, instanceName, deviceName string) error {
	if !ValidInstanceName(instanceName) || !ValidDeviceName(deviceName) {
		return ErrInvalidName
	}
	payload := map[string]any{
		"devices": map[string]any{
			deviceName: nil,
		},
	}
	env, err := c.do(ctx, http.MethodPatch, "/1.0/instances/"+url.PathEscape(instanceName), payload)
	if err != nil {
		return err
	}
	return c.waitOperation(ctx, env.Operation)
}

func (c *Client) ListSnapshots(ctx context.Context, instanceName string) ([]Snapshot, error) {
	if !ValidInstanceName(instanceName) {
		return nil, ErrInvalidName
	}
	env, err := c.do(ctx, http.MethodGet, "/1.0/instances/"+url.PathEscape(instanceName)+"/snapshots?recursion=1", nil)
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Name      string    `json:"name"`
		CreatedAt time.Time `json:"created_at"`
		Stateful  bool      `json:"stateful"`
		Size      int64     `json:"size"`
	}
	if err := json.Unmarshal(env.Metadata, &raw); err != nil {
		return nil, err
	}
	out := make([]Snapshot, 0, len(raw))
	for _, s := range raw {
		snap := Snapshot{
			Name:      s.Name,
			Stateful:  s.Stateful,
			SizeBytes: s.Size,
		}
		if !s.CreatedAt.IsZero() {
			snap.CreatedAt = s.CreatedAt.UTC().Format(time.RFC3339)
		}
		out = append(out, snap)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt > out[j].CreatedAt
	})
	return out, nil
}

func (c *Client) CreateSnapshot(ctx context.Context, instanceName string, req CreateSnapshotRequest) error {
	if !ValidInstanceName(instanceName) || !ValidSnapshotName(req.Name) {
		return ErrInvalidName
	}
	payload := map[string]any{
		"name":     req.Name,
		"stateful": req.Stateful,
	}
	env, err := c.do(ctx, http.MethodPost, "/1.0/instances/"+url.PathEscape(instanceName)+"/snapshots", payload)
	if err != nil {
		return err
	}
	return c.waitOperation(ctx, env.Operation)
}

func (c *Client) RestoreSnapshot(ctx context.Context, instanceName, snapName string) error {
	if !ValidInstanceName(instanceName) || !ValidSnapshotName(snapName) {
		return ErrInvalidName
	}
	payload := map[string]any{
		"restore": snapName,
	}
	env, err := c.do(ctx, http.MethodPut, "/1.0/instances/"+url.PathEscape(instanceName), payload)
	if err != nil {
		return err
	}
	return c.waitOperation(ctx, env.Operation)
}

func (c *Client) DeleteSnapshot(ctx context.Context, instanceName, snapName string) error {
	if !ValidInstanceName(instanceName) || !ValidSnapshotName(snapName) {
		return ErrInvalidName
	}
	env, err := c.do(ctx, http.MethodDelete, "/1.0/instances/"+url.PathEscape(instanceName)+"/snapshots/"+url.PathEscape(snapName), nil)
	if err != nil {
		return err
	}
	return c.waitOperation(ctx, env.Operation)
}

func (c *Client) ListResources(ctx context.Context) (Resources, error) {
	res := Resources{
		StoragePools: []StoragePool{},
		Networks:     []Network{},
		Profiles:     []Profile{},
	}

	// Storage pools
	if env, err := c.do(ctx, http.MethodGet, "/1.0/storage-pools?recursion=1", nil); err == nil {
		var pools []struct {
			Name        string   `json:"name"`
			Driver      string   `json:"driver"`
			Status      string   `json:"status"`
			Description string   `json:"description"`
			UsedBy      []string `json:"used_by"`
		}
		if json.Unmarshal(env.Metadata, &pools) == nil {
			for _, p := range pools {
				sp := StoragePool{
					Name:        p.Name,
					Driver:      p.Driver,
					Status:      p.Status,
					Description: p.Description,
					UsedByCount: len(p.UsedBy),
				}
				if rEnv, rErr := c.do(ctx, http.MethodGet, "/1.0/storage-pools/"+url.PathEscape(p.Name)+"/resources", nil); rErr == nil {
					var usage struct {
						Space struct {
							Used  uint64 `json:"used"`
							Total uint64 `json:"total"`
						} `json:"space"`
					}
					if json.Unmarshal(rEnv.Metadata, &usage) == nil {
						sp.UsedBytes = usage.Space.Used
						sp.TotalBytes = usage.Space.Total
					}
				}
				res.StoragePools = append(res.StoragePools, sp)
			}
		}
	}

	// Networks
	if env, err := c.do(ctx, http.MethodGet, "/1.0/networks?recursion=1", nil); err == nil {
		var nets []struct {
			Name        string            `json:"name"`
			Type        string            `json:"type"`
			Managed     bool              `json:"managed"`
			Status      string            `json:"status"`
			Description string            `json:"description"`
			Config      map[string]string `json:"config"`
			UsedBy      []string          `json:"used_by"`
		}
		if json.Unmarshal(env.Metadata, &nets) == nil {
			for _, n := range nets {
				res.Networks = append(res.Networks, Network{
					Name:        n.Name,
					Type:        n.Type,
					Managed:     n.Managed,
					Status:      n.Status,
					Description: n.Description,
					IPv4Address: n.Config["ipv4.address"],
					IPv6Address: n.Config["ipv6.address"],
					UsedByCount: len(n.UsedBy),
				})
			}
		}
	}

	// Profiles
	if env, err := c.do(ctx, http.MethodGet, "/1.0/profiles?recursion=1", nil); err == nil {
		var profs []struct {
			Name        string   `json:"name"`
			Description string   `json:"description"`
			UsedBy      []string `json:"used_by"`
		}
		if json.Unmarshal(env.Metadata, &profs) == nil {
			for _, p := range profs {
				res.Profiles = append(res.Profiles, Profile{
					Name:        p.Name,
					Description: p.Description,
					UsedByCount: len(p.UsedBy),
				})
			}
		}
	}

	return res, nil
}

func splitPipe(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, " | ")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
