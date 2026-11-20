package host

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Inventory represents static/slow-changing hardware and OS metadata for the local host.
type Inventory struct {
	Hostname         string `json:"hostname"`
	OSName           string `json:"os_name"`
	OSVersion        string `json:"os_version"`
	OSPretty         string `json:"os_pretty"`
	Kernel           string `json:"kernel"`
	Arch             string `json:"arch"`
	CPUModel         string `json:"cpu_model"`
	CPUCores         int    `json:"cpu_cores"`
	CPUThreads       int    `json:"cpu_threads"`
	MemoryTotalBytes uint64 `json:"memory_total_bytes"`
	BootID           string `json:"boot_id"`
	UpdatedAtMs      int64  `json:"updated_at_ms"`
}

// CollectInventory gathers host OS, kernel, CPU, memory, and boot_id metadata from procRoot and etcRoot.
func CollectInventory(procRoot, etcRoot string, now time.Time) Inventory {
	if procRoot == "" {
		procRoot = "/proc"
	}
	if etcRoot == "" {
		etcRoot = "/etc"
	}
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		hostname = "localhost"
	}

	inv := Inventory{
		Hostname:    hostname,
		Arch:        runtime.GOARCH,
		UpdatedAtMs: now.UTC().UnixMilli(),
	}

	if data, err := os.ReadFile(filepath.Join(etcRoot, "os-release")); err == nil {
		parseOSRelease(data, &inv)
	}
	if data, err := os.ReadFile(filepath.Join(procRoot, "version")); err == nil {
		inv.Kernel = parseProcVersion(data)
	}
	if data, err := os.ReadFile(filepath.Join(procRoot, "cpuinfo")); err == nil {
		parseCPUInfo(data, &inv)
	}
	if data, err := os.ReadFile(filepath.Join(procRoot, "meminfo")); err == nil {
		inv.MemoryTotalBytes = parseMemTotal(data)
	}
	if data, err := os.ReadFile(filepath.Join(procRoot, "sys/kernel/random/boot_id")); err == nil {
		inv.BootID = strings.TrimSpace(string(data))
	}

	return inv
}

func parseOSRelease(data []byte, inv *Inventory) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"'`)
		switch k {
		case "NAME":
			inv.OSName = v
		case "VERSION_ID":
			inv.OSVersion = v
		case "PRETTY_NAME":
			inv.OSPretty = v
		}
	}
}

func parseProcVersion(data []byte) string {
	fields := strings.Fields(string(data))
	if len(fields) >= 3 && fields[0] == "Linux" && fields[1] == "version" {
		return fields[2]
	}
	if len(fields) > 0 {
		return strings.Join(fields, " ")
	}
	return ""
}

func parseCPUInfo(data []byte, inv *Inventory) {
	type physCore struct {
		physID string
		coreID string
	}
	uniqueCores := make(map[physCore]struct{})
	var curPhys, curCore string
	var declaredCores int

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			if curPhys != "" && curCore != "" {
				uniqueCores[physCore{physID: curPhys, coreID: curCore}] = struct{}{}
			}
			curPhys = ""
			curCore = ""
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key := strings.TrimSpace(k)
		val := strings.TrimSpace(v)
		switch key {
		case "processor":
			inv.CPUThreads++
		case "model name", "Model", "Hardware":
			if inv.CPUModel == "" {
				inv.CPUModel = val
			}
		case "physical id":
			curPhys = val
		case "core id":
			curCore = val
		case "cpu cores":
			if declaredCores == 0 {
				if n, err := strconv.Atoi(val); err == nil && n > 0 {
					declaredCores = n
				}
			}
		}
	}
	if curPhys != "" && curCore != "" {
		uniqueCores[physCore{physID: curPhys, coreID: curCore}] = struct{}{}
	}

	switch {
	case len(uniqueCores) > 0:
		inv.CPUCores = len(uniqueCores)
	case declaredCores > 0:
		inv.CPUCores = declaredCores
	default:
		inv.CPUCores = inv.CPUThreads
	}
}

func parseMemTotal(data []byte) uint64 {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "MemTotal:"))
		if len(fields) == 0 {
			return 0
		}
		v, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			return 0
		}
		if len(fields) >= 2 && strings.EqualFold(fields[1], "kB") {
			return v * 1024
		}
		return v
	}
	return 0
}

// FormatMetadata enforces valid compact JSON under maxBytes.
func FormatMetadata(v any, maxBytes int) (string, error) {
	if maxBytes <= 0 {
		maxBytes = 64 * 1024
	}
	raw, err := marshalJSON(v)
	if err != nil {
		return "", err
	}
	if len(raw) > maxBytes {
		return "", fmt.Errorf("metadata JSON exceeds max size %d bytes (%d)", maxBytes, len(raw))
	}
	return string(raw), nil
}
