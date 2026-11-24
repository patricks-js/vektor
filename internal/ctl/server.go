package ctl

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type Executor interface {
	Execute(ctx context.Context, req Request) (string, error)
}

type SystemExecutor struct{}

func (SystemExecutor) Execute(ctx context.Context, req Request) (string, error) {
	if err := ValidateRequest(req); err != nil {
		return "", err
	}
	switch req.Action {
	case "service.start", "service.stop", "service.restart", "service.reload", "service.enable", "service.disable":
		verb := strings.TrimPrefix(req.Action, "service.")
		cmd := exec.CommandContext(ctx, "systemctl", verb, req.Target)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = err.Error()
			}
			return "", errors.New(msg)
		}
		return fmt.Sprintf("systemctl %s %s completed", verb, req.Target), nil

	case "process.signal":
		pid, err := strconv.Atoi(req.Target)
		if err != nil || pid <= 1 {
			return "", ErrInvalidAction
		}
		sig := syscall.SIGTERM
		if req.Signal == "KILL" {
			sig = syscall.SIGKILL
		}
		if err := unix.Kill(pid, sig); err != nil {
			return "", err
		}
		return fmt.Sprintf("signal %s sent to pid %d", req.Signal, pid), nil

	case "host.reboot":
		cmd := exec.CommandContext(ctx, "systemctl", "reboot")
		if err := cmd.Run(); err != nil {
			return "", err
		}
		return "host reboot initiated", nil

	case "host.poweroff":
		cmd := exec.CommandContext(ctx, "systemctl", "poweroff")
		if err := cmd.Run(); err != nil {
			return "", err
		}
		return "host poweroff initiated", nil

	default:
		return "", ErrInvalidAction
	}
}

type Server struct {
	allowedUIDs map[uint32]bool
	exec        Executor
	idleTimeout time.Duration
}

func NewServer(allowedUIDs []uint32, exec Executor, idleTimeout time.Duration) *Server {
	allowed := make(map[uint32]bool, len(allowedUIDs))
	for _, uid := range allowedUIDs {
		allowed[uid] = true
	}
	if exec == nil {
		exec = SystemExecutor{}
	}
	return &Server{allowedUIDs: allowed, exec: exec, idleTimeout: idleTimeout}
}

func (s *Server) Serve(ctx context.Context, ln *net.UnixListener) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if s.idleTimeout > 0 {
			_ = ln.SetDeadline(time.Now().Add(s.idleTimeout))
		}
		conn, err := ln.AcceptUnix()
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				// Idle timeout reached under systemd socket activation -> exit cleanly.
				return nil
			}
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		s.handleConn(ctx, conn)
	}
}

func (s *Server) handleConn(ctx context.Context, conn *net.UnixConn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))

	uid, err := peerUID(conn)
	if err != nil || (len(s.allowedUIDs) > 0 && !s.allowedUIDs[uid]) {
		writeResp(conn, Response{OK: false, Error: "unauthorized peer"})
		return
	}

	line, err := bufio.NewReader(io.LimitReader(conn, 8192)).ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		writeResp(conn, Response{OK: false, Error: "invalid request"})
		return
	}
	var req Request
	if err := json.Unmarshal(bytes.TrimSpace(line), &req); err != nil {
		writeResp(conn, Response{OK: false, Error: "malformed json"})
		return
	}
	if err := ValidateRequest(req); err != nil {
		writeResp(conn, Response{OK: false, Error: err.Error()})
		return
	}

	runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	msg, err := s.exec.Execute(runCtx, req)
	if err != nil {
		writeResp(conn, Response{OK: false, Error: err.Error()})
		return
	}
	writeResp(conn, Response{OK: true, Message: msg})
}

func peerUID(conn *net.UnixConn) (uint32, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var ucred *unix.Ucred
	var sockErr error
	if err := raw.Control(func(fd uintptr) {
		ucred, sockErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return 0, err
	}
	if sockErr != nil {
		return 0, sockErr
	}
	return ucred.Uid, nil
}

func writeResp(conn net.Conn, resp Response) {
	raw, err := json.Marshal(resp)
	if err != nil {
		return
	}
	_, _ = conn.Write(append(raw, '\n'))
}
