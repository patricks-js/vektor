package collector

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxProcSysFileBytes = 4 << 20 // 4 MiB safety cap for /proc and /sys files.

// ReadBootID reads the kernel's ephemeral boot UUID from <procRoot>/sys/kernel/random/boot_id.
func ReadBootID(procRoot string) (string, error) {
	if procRoot == "" {
		procRoot = "/proc"
	}
	b, err := readFileBounded(filepath.Join(procRoot, "sys/kernel/random/boot_id"), 1024)
	if err != nil {
		return "", fmt.Errorf("read boot_id: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

func readFileBounded(path string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = maxProcSysFileBytes
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes))
	if err != nil {
		return nil, err
	}
	return data, nil
}
