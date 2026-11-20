package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"vektor/internal/auth"
	"vektor/internal/config"
	"vektor/internal/history"
	"vektor/internal/httpapi"
	"vektor/internal/metrics/scheduler"
	"vektor/web"
)

func runDaemon(parent context.Context, cfg config.HTTPConfig, logger *slog.Logger, sched *scheduler.Scheduler,
	queries *history.QueryService, sessions *auth.Store, hub *scheduler.LiveHub) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.ListenAddr)
	if err != nil {
		if parent.Err() != nil {
			return nil
		}
		return fmt.Errorf("listen HTTP: %w", err)
	}
	defer listener.Close()

	server := httpapi.New(cfg, queries, sessions, hub).WithUI(web.Handler()).Server(ctx)
	served := make(chan error, 1)
	scheduled := make(chan error, 1)
	go func() {
		if cfg.TLSCert != "" {
			served <- server.ServeTLS(listener, cfg.TLSCert, cfg.TLSKey)
		} else {
			served <- server.Serve(listener)
		}
	}()
	go func() { scheduled <- sched.Run(ctx) }()
	logger.Info("HTTP listener bound", slog.String("address", listener.Addr().String()), slog.Bool("tls", cfg.TLSCert != ""))

	var serveErr error
	select {
	case <-parent.Done():
	case err := <-served:
		serveErr = err
		served = nil
	}
	cancel()
	shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	shutdownErr := server.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		_ = server.Close()
	}
	if served != nil {
		serveErr = <-served
	}
	if err := <-scheduled; err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return fmt.Errorf("serve HTTP: %w", serveErr)
	}
	if shutdownErr != nil {
		return fmt.Errorf("shutdown HTTP: %w", shutdownErr)
	}
	logger.Info("vektord stopped cleanly on signal")
	return nil
}
