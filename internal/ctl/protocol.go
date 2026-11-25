// Package ctl implements the typed, allowlisted Unix-socket IPC protocol
// between the unprivileged vektord daemon and the privileged vektor-ctl helper.
package ctl

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"vektor/internal/hostmgmt"
	"vektor/internal/incus"
)

const DefaultSocketPath = "/run/vektor/ctl.sock"

var (
	ErrInvalidAction = errors.New("invalid or disallowed control action")
	ErrUnavailable   = errors.New("vektor-ctl helper socket unavailable")
)

type Request struct {
	Action  string          `json:"action"`
	Target  string          `json:"target,omitempty"`
	Signal  string          `json:"signal,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type Response struct {
	OK      bool            `json:"ok"`
	Message string          `json:"message,omitempty"`
	Error   string          `json:"error,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func actionTimeout(action string) time.Duration {
	switch action {
	case "incus.instance.create", "incus.backup.create":
		return 10 * time.Minute
	case "incus.instance.exec":
		return 65 * time.Second
	case "incus.instance.start", "incus.instance.stop", "incus.instance.force_stop",
		"incus.instance.restart", "incus.instance.freeze", "incus.instance.unfreeze",
		"incus.instance.delete", "incus.instance.update",
		"incus.snapshot.create", "incus.snapshot.restore", "incus.snapshot.delete",
		"incus.backup.delete":
		return 60 * time.Second
	default:
		if strings.HasPrefix(action, "incus.") {
			return 15 * time.Second
		}
		return 10 * time.Second
	}
}

func ValidateRequest(req Request) error {
	switch req.Action {
	case "service.start", "service.stop", "service.restart", "service.reload", "service.enable", "service.disable":
		if !hostmgmt.ValidUnitName(req.Target) {
			return ErrInvalidAction
		}
		if req.Target == "vektord.service" && (req.Action == "service.stop" || req.Action == "service.disable") {
			return ErrInvalidAction
		}
		return nil
	case "process.signal":
		pid, err := strconv.Atoi(req.Target)
		if err != nil || pid <= 1 {
			return ErrInvalidAction
		}
		if req.Signal != "TERM" && req.Signal != "KILL" {
			return ErrInvalidAction
		}
		return nil
	case "host.reboot", "host.poweroff":
		return nil
	case "incus.status", "incus.instances.list", "incus.resources.list":
		return nil
	case "incus.instance.get", "incus.instance.start", "incus.instance.stop", "incus.instance.force_stop",
		"incus.instance.restart", "incus.instance.freeze", "incus.instance.unfreeze",
		"incus.instance.delete", "incus.instance.console",
		"incus.snapshots.list", "incus.snapshot.create",
		"incus.backups.list", "incus.backup.create":
		if !incus.ValidInstanceName(req.Target) {
			return ErrInvalidAction
		}
		return nil
	case "incus.instance.update", "incus.instance.exec":
		if !incus.ValidInstanceName(req.Target) || len(req.Payload) == 0 {
			return ErrInvalidAction
		}
		return nil
	case "incus.instance.create":
		if len(req.Payload) == 0 {
			return ErrInvalidAction
		}
		return nil
	case "incus.snapshot.restore", "incus.snapshot.delete", "incus.backup.delete":
		if !incus.ValidInstanceName(req.Target) || !incus.ValidSnapshotName(req.Signal) {
			return ErrInvalidAction
		}
		return nil
	default:
		return ErrInvalidAction
	}
}

type Client struct {
	socketPath string
	timeout    time.Duration
}

func NewClient(socketPath string) *Client {
	if socketPath == "" {
		socketPath = DefaultSocketPath
	}
	return &Client{socketPath: socketPath, timeout: 10 * time.Second}
}

func (c *Client) Do(ctx context.Context, req Request) (Response, error) {
	if err := ValidateRequest(req); err != nil {
		return Response{}, err
	}
	timeout := actionTimeout(req.Action)
	if c.timeout > timeout {
		timeout = c.timeout
	}
	var dialer net.Dialer
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn, err := dialer.DialContext(dialCtx, "unix", c.socketPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, net.ErrClosed) {
			return Response{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		return Response{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer conn.Close()

	if deadline, ok := dialCtx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return Response{}, err
	}
	if _, err := conn.Write(append(raw, '\n')); err != nil {
		return Response{}, err
	}

	line, err := bufio.NewReader(io.LimitReader(conn, 8<<20)).ReadBytes('\n')
	if err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return Response{}, err
	}
	return resp, nil
}
