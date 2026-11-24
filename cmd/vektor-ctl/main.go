// Command vektor-ctl is the minimal, socket-activated privileged helper for Vektor.
// It validates callers via SO_PEERCRED and executes only strictly allowlisted actions.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"syscall"
	"time"

	"vektor/internal/ctl"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:]); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "vektor-ctl: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	var socketPath string
	var allowedUser string
	var idleTimeout time.Duration
	var showVersion bool

	fs := flag.NewFlagSet("vektor-ctl", flag.ContinueOnError)
	fs.StringVar(&socketPath, "socket", ctl.DefaultSocketPath, "Unix socket path (ignored when systemd socket activation is used)")
	fs.StringVar(&allowedUser, "allow-user", "vektor", "System username allowed to send control commands (root is always allowed)")
	fs.DurationVar(&idleTimeout, "idle-timeout", 30*time.Second, "Exit after being idle for this duration (0 disables)")
	fs.BoolVar(&showVersion, "version", false, "Print version and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if showVersion {
		fmt.Printf("vektor-ctl %s\n", version)
		return nil
	}

	allowedUIDs := []uint32{0} // root
	if u, err := user.Lookup(allowedUser); err == nil {
		if uid, err := strconv.ParseUint(u.Uid, 10, 32); err == nil {
			allowedUIDs = append(allowedUIDs, uint32(uid))
		}
	}

	ln, err := acquireListener(socketPath)
	if err != nil {
		return err
	}
	defer ln.Close()

	srv := ctl.NewServer(allowedUIDs, ctl.SystemExecutor{}, idleTimeout)
	return srv.Serve(ctx, ln)
}

func acquireListener(socketPath string) (*net.UnixListener, error) {
	// Systemd socket activation passes the pre-bound socket on FD 3.
	if os.Getenv("LISTEN_FDS") == "1" && os.Getenv("LISTEN_PID") == strconv.Itoa(os.Getpid()) {
		f := os.NewFile(uintptr(3), "systemd-socket")
		defer f.Close()
		ln, err := net.FileListener(f)
		if err != nil {
			return nil, fmt.Errorf("adopt systemd socket: %w", err)
		}
		unixLn, ok := ln.(*net.UnixListener)
		if !ok {
			_ = ln.Close()
			return nil, errors.New("systemd socket is not a unix listener")
		}
		return unixLn, nil
	}

	_ = os.Remove(socketPath)
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", socketPath, err)
	}
	_ = os.Chmod(socketPath, 0o660)
	return ln, nil
}
