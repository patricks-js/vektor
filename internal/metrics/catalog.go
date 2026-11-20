package metrics

import (
	"slices"
)

const CatalogVersion = "v1"

// Raw counter input keys emitted by collectors before CounterTracker rate derivation.
const (
	RawDiskReadBytesTotal  = "disk.read_bytes_total"
	RawDiskWriteBytesTotal = "disk.write_bytes_total"
	RawDiskReadsTotal      = "disk.reads_total"
	RawDiskWritesTotal     = "disk.writes_total"
	RawDiskIOTimeMsTotal   = "disk.io_time_ms_total"

	RawNetRxBytesTotal   = "net.rx_bytes_total"
	RawNetTxBytesTotal   = "net.tx_bytes_total"
	RawNetRxPacketsTotal = "net.rx_packets_total"
	RawNetTxPacketsTotal = "net.tx_packets_total"
	RawNetRxErrorsTotal  = "net.rx_errors_total"
	RawNetTxErrorsTotal  = "net.tx_errors_total"
	RawNetRxDropsTotal   = "net.rx_drops_total"
	RawNetTxDropsTotal   = "net.tx_drops_total"
)

// Canonical persisted metric keys registered in the versioned catalog.
const (
	KeyCPUBusyPct     = "cpu.busy_pct"
	KeyCPUUserPct     = "cpu.user_pct"
	KeyCPUSystemPct   = "cpu.system_pct"
	KeyCPUIOWaitPct   = "cpu.iowait_pct"
	KeyCPUStealPct    = "cpu.steal_pct"
	KeyCPUCoreBusyPct = "cpu.core.busy_pct"

	KeyLoad1m  = "load.1m"
	KeyLoad5m  = "load.5m"
	KeyLoad15m = "load.15m"

	KeyMemoryTotalBytes     = "memory.total_bytes"
	KeyMemoryUsedBytes      = "memory.used_bytes"
	KeyMemoryAvailableBytes = "memory.available_bytes"
	KeyMemoryCachedBytes    = "memory.cached_bytes"
	KeyMemoryBuffersBytes   = "memory.buffers_bytes"
	KeyMemoryUsedPct        = "memory.used_pct"
	KeySwapTotalBytes       = "swap.total_bytes"
	KeySwapUsedBytes        = "swap.used_bytes"
	KeySwapUsedPct          = "swap.used_pct"

	KeyDiskReadBytesPerSec  = "disk.read_bytes_per_sec"
	KeyDiskWriteBytesPerSec = "disk.write_bytes_per_sec"
	KeyDiskReadsPerSec      = "disk.reads_per_sec"
	KeyDiskWritesPerSec     = "disk.writes_per_sec"
	KeyDiskIOTimeMsPerSec   = "disk.io_time_ms_per_sec"

	KeyFilesystemTotalBytes     = "filesystem.total_bytes"
	KeyFilesystemUsedBytes      = "filesystem.used_bytes"
	KeyFilesystemAvailableBytes = "filesystem.available_bytes"
	KeyFilesystemUsedPct        = "filesystem.used_pct"
	KeyFilesystemInodesUsedPct  = "filesystem.inodes_used_pct"

	KeyNetRxBytesPerSec   = "net.rx_bytes_per_sec"
	KeyNetTxBytesPerSec   = "net.tx_bytes_per_sec"
	KeyNetRxPacketsPerSec = "net.rx_packets_per_sec"
	KeyNetTxPacketsPerSec = "net.tx_packets_per_sec"
	KeyNetRxErrorsPerSec  = "net.rx_errors_per_sec"
	KeyNetTxErrorsPerSec  = "net.tx_errors_per_sec"
	KeyNetRxDropsPerSec   = "net.rx_drops_per_sec"
	KeyNetTxDropsPerSec   = "net.tx_drops_per_sec"

	KeySensorTemperatureCelsius = "sensor.temperature_celsius"
)

// MetricDef defines a canonical metric entry in the central catalog.
type MetricDef struct {
	Key           string
	Unit          string
	ResourceKinds []string
	Modality      MetricModality
	Enabled       bool
	Aggregators   []string
}

var defaultAggregators = []string{"avg", "min", "max", "last"}

var catalogList = []MetricDef{
	{Key: KeyCPUBusyPct, Unit: "%", ResourceKinds: []string{"host"}, Modality: ModalityRate, Enabled: true, Aggregators: defaultAggregators},
	{Key: KeyCPUUserPct, Unit: "%", ResourceKinds: []string{"host"}, Modality: ModalityRate, Enabled: true, Aggregators: defaultAggregators},
	{Key: KeyCPUSystemPct, Unit: "%", ResourceKinds: []string{"host"}, Modality: ModalityRate, Enabled: true, Aggregators: defaultAggregators},
	{Key: KeyCPUIOWaitPct, Unit: "%", ResourceKinds: []string{"host"}, Modality: ModalityRate, Enabled: true, Aggregators: defaultAggregators},
	{Key: KeyCPUStealPct, Unit: "%", ResourceKinds: []string{"host"}, Modality: ModalityRate, Enabled: true, Aggregators: defaultAggregators},
	{Key: KeyCPUCoreBusyPct, Unit: "%", ResourceKinds: []string{"cpu_core"}, Modality: ModalityRate, Enabled: true, Aggregators: defaultAggregators},

	{Key: KeyLoad1m, Unit: "load", ResourceKinds: []string{"host"}, Modality: ModalityGauge, Enabled: true, Aggregators: defaultAggregators},
	{Key: KeyLoad5m, Unit: "load", ResourceKinds: []string{"host"}, Modality: ModalityGauge, Enabled: true, Aggregators: defaultAggregators},
	{Key: KeyLoad15m, Unit: "load", ResourceKinds: []string{"host"}, Modality: ModalityGauge, Enabled: true, Aggregators: defaultAggregators},

	{Key: KeyMemoryTotalBytes, Unit: "bytes", ResourceKinds: []string{"host"}, Modality: ModalityGauge, Enabled: true, Aggregators: defaultAggregators},
	{Key: KeyMemoryUsedBytes, Unit: "bytes", ResourceKinds: []string{"host"}, Modality: ModalityGauge, Enabled: true, Aggregators: defaultAggregators},
	{Key: KeyMemoryAvailableBytes, Unit: "bytes", ResourceKinds: []string{"host"}, Modality: ModalityGauge, Enabled: true, Aggregators: defaultAggregators},
	{Key: KeyMemoryCachedBytes, Unit: "bytes", ResourceKinds: []string{"host"}, Modality: ModalityGauge, Enabled: true, Aggregators: defaultAggregators},
	{Key: KeyMemoryBuffersBytes, Unit: "bytes", ResourceKinds: []string{"host"}, Modality: ModalityGauge, Enabled: true, Aggregators: defaultAggregators},
	{Key: KeyMemoryUsedPct, Unit: "%", ResourceKinds: []string{"host"}, Modality: ModalityGauge, Enabled: true, Aggregators: defaultAggregators},
	{Key: KeySwapTotalBytes, Unit: "bytes", ResourceKinds: []string{"host"}, Modality: ModalityGauge, Enabled: true, Aggregators: defaultAggregators},
	{Key: KeySwapUsedBytes, Unit: "bytes", ResourceKinds: []string{"host"}, Modality: ModalityGauge, Enabled: true, Aggregators: defaultAggregators},
	{Key: KeySwapUsedPct, Unit: "%", ResourceKinds: []string{"host"}, Modality: ModalityGauge, Enabled: true, Aggregators: defaultAggregators},

	{Key: KeyDiskReadBytesPerSec, Unit: "bytes/s", ResourceKinds: []string{"disk"}, Modality: ModalityRate, Enabled: true, Aggregators: defaultAggregators},
	{Key: KeyDiskWriteBytesPerSec, Unit: "bytes/s", ResourceKinds: []string{"disk"}, Modality: ModalityRate, Enabled: true, Aggregators: defaultAggregators},
	{Key: KeyDiskReadsPerSec, Unit: "ops/s", ResourceKinds: []string{"disk"}, Modality: ModalityRate, Enabled: true, Aggregators: defaultAggregators},
	{Key: KeyDiskWritesPerSec, Unit: "ops/s", ResourceKinds: []string{"disk"}, Modality: ModalityRate, Enabled: true, Aggregators: defaultAggregators},
	{Key: KeyDiskIOTimeMsPerSec, Unit: "ms/s", ResourceKinds: []string{"disk"}, Modality: ModalityRate, Enabled: true, Aggregators: defaultAggregators},

	{Key: KeyFilesystemTotalBytes, Unit: "bytes", ResourceKinds: []string{"filesystem"}, Modality: ModalityGauge, Enabled: true, Aggregators: defaultAggregators},
	{Key: KeyFilesystemUsedBytes, Unit: "bytes", ResourceKinds: []string{"filesystem"}, Modality: ModalityGauge, Enabled: true, Aggregators: defaultAggregators},
	{Key: KeyFilesystemAvailableBytes, Unit: "bytes", ResourceKinds: []string{"filesystem"}, Modality: ModalityGauge, Enabled: true, Aggregators: defaultAggregators},
	{Key: KeyFilesystemUsedPct, Unit: "%", ResourceKinds: []string{"filesystem"}, Modality: ModalityGauge, Enabled: true, Aggregators: defaultAggregators},
	{Key: KeyFilesystemInodesUsedPct, Unit: "%", ResourceKinds: []string{"filesystem"}, Modality: ModalityGauge, Enabled: true, Aggregators: defaultAggregators},

	{Key: KeyNetRxBytesPerSec, Unit: "bytes/s", ResourceKinds: []string{"nic", "host"}, Modality: ModalityRate, Enabled: true, Aggregators: defaultAggregators},
	{Key: KeyNetTxBytesPerSec, Unit: "bytes/s", ResourceKinds: []string{"nic", "host"}, Modality: ModalityRate, Enabled: true, Aggregators: defaultAggregators},
	{Key: KeyNetRxPacketsPerSec, Unit: "packets/s", ResourceKinds: []string{"nic"}, Modality: ModalityRate, Enabled: true, Aggregators: defaultAggregators},
	{Key: KeyNetTxPacketsPerSec, Unit: "packets/s", ResourceKinds: []string{"nic"}, Modality: ModalityRate, Enabled: true, Aggregators: defaultAggregators},
	{Key: KeyNetRxErrorsPerSec, Unit: "errors/s", ResourceKinds: []string{"nic"}, Modality: ModalityRate, Enabled: true, Aggregators: defaultAggregators},
	{Key: KeyNetTxErrorsPerSec, Unit: "errors/s", ResourceKinds: []string{"nic"}, Modality: ModalityRate, Enabled: true, Aggregators: defaultAggregators},
	{Key: KeyNetRxDropsPerSec, Unit: "drops/s", ResourceKinds: []string{"nic"}, Modality: ModalityRate, Enabled: true, Aggregators: defaultAggregators},
	{Key: KeyNetTxDropsPerSec, Unit: "drops/s", ResourceKinds: []string{"nic"}, Modality: ModalityRate, Enabled: true, Aggregators: defaultAggregators},

	{Key: KeySensorTemperatureCelsius, Unit: "°C", ResourceKinds: []string{"sensor"}, Modality: ModalityGauge, Enabled: true, Aggregators: defaultAggregators},
}

var catalogMap = func() map[string]MetricDef {
	m := make(map[string]MetricDef, len(catalogList))
	for _, def := range catalogList {
		m[def.Key] = def
	}
	return m
}()

// CounterOutputKey maps raw cumulative counter keys emitted by collectors to derived catalog rate keys.
var CounterOutputKey = map[string]string{
	RawDiskReadBytesTotal:  KeyDiskReadBytesPerSec,
	RawDiskWriteBytesTotal: KeyDiskWriteBytesPerSec,
	RawDiskReadsTotal:      KeyDiskReadsPerSec,
	RawDiskWritesTotal:     KeyDiskWritesPerSec,
	RawDiskIOTimeMsTotal:   KeyDiskIOTimeMsPerSec,

	RawNetRxBytesTotal:   KeyNetRxBytesPerSec,
	RawNetTxBytesTotal:   KeyNetTxBytesPerSec,
	RawNetRxPacketsTotal: KeyNetRxPacketsPerSec,
	RawNetTxPacketsTotal: KeyNetTxPacketsPerSec,
	RawNetRxErrorsTotal:  KeyNetRxErrorsPerSec,
	RawNetTxErrorsTotal:  KeyNetTxErrorsPerSec,
	RawNetRxDropsTotal:   KeyNetRxDropsPerSec,
	RawNetTxDropsTotal:   KeyNetTxDropsPerSec,
}

// LookupMetric looks up a canonical metric definition by key.
func LookupMetric(key string) (MetricDef, bool) {
	def, ok := catalogMap[key]
	if !ok {
		return MetricDef{}, false
	}
	def.ResourceKinds = slices.Clone(def.ResourceKinds)
	def.Aggregators = slices.Clone(def.Aggregators)
	return def, true
}

// IsKnownMetric reports whether key exists in the central catalog.
func IsKnownMetric(key string) bool {
	_, ok := catalogMap[key]
	return ok
}

// AllMetrics returns a defensive copy of all catalog definitions.
func AllMetrics() []MetricDef {
	out := make([]MetricDef, len(catalogList))
	for i, def := range catalogList {
		def.ResourceKinds = slices.Clone(def.ResourceKinds)
		def.Aggregators = slices.Clone(def.Aggregators)
		out[i] = def
	}
	return out
}

// AlignBucketMs floors a UTC timestamp in milliseconds to an integer boundary of resolutionMs.
func AlignBucketMs(tsMs, resolutionMs int64) int64 {
	if resolutionMs <= 0 {
		return tsMs
	}
	rem := tsMs % resolutionMs
	if rem < 0 {
		rem += resolutionMs
	}
	return tsMs - rem
}
