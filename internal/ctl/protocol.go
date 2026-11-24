// Package ctl implements the typed, allowlisted Unix-socket IPC protocol
// between the unprivileged vektord daemon and the privileged vektor-ctl helper.
package ctl

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"vektor/internal/hostmgmt"
)

const DefaultSocketPath = "/run/vektor/ctl.sock"

var (
	ErrInvalidAction = errors.New("invalid or disallowed control action")
	ErrUnavailable   = errors.New("vektor-ctl helper socket unavailable")
)

type Request struct {
	Action string `json:"action"`
	Target string `json:"target,omitempty"`
	Signal string `json:"signal,omitempty"`
}

type Response struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
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
	var dialer net.Dialer
	dialCtx, cancel := context.WithTimeout(ctx, c.timeout)
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

	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return Response{}, err
	}
	return resp, nil
}
