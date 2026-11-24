// Package incus implements a zero-dependency Linux Unix-socket client for the
// Incus 1.0 REST API, designed to run inside the privileged vektor-ctl helper.
package incus

import (
	"errors"
	"regexp"
)

const DefaultSocketPath = "/var/lib/incus/unix.socket"

var (
	ErrUnavailable = errors.New("incus unix socket unavailable")
	ErrInvalidName = errors.New("invalid incus resource name")

	instanceNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{0,62}$`)
	snapshotNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)
	imageAliasRe   = regexp.MustCompile(`^[a-zA-Z0-9._:/-]{1,128}$`)
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

type Status struct {
	Available      bool     `json:"available"`
	Reason         string   `json:"reason,omitempty"`
	ServerVersion  string   `json:"server_version,omitempty"`
	KernelVersion  string   `json:"kernel_version,omitempty"`
	Drivers        []string `json:"drivers,omitempty"`
	StorageDrivers []string `json:"storage_drivers,omitempty"`
	InstanceCount  int      `json:"instance_count"`
	RunningCount   int      `json:"running_count"`
	VMCount        int      `json:"vm_count"`
	ContainerCount int      `json:"container_count"`
}

type Instance struct {
	Name             string   `json:"name"`
	Type             string   `json:"type"` // "container" | "virtual-machine"
	Status           string   `json:"status"`
	Description      string   `json:"description,omitempty"`
	Architecture     string   `json:"architecture,omitempty"`
	Ephemeral        bool     `json:"ephemeral"`
	Stateful         bool     `json:"stateful"`
	Profiles         []string `json:"profiles"`
	CreatedAt        string   `json:"created_at,omitempty"`
	LastUsedAt       string   `json:"last_used_at,omitempty"`
	CPULimit         string   `json:"cpu_limit,omitempty"`
	MemoryLimit      string   `json:"memory_limit,omitempty"`
	CPUUsageNs       int64    `json:"cpu_usage_ns"`
	MemoryUsageBytes int64    `json:"memory_usage_bytes"`
	MemoryPeakBytes  int64    `json:"memory_peak_bytes"`
	DiskUsageBytes   int64    `json:"disk_usage_bytes"`
	Processes        int64    `json:"processes"`
	PID              int64    `json:"pid"`
	IPv4             []string `json:"ipv4"`
	IPv6             []string `json:"ipv6"`
	SnapshotsCount   int      `json:"snapshots_count"`
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
	Name        string   `json:"name"`
	Type        string   `json:"type"` // "container" | "virtual-machine"
	Image       string   `json:"image"`
	Description string   `json:"description,omitempty"`
	CPULimit    string   `json:"cpu_limit,omitempty"`
	MemoryLimit string   `json:"memory_limit,omitempty"`
	Profiles    []string `json:"profiles,omitempty"`
	Start       bool     `json:"start"`
}

type CreateSnapshotRequest struct {
	Name     string `json:"name"`
	Stateful bool   `json:"stateful,omitempty"`
}

type StateActionRequest struct {
	Action  string `json:"action"` // start, stop, force_stop, restart, freeze, unfreeze
	Timeout int    `json:"timeout,omitempty"`
}
