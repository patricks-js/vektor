// Package incus implements a zero-dependency Linux Unix-socket client for the
// Incus 1.0 REST API, designed to run inside the privileged vektor-ctl helper.
package incus

import (
	"errors"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const DefaultSocketPath = "/var/lib/incus/unix.socket"

var (
	ErrUnavailable = errors.New("incus unix socket unavailable")
	ErrInvalidName = errors.New("invalid incus resource name")

	instanceNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{0,62}$`)
	snapshotNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)
	imageAliasRe   = regexp.MustCompile(`^[a-zA-Z0-9._:/@-]{1,256}$`)
	deviceNameRe   = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)
	envKeyRe       = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
)

func ValidInstanceName(name string) bool {
	return instanceNameRe.MatchString(name)
}

func ValidSnapshotName(name string) bool {
	return snapshotNameRe.MatchString(name)
}

func ValidImageAlias(alias string) bool {
	return imageAliasRe.MatchString(alias)
}

func ValidDeviceName(name string) bool {
	return deviceNameRe.MatchString(name)
}

func ValidEnvKey(key string) bool {
	return envKeyRe.MatchString(key)
}

func ValidProxyAddr(addr string) bool {
	var rest string
	switch {
	case strings.HasPrefix(addr, "tcp:"):
		rest = strings.TrimPrefix(addr, "tcp:")
	case strings.HasPrefix(addr, "udp:"):
		rest = strings.TrimPrefix(addr, "udp:")
	default:
		return false
	}
	hostStr, portStr, err := net.SplitHostPort(rest)
	if err != nil {
		return false
	}
	if _, err := netip.ParseAddr(hostStr); err != nil {
		return false
	}
	port, err := strconv.Atoi(portStr)
	return err == nil && port >= 1 && port <= 65535
}

// ValidImageServer ensures image pull servers use HTTPS and do not target loopback,
// link-local, or private IP literals (mitigating SSRF through incusd).
func ValidImageServer(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return true
	}
	if len(raw) > 256 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return false
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		ip = ip.Unmap()
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
			return false
		}
	}
	return true
}

type ProxyDevice struct {
	Name    string `json:"name"`
	Listen  string `json:"listen"`  // e.g. tcp:0.0.0.0:8080
	Connect string `json:"connect"` // e.g. tcp:127.0.0.1:80
}

type Status struct {
	Available      bool     `json:"available"`
	Reason         string   `json:"reason,omitempty"`
	ServerVersion  string   `json:"server_version,omitempty"`
	KernelVersion  string   `json:"kernel_version,omitempty"`
	Drivers        []string `json:"drivers,omitempty"`
	StorageDrivers []string `json:"storage_drivers,omitempty"`
	OCISupported   bool     `json:"oci_supported"`
	InstanceCount  int      `json:"instance_count"`
	RunningCount   int      `json:"running_count"`
	VMCount        int      `json:"vm_count"`
	ContainerCount int      `json:"container_count"`
	OCICount       int      `json:"oci_count"`
}

type Instance struct {
	Name             string            `json:"name"`
	Type             string            `json:"type"` // "container" | "virtual-machine"
	OCI              bool              `json:"oci"`
	Status           string            `json:"status"`
	Description      string            `json:"description,omitempty"`
	Architecture     string            `json:"architecture,omitempty"`
	Ephemeral        bool              `json:"ephemeral"`
	Stateful         bool              `json:"stateful"`
	Profiles         []string          `json:"profiles"`
	CreatedAt        string            `json:"created_at,omitempty"`
	LastUsedAt       string            `json:"last_used_at,omitempty"`
	CPULimit         string            `json:"cpu_limit,omitempty"`
	MemoryLimit      string            `json:"memory_limit,omitempty"`
	Autostart        bool              `json:"autostart"`
	Env              map[string]string `json:"env,omitempty"`
	Proxies          []ProxyDevice     `json:"proxies,omitempty"`
	CPUUsageNs       int64             `json:"cpu_usage_ns"`
	MemoryUsageBytes int64             `json:"memory_usage_bytes"`
	MemoryPeakBytes  int64             `json:"memory_peak_bytes"`
	DiskUsageBytes   int64             `json:"disk_usage_bytes"`
	Processes        int64             `json:"processes"`
	PID              int64             `json:"pid"`
	IPv4             []string          `json:"ipv4"`
	IPv6             []string          `json:"ipv6"`
	SnapshotsCount   int               `json:"snapshots_count"`
}

type Snapshot struct {
	Name      string `json:"name"`
	CreatedAt string `json:"created_at,omitempty"`
	Stateful  bool   `json:"stateful"`
	SizeBytes int64  `json:"size_bytes"`
}

type StoragePool struct {
	Name        string `json:"name"`
	Driver      string `json:"driver"`
	Status      string `json:"status"`
	Description string `json:"description,omitempty"`
	UsedBytes   uint64 `json:"used_bytes"`
	TotalBytes  uint64 `json:"total_bytes"`
	UsedByCount int    `json:"used_by_count"`
}

type Network struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Managed     bool   `json:"managed"`
	Status      string `json:"status,omitempty"`
	Description string `json:"description,omitempty"`
	IPv4Address string `json:"ipv4_address,omitempty"`
	IPv6Address string `json:"ipv6_address,omitempty"`
	UsedByCount int    `json:"used_by_count"`
}

type Profile struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	UsedByCount int    `json:"used_by_count"`
}

type Resources struct {
	StoragePools []StoragePool `json:"storage_pools"`
	Networks     []Network     `json:"networks"`
	Profiles     []Profile     `json:"profiles"`
}

type CreateInstanceRequest struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`               // "container" | "virtual-machine"
	Protocol    string            `json:"protocol,omitempty"` // "" | "simplestreams" | "oci"
	Server      string            `json:"server,omitempty"`   // e.g. https://registry-1.docker.io or https://ghcr.io
	Image       string            `json:"image"`
	Description string            `json:"description,omitempty"`
	CPULimit    string            `json:"cpu_limit,omitempty"`
	MemoryLimit string            `json:"memory_limit,omitempty"`
	Profiles    []string          `json:"profiles,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Proxies     []ProxyDevice     `json:"proxies,omitempty"`
	Start       bool              `json:"start"`
}

type CreateSnapshotRequest struct {
	Name     string `json:"name"`
	Stateful bool   `json:"stateful,omitempty"`
}

type StateActionRequest struct {
	Action  string `json:"action"` // start, stop, force_stop, restart, freeze, unfreeze
	Timeout int    `json:"timeout,omitempty"`
}

type UpdateInstanceRequest struct {
	Description *string           `json:"description,omitempty"`
	CPULimit    *string           `json:"cpu_limit,omitempty"`
	MemoryLimit *string           `json:"memory_limit,omitempty"`
	Autostart   *bool             `json:"autostart,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	UnsetEnv    []string          `json:"unset_env,omitempty"`
}

type ExecRequest struct {
	Command string `json:"command"`
	Cwd     string `json:"cwd,omitempty"`
}

type ExecResult struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

type Backup struct {
	Name          string `json:"name"`
	CreatedAt     string `json:"created_at,omitempty"`
	ExpiresAt     string `json:"expires_at,omitempty"`
	InstanceOnly  bool   `json:"instance_only"`
	Optimized     bool   `json:"optimized_storage"`
}

type CreateBackupRequest struct {
	Name             string `json:"name"`
	InstanceOnly     bool   `json:"instance_only"`
	OptimizedStorage bool   `json:"optimized_storage"`
}
