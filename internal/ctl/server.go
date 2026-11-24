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
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"vektor/internal/incus"
)

type Executor interface {
	Execute(ctx context.Context, req Request) (string, error)
}

type DataExecutor interface {
	ExecuteData(ctx context.Context, req Request) (Response, error)
}

type SystemExecutor struct {
	Incus *incus.Client
}

func (e SystemExecutor) incusClient() *incus.Client {
	if e.Incus != nil {
		return e.Incus
	}
	return incus.NewClient(os.Getenv("VEKTOR_INCUS_SOCKET"))
}

func (e SystemExecutor) Execute(ctx context.Context, req Request) (string, error) {
	resp, err := e.ExecuteData(ctx, req)
	if err != nil {
		return "", err
	}
	if !resp.OK {
		return "", errors.New(resp.Error)
	}
	return resp.Message, nil
}

func marshalData(v any) (json.RawMessage, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func (e SystemExecutor) ExecuteData(ctx context.Context, req Request) (Response, error) {
	if err := ValidateRequest(req); err != nil {
		return Response{}, err
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
			return Response{}, errors.New(msg)
		}
		return Response{OK: true, Message: fmt.Sprintf("systemctl %s %s completed", verb, req.Target)}, nil

	case "process.signal":
		pid, err := strconv.Atoi(req.Target)
		if err != nil || pid <= 1 {
			return Response{}, ErrInvalidAction
		}
		sig := syscall.SIGTERM
		if req.Signal == "KILL" {
			sig = syscall.SIGKILL
		}
		if err := unix.Kill(pid, sig); err != nil {
			return Response{}, err
		}
		return Response{OK: true, Message: fmt.Sprintf("signal %s sent to pid %d", req.Signal, pid)}, nil

	case "host.reboot":
		cmd := exec.CommandContext(ctx, "systemctl", "reboot")
		if err := cmd.Run(); err != nil {
			return Response{}, err
		}
		return Response{OK: true, Message: "host reboot initiated"}, nil

	case "host.poweroff":
		cmd := exec.CommandContext(ctx, "systemctl", "poweroff")
		if err := cmd.Run(); err != nil {
			return Response{}, err
		}
		return Response{OK: true, Message: "host poweroff initiated"}, nil

	case "incus.status":
		st, err := e.incusClient().Status(ctx)
		if err != nil {
			return Response{}, err
		}
		data, err := marshalData(st)
		if err != nil {
			return Response{}, err
		}
		return Response{OK: true, Data: data}, nil

	case "incus.instances.list":
		list, err := e.incusClient().ListInstances(ctx)
		if err != nil {
			return Response{}, err
		}
		data, err := marshalData(list)
		if err != nil {
			return Response{}, err
		}
		return Response{OK: true, Data: data}, nil

	case "incus.instance.get":
		inst, err := e.incusClient().GetInstance(ctx, req.Target)
		if err != nil {
			return Response{}, err
		}
		data, err := marshalData(inst)
		if err != nil {
			return Response{}, err
		}
		return Response{OK: true, Data: data}, nil

	case "incus.instance.start", "incus.instance.stop", "incus.instance.force_stop",
		"incus.instance.restart", "incus.instance.freeze", "incus.instance.unfreeze":
		verb := strings.TrimPrefix(req.Action, "incus.instance.")
		if err := e.incusClient().ChangeState(ctx, req.Target, verb); err != nil {
			return Response{}, err
		}
		return Response{OK: true, Message: fmt.Sprintf("incus %s %s completed", verb, req.Target)}, nil

	case "incus.instance.create":
		var createReq incus.CreateInstanceRequest
		if err := json.Unmarshal(req.Payload, &createReq); err != nil {
			return Response{}, errors.New("invalid create instance payload")
		}
		if err := e.incusClient().CreateInstance(ctx, createReq); err != nil {
			return Response{}, err
		}
		return Response{OK: true, Message: fmt.Sprintf("instance %s created", createReq.Name)}, nil

	case "incus.instance.delete":
		if err := e.incusClient().DeleteInstance(ctx, req.Target); err != nil {
			return Response{}, err
		}
		return Response{OK: true, Message: fmt.Sprintf("instance %s deleted", req.Target)}, nil

	case "incus.snapshots.list":
		snaps, err := e.incusClient().ListSnapshots(ctx, req.Target)
		if err != nil {
			return Response{}, err
		}
		data, err := marshalData(snaps)
		if err != nil {
			return Response{}, err
		}
		return Response{OK: true, Data: data}, nil

	case "incus.snapshot.create":
		var snapReq incus.CreateSnapshotRequest
		if err := json.Unmarshal(req.Payload, &snapReq); err != nil {
			return Response{}, errors.New("invalid snapshot payload")
		}
		if err := e.incusClient().CreateSnapshot(ctx, req.Target, snapReq); err != nil {
			return Response{}, err
		}
		return Response{OK: true, Message: fmt.Sprintf("snapshot %s/%s created", req.Target, snapReq.Name)}, nil

	case "incus.snapshot.restore":
		if err := e.incusClient().RestoreSnapshot(ctx, req.Target, req.Signal); err != nil {
			return Response{}, err
		}
		return Response{OK: true, Message: fmt.Sprintf("snapshot %s/%s restored", req.Target, req.Signal)}, nil

	case "incus.snapshot.delete":
		if err := e.incusClient().DeleteSnapshot(ctx, req.Target, req.Signal); err != nil {
			return Response{}, err
		}
		return Response{OK: true, Message: fmt.Sprintf("snapshot %s/%s deleted", req.Target, req.Signal)}, nil

	case "incus.resources.list":
		res, err := e.incusClient().ListResources(ctx)
		if err != nil {
			return Response{}, err
		}
		data, err := marshalData(res)
		if err != nil {
			return Response{}, err
		}
		return Response{OK: true, Data: data}, nil

	default:
		return Response{}, ErrInvalidAction
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

	line, err := bufio.NewReader(io.LimitReader(conn, 64*1024)).ReadBytes('\n')
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

	timeout := actionTimeout(req.Action)
	_ = conn.SetDeadline(time.Now().Add(timeout + 5*time.Second))
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if dataExec, ok := s.exec.(DataExecutor); ok {
		resp, err := dataExec.ExecuteData(runCtx, req)
		if err != nil {
			writeResp(conn, Response{OK: false, Error: err.Error()})
			return
		}
		writeResp(conn, resp)
		return
	}
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
