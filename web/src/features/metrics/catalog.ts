// Subset of the daemon catalog used by the dashboard.
export const metric = {
  cpu: 'cpu.busy_pct', core: 'cpu.core.busy_pct',
  load1: 'load.1m', load5: 'load.5m', load15: 'load.15m',
  ram: 'memory.used_pct', ramUsed: 'memory.used_bytes', ramTotal: 'memory.total_bytes',
  ramAvailable: 'memory.available_bytes', cache: 'memory.cached_bytes', buffers: 'memory.buffers_bytes',
  swapUsed: 'swap.used_bytes', swapTotal: 'swap.total_bytes', swap: 'swap.used_pct',
  fsUsed: 'filesystem.used_bytes', fsTotal: 'filesystem.total_bytes',
  fsAvailable: 'filesystem.available_bytes', fs: 'filesystem.used_pct',
  diskRead: 'disk.read_bytes_per_sec', diskWrite: 'disk.write_bytes_per_sec',
  diskReads: 'disk.reads_per_sec', diskWrites: 'disk.writes_per_sec',
  rx: 'net.rx_bytes_per_sec', tx: 'net.tx_bytes_per_sec',
  rxErrors: 'net.rx_errors_per_sec', txErrors: 'net.tx_errors_per_sec',
  rxDrops: 'net.rx_drops_per_sec', txDrops: 'net.tx_drops_per_sec',
  temperature: 'sensor.temperature_celsius',
  incusCpu: 'incus.cpu_pct', incusMemory: 'incus.memory_bytes',
  incusDisk: 'incus.disk_bytes', incusProcesses: 'incus.processes',
} as const
