package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	SynchronousNormal = "NORMAL"
	SynchronousFull   = "FULL"
)

// Config defines runtime, scheduling, storage, and Linux discovery settings for vektord.
type Config struct {
	DataDir  string
	DBPath   string
	ProcRoot string
	SysRoot  string
	EtcRoot  string
	DevDiskRoot string

	PersistInterval   time.Duration
	LiveInterval      time.Duration
	DiscoveryInterval time.Duration
	SlowFSInterval    time.Duration
	SensorInterval    time.Duration
	RollupInterval    time.Duration
	RollupGrace       time.Duration

	CollectorTimeout     time.Duration
	SlowCollectorTimeout time.Duration
	MaxFastConcurrency   int
	CounterEvictAfter    time.Duration

	RawRetention    time.Duration
	MediumRetention time.Duration
	LongRetention   time.Duration

	SQLiteBusyTimeoutMs int
	SQLiteSynchronous   string

	IncludePerCoreCPU bool
	IncludeSensors    bool

	ExcludedFSTypes []string
}

// Default returns a production-safe configuration aligned with TECHNICAL_DESIGN_V0.1.md.
func Default() Config {
	return Config{
		DataDir:     "./data",
		ProcRoot:    "/proc",
		SysRoot:     "/sys",
		EtcRoot:     "/etc",
		DevDiskRoot: "/dev/disk",

		PersistInterval:   30 * time.Second,
		LiveInterval:      2 * time.Second,
		DiscoveryInterval: 60 * time.Second,
		SlowFSInterval:    60 * time.Second,
		SensorInterval:    10 * time.Second,
		RollupInterval:    5 * time.Minute,
		RollupGrace:       60 * time.Second,

		CollectorTimeout:     5 * time.Second,
		SlowCollectorTimeout: 4 * time.Second,
		MaxFastConcurrency:   4,
		CounterEvictAfter:    time.Hour,

		RawRetention:    24 * time.Hour,
		MediumRetention: 7 * 24 * time.Hour,
		LongRetention:   90 * 24 * time.Hour,

		SQLiteBusyTimeoutMs: 5000,
		SQLiteSynchronous:   SynchronousNormal,

		IncludePerCoreCPU: true,
		IncludeSensors:    true,

		ExcludedFSTypes: []string{
			"proc", "sysfs", "tmpfs", "devtmpfs", "devpts",
			"cgroup", "cgroup2", "overlay", "squashfs", "tracefs",
			"securityfs", "debugfs", "pstore", "autofs", "mqueue",
			"hugetlbfs", "ramfs", "bpf", "nsfs", "efivarfs",
			"configfs", "fusectl", "rpc_pipefs",
			"nfs", "nfs4", "cifs", "smb3", "fuse", "sshfs",
			"glusterfs", "ceph", "9p",
		},
	}
}

// EffectiveDBPath returns DBPath if explicitly set, or <DataDir>/vektor.db.
func (c Config) EffectiveDBPath() string {
	if strings.TrimSpace(c.DBPath) != "" {
		return c.DBPath
	}
	return filepath.Join(c.DataDir, "vektor.db")
}

// Validate checks bounds and invariants across intervals, retention, concurrency, and SQLite settings.
func (c Config) Validate() error {
	var errs []error

	if strings.TrimSpace(c.DataDir) == "" && strings.TrimSpace(c.DBPath) == "" {
		errs = append(errs, errors.New("data_dir or db_path must be set"))
	}
	if strings.TrimSpace(c.ProcRoot) == "" {
		errs = append(errs, errors.New("proc_root must not be empty"))
	}
	if strings.TrimSpace(c.SysRoot) == "" {
		errs = append(errs, errors.New("sys_root must not be empty"))
	}

	errs = appendDurationBounds(errs, "persist_interval", c.PersistInterval, 10*time.Second, 5*time.Minute)
	errs = appendDurationBounds(errs, "live_interval", c.LiveInterval, 1*time.Second, 10*time.Second)
	errs = appendDurationBounds(errs, "discovery_interval", c.DiscoveryInterval, 15*time.Second, 15*time.Minute)
	errs = appendDurationBounds(errs, "slow_fs_interval", c.SlowFSInterval, 15*time.Second, 15*time.Minute)
	errs = appendDurationBounds(errs, "sensor_interval", c.SensorInterval, 5*time.Second, 5*time.Minute)
	errs = appendDurationBounds(errs, "rollup_interval", c.RollupInterval, 1*time.Minute, 30*time.Minute)
	errs = appendDurationBounds(errs, "rollup_grace", c.RollupGrace, 10*time.Second, 10*time.Minute)

	if c.CollectorTimeout <= 0 || c.CollectorTimeout > 30*time.Second {
		errs = append(errs, fmt.Errorf("collector_timeout %s must be in (0s, 30s]", c.CollectorTimeout))
	}
	if c.SlowCollectorTimeout <= 0 || c.SlowCollectorTimeout > 30*time.Second {
		errs = append(errs, fmt.Errorf("slow_collector_timeout %s must be in (0s, 30s]", c.SlowCollectorTimeout))
	}
	if c.MaxFastConcurrency < 1 || c.MaxFastConcurrency > 16 {
		errs = append(errs, fmt.Errorf("max_fast_concurrency %d must be in [1, 16]", c.MaxFastConcurrency))
	}
	if c.CounterEvictAfter < c.PersistInterval {
		errs = append(errs, fmt.Errorf("counter_evict_after %s must be >= persist_interval %s", c.CounterEvictAfter, c.PersistInterval))
	}

	if c.RawRetention < time.Hour {
		errs = append(errs, fmt.Errorf("raw_retention %s must be >= 1h", c.RawRetention))
	}
	if c.MediumRetention < c.RawRetention {
		errs = append(errs, fmt.Errorf("medium_retention %s must be >= raw_retention %s", c.MediumRetention, c.RawRetention))
	}
	if c.LongRetention < c.MediumRetention {
		errs = append(errs, fmt.Errorf("long_retention %s must be >= medium_retention %s", c.LongRetention, c.MediumRetention))
	}

	if c.SQLiteBusyTimeoutMs < 100 || c.SQLiteBusyTimeoutMs > 60000 {
		errs = append(errs, fmt.Errorf("sqlite_busy_timeout_ms %d must be in [100, 60000]", c.SQLiteBusyTimeoutMs))
	}
	syncMode := strings.ToUpper(strings.TrimSpace(c.SQLiteSynchronous))
	if !slices.Contains([]string{SynchronousNormal, SynchronousFull}, syncMode) {
		errs = append(errs, fmt.Errorf("sqlite_synchronous %q must be NORMAL or FULL", c.SQLiteSynchronous))
	}

	return errors.Join(errs...)
}

func appendDurationBounds(errs []error, name string, v, minV, maxV time.Duration) []error {
	if v < minV || v > maxV {
		return append(errs, fmt.Errorf("%s %s must be in [%s, %s]", name, v, minV, maxV))
	}
	return errs
}
