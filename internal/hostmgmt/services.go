package hostmgmt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidUnit   = errors.New("invalid systemd unit name")
	ErrUnavailable   = errors.New("systemd or journald unavailable")
	ErrPermission    = errors.New("insufficient permissions to read journald logs")
)

type Service struct {
	Name        string `json:"name"`
	LoadState   string `json:"load_state"`
	ActiveState string `json:"active_state"`
	SubState    string `json:"sub_state"`
	UnitFile    string `json:"unit_file_state,omitempty"`
	Description string `json:"description"`
}

type LogEntry struct {
	TimestampMs int64  `json:"timestamp_ms"`
	Priority    string `json:"priority"`
	Unit        string `json:"unit,omitempty"`
	Message     string `json:"message"`
}

// ValidUnitName strictly validates systemd service names without allowing flags or paths.
func ValidUnitName(name string) bool {
	if len(name) == 0 || len(name) > 128 || strings.HasPrefix(name, "-") {
		return false
	}
	for _, ch := range name {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' ||
			ch == '@' || ch == '.' || ch == '_' || ch == '-' || ch == ':') {
			return false
		}
	}
	return true
}

type systemctlUnit struct {
	Unit        string `json:"unit"`
	Load        string `json:"load"`
	Active      string `json:"active"`
	Sub         string `json:"sub"`
	Description string `json:"description"`
}

type systemctlUnitFile struct {
	UnitFile string `json:"unit_file"`
	State    string `json:"state"`
}

func (m *Manager) ListServices(ctx context.Context, stateFilter string) ([]Service, error) {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return nil, ErrUnavailable
	}
	cmd := exec.CommandContext(ctx, "systemctl", "list-units", "--type=service", "--all", "--no-pager", "--no-legend", "--output=json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil && stdout.Len() == 0 {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("systemctl: %s", msg)
		}
		return nil, err
	}

	unitFiles := m.getUnitFiles(ctx)
	return parseSystemctlUnits(stdout.Bytes(), unitFiles, stateFilter)
}

func (m *Manager) getUnitFiles(ctx context.Context) map[string]string {
	m.mu.Lock()
	if time.Since(m.unitFilesAt) < 60*time.Second && m.unitFilesCache != nil {
		cached := m.unitFilesCache
		m.mu.Unlock()
		return cached
	}
	m.mu.Unlock()

	unitFiles := make(map[string]string)
	filesCmd := exec.CommandContext(ctx, "systemctl", "list-unit-files", "--type=service", "--no-pager", "--no-legend", "--output=json")
	var filesOut bytes.Buffer
	filesCmd.Stdout = &filesOut
	if err := filesCmd.Run(); err == nil && filesOut.Len() > 0 {
		var parsed []systemctlUnitFile
		if json.Unmarshal(filesOut.Bytes(), &parsed) == nil {
			for _, f := range parsed {
				unitFiles[f.UnitFile] = f.State
			}
		}
	}

	m.mu.Lock()
	m.unitFilesCache = unitFiles
	m.unitFilesAt = time.Now()
	m.mu.Unlock()
	return unitFiles
}

func parseSystemctlUnits(raw []byte, unitFiles map[string]string, stateFilter string) ([]Service, error) {
	var items []systemctlUnit
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	services := make([]Service, 0, len(items))
	for _, item := range items {
		if item.Unit == "" {
			continue
		}
		switch stateFilter {
		case "active":
			if item.Active != "active" {
				continue
			}
		case "failed":
			if item.Active != "failed" {
				continue
			}
		case "inactive":
			if item.Active == "active" || item.Active == "failed" {
				continue
			}
		}
		services = append(services, Service{
			Name:        item.Unit,
			LoadState:   item.Load,
			ActiveState: item.Active,
			SubState:    item.Sub,
			UnitFile:    unitFiles[item.Unit],
			Description: item.Description,
		})
	}
	sort.Slice(services, func(i, j int) bool {
		// Failed first, then active, then others, then alphabetical.
		rank := func(state string) int {
			switch state {
			case "failed":
				return 0
			case "active":
				return 1
			default:
				return 2
			}
		}
		ri, rj := rank(services[i].ActiveState), rank(services[j].ActiveState)
		if ri != rj {
			return ri < rj
		}
		return services[i].Name < services[j].Name
	})
	return services, nil
}

func priorityArg(level string) string {
	switch level {
	case "err", "error":
		return "3"
	case "warning", "warn":
		return "4"
	case "info":
		return "6"
	default:
		return ""
	}
}

func priorityName(code string) string {
	switch code {
	case "0", "1", "2", "3":
		return "err"
	case "4":
		return "warning"
	case "5", "6":
		return "info"
	case "7":
		return "debug"
	default:
		return "info"
	}
}

func (m *Manager) ServiceLogs(ctx context.Context, unit string, lines int, priority string) ([]LogEntry, error) {
	if !ValidUnitName(unit) {
		return nil, ErrInvalidUnit
	}
	if lines <= 0 || lines > 500 {
		lines = 100
	}
	if _, err := exec.LookPath("journalctl"); err != nil {
		return nil, ErrUnavailable
	}

	args := []string{"-u", unit, "-n", strconv.Itoa(lines), "--no-pager", "-o", "json"}
	if p := priorityArg(priority); p != "" {
		args = append(args, "-p", p)
	}
	cmd := exec.CommandContext(ctx, "journalctl", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	errText := strings.ToLower(strings.TrimSpace(stderr.String()))
	if stdout.Len() == 0 {
		if strings.Contains(errText, "not seeing messages from other users") ||
			strings.Contains(errText, "permission denied") ||
			strings.Contains(errText, "access denied") {
			return nil, ErrPermission
		}
		if err != nil {
			if errText != "" {
				return nil, fmt.Errorf("journalctl: %s", strings.TrimSpace(stderr.String()))
			}
			return nil, err
		}
	}
	return parseJournalJSON(stdout.Bytes()), nil
}

func parseJournalJSON(raw []byte) []LogEntry {
	entries := []LogEntry{}
	for _, line := range bytes.Split(raw, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var obj map[string]any
		if json.Unmarshal(line, &obj) != nil {
			continue
		}
		var tsMs int64
		if rawTS, ok := obj["__REALTIME_TIMESTAMP"].(string); ok {
			if us, err := strconv.ParseInt(rawTS, 10, 64); err == nil {
				tsMs = us / 1000
			}
		}
		msg := ""
		switch v := obj["MESSAGE"].(type) {
		case string:
			msg = v
		case []any:
			buf := make([]byte, 0, len(v))
			for _, b := range v {
				if n, ok := b.(float64); ok {
					buf = append(buf, byte(n))
				}
			}
			msg = string(buf)
		}
		prio, _ := obj["PRIORITY"].(string)
		unit, _ := obj["_SYSTEMD_UNIT"].(string)
		entries = append(entries, LogEntry{
			TimestampMs: tsMs,
			Priority:    priorityName(prio),
			Unit:        unit,
			Message:     msg,
		})
	}
	return entries
}
