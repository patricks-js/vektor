// Package hostmgmt provides read-only host operational visibility:
// processes from /proc, systemd services, and recent journald logs.
package hostmgmt

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Process struct {
	PID        int     `json:"pid"`
	PPID       int     `json:"ppid"`
	Name       string  `json:"name"`
	Command    string  `json:"command"`
	User       string  `json:"user"`
	State      string  `json:"state"`
	CPUPercent float64 `json:"cpu_percent"`
	RSSBytes   uint64  `json:"rss_bytes"`
	Threads    int     `json:"threads"`
}

type procSample struct {
	cpuTicks uint64
	at       time.Time
}

type Manager struct {
	procRoot string
	pageSize uint64
	clkTck   float64

	mu       sync.Mutex
	prev     map[int]procSample
	uidCache map[int]string
	uidAt    time.Time
}

func New(procRoot string) *Manager {
	if procRoot == "" {
		procRoot = "/proc"
	}
	pageSize := uint64(os.Getpagesize())
	if pageSize == 0 {
		pageSize = 4096
	}
	return &Manager{
		procRoot: procRoot,
		pageSize: pageSize,
		clkTck:   100, // Standard USER_HZ on Linux x86_64/arm64
		prev:     make(map[int]procSample),
		uidCache: make(map[int]string),
	}
}

func (m *Manager) lookupUser(uid int) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if time.Since(m.uidAt) > 5*time.Minute || len(m.uidCache) == 0 {
		m.uidCache = parsePasswd("/etc/passwd")
		m.uidAt = time.Now()
	}
	if name, ok := m.uidCache[uid]; ok {
		return name
	}
	return strconv.Itoa(uid)
}

func parsePasswd(path string) map[int]string {
	out := make(map[int]string)
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		parts := strings.Split(sc.Text(), ":")
		if len(parts) >= 3 {
			if uid, err := strconv.Atoi(parts[2]); err == nil {
				out[uid] = parts[0]
			}
		}
	}
	return out
}

func (m *Manager) ListProcesses(ctx context.Context, sortBy string, limit int) ([]Process, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	entries, err := os.ReadDir(m.procRoot)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	nextSamples := make(map[int]procSample, len(entries))
	procs := make([]Process, 0, 128)

	m.mu.Lock()
	prev := m.prev
	m.mu.Unlock()

	for _, entry := range entries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		statRaw, err := os.ReadFile(filepath.Join(m.procRoot, entry.Name(), "stat"))
		if err != nil {
			continue
		}
		p, ticks, ok := parseProcStat(pid, string(statRaw), m.pageSize)
		if !ok {
			continue
		}
		nextSamples[pid] = procSample{cpuTicks: ticks, at: now}
		if old, existed := prev[pid]; existed {
			dt := now.Sub(old.at).Seconds()
			if dt > 0.05 && ticks >= old.cpuTicks {
				deltaSec := float64(ticks-old.cpuTicks) / m.clkTck
				p.CPUPercent = (deltaSec / dt) * 100
			}
		}

		if statusRaw, err := os.ReadFile(filepath.Join(m.procRoot, entry.Name(), "status")); err == nil {
			if uid, ok := parseProcUID(string(statusRaw)); ok {
				p.User = m.lookupUser(uid)
			}
		}
		if cmdRaw, err := os.ReadFile(filepath.Join(m.procRoot, entry.Name(), "cmdline")); err == nil && len(cmdRaw) > 0 {
			cmd := strings.TrimSpace(strings.ReplaceAll(string(cmdRaw), "\x00", " "))
			if len(cmd) > 200 {
				cmd = cmd[:200] + "…"
			}
			p.Command = cmd
		}
		if p.Command == "" {
			p.Command = "[" + p.Name + "]"
		}
		procs = append(procs, p)
	}

	m.mu.Lock()
	m.prev = nextSamples
	m.mu.Unlock()

	sort.Slice(procs, func(i, j int) bool {
		if sortBy == "memory" {
			if procs[i].RSSBytes != procs[j].RSSBytes {
				return procs[i].RSSBytes > procs[j].RSSBytes
			}
			return procs[i].CPUPercent > procs[j].CPUPercent
		}
		if procs[i].CPUPercent != procs[j].CPUPercent {
			return procs[i].CPUPercent > procs[j].CPUPercent
		}
		if procs[i].RSSBytes != procs[j].RSSBytes {
			return procs[i].RSSBytes > procs[j].RSSBytes
		}
		return procs[i].PID < procs[j].PID
	})

	if len(procs) > limit {
		procs = procs[:limit]
	}
	return procs, nil
}

// parseProcStat extracts fields from /proc/<pid>/stat safely handling spaces/parens in comm.
func parseProcStat(pid int, raw string, pageSize uint64) (Process, uint64, bool) {
	open := strings.IndexByte(raw, '(')
	close := strings.LastIndexByte(raw, ')')
	if open < 0 || close <= open || close+2 >= len(raw) {
		return Process{}, 0, false
	}
	name := raw[open+1 : close]
	fields := strings.Fields(raw[close+2:])
	// fields[0]=state, [1]=ppid, [11]=utime, [12]=stime, [17]=num_threads, [21]=rss
	if len(fields) < 22 {
		return Process{}, 0, false
	}
	ppid, _ := strconv.Atoi(fields[1])
	utime, _ := strconv.ParseUint(fields[11], 10, 64)
	stime, _ := strconv.ParseUint(fields[12], 10, 64)
	threads, _ := strconv.Atoi(fields[17])
	rssPages, _ := strconv.ParseUint(fields[21], 10, 64)

	return Process{
		PID:      pid,
		PPID:     ppid,
		Name:     name,
		State:    fields[0],
		RSSBytes: rssPages * pageSize,
		Threads:  threads,
	}, utime + stime, true
}

func parseProcUID(status string) (int, bool) {
	for _, line := range strings.Split(status, "\n") {
		if strings.HasPrefix(line, "Uid:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				uid, err := strconv.Atoi(fields[1])
				if err == nil {
					return uid, true
				}
			}
			break
		}
	}
	return 0, false
}
