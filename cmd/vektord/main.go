package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"vektor/internal/auth"
	"vektor/internal/config"
	"vektor/internal/database"
	"vektor/internal/history"
	"vektor/internal/host"
	"vektor/internal/metrics"
	"vektor/internal/metrics/collector"
	"vektor/internal/metrics/scheduler"
	"vektor/internal/metrics/tracker"
	"vektor/internal/observability"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "vektord: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	var command string
	if len(args) > 0 && (args[0] == "bootstrap-admin" || args[0] == "revoke-sessions") {
		command, args = args[0], args[1:]
	}
	cfg := config.Default()
	applyEnvDefaults(&cfg)
	if err := applyHTTPEnv(&cfg.HTTP); err != nil {
		return err
	}

	logLevelStr := envOrDefault("VEKTOR_LOG_LEVEL", "info")
	var checkMode bool

	fs := flag.NewFlagSet("vektord", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&cfg.DataDir, "data-dir", cfg.DataDir, "Directory for SQLite database and state files (env VEKTOR_DATA_DIR)")
	fs.DurationVar(&cfg.PersistInterval, "persist-interval", cfg.PersistInterval, "Persistence collection interval (env VEKTOR_PERSIST_INTERVAL)")
	fs.DurationVar(&cfg.LiveInterval, "live-interval", cfg.LiveInterval, "On-demand live collection interval (env VEKTOR_LIVE_INTERVAL)")
	fs.DurationVar(&cfg.DiscoveryInterval, "discovery-interval", cfg.DiscoveryInterval, "Resource discovery interval (env VEKTOR_DISCOVERY_INTERVAL)")
	fs.StringVar(&cfg.HTTP.ListenAddr, "http-addr", cfg.HTTP.ListenAddr, "HTTP listen host:port (env VEKTOR_HTTP_ADDR)")
	fs.StringVar(&cfg.HTTP.PublicOrigin, "public-origin", cfg.HTTP.PublicOrigin, "External HTTP(S) origin (env VEKTOR_PUBLIC_ORIGIN)")
	fs.StringVar(&cfg.HTTP.TLSCert, "tls-cert", cfg.HTTP.TLSCert, "HTTPS certificate file (env VEKTOR_TLS_CERT)")
	fs.StringVar(&cfg.HTTP.TLSKey, "tls-key", cfg.HTTP.TLSKey, "HTTPS private key file (env VEKTOR_TLS_KEY)")
	fs.DurationVar(&cfg.HTTP.SessionTTL, "session-ttl", cfg.HTTP.SessionTTL, "Administrative session lifetime (env VEKTOR_SESSION_TTL)")
	fs.IntVar(&cfg.HTTP.MaxLiveClients, "max-live-clients", cfg.HTTP.MaxLiveClients, "Maximum SSE subscribers (env VEKTOR_MAX_LIVE_CLIENTS)")
	fs.StringVar(&logLevelStr, "log-level", logLevelStr, "Structured log level: debug, info, warn, error (env VEKTOR_LOG_LEVEL)")
	fs.BoolVar(&checkMode, "check", false, "Run discovery + single collection/write cycle, print JSON status, and exit")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: vektord [bootstrap-admin | revoke-sessions] [flags]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() > 0 || (command != "" && checkMode) {
		return errors.New("invalid arguments; see --help")
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	logger := observability.NewLogger(stderr, observability.ParseLogLevel(logLevelStr))
	health := observability.NewHealthTracker()
	clk := metrics.NewRealClock()

	db, err := database.Open(ctx, cfg)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = db.Checkpoint(shutdownCtx, "PASSIVE")
		_ = db.Close()
	}()

	sessions := auth.NewStore(db, cfg.HTTP.SessionTTL)
	if command != "" {
		return runAdmin(ctx, command, sessions, stdin, stdout, stderr)
	}

	registry := host.NewRegistry(db)
	disc := collector.NewDiscovery(cfg.ProcRoot, cfg.SysRoot, cfg.DevDiskRoot, cfg.ExcludedFSTypes, cfg.IncludeSensors)
	topo := disc.Topology()

	norm := tracker.NewNormalizer(topo.IncludeNICInHostAggregate)
	fastCols := []metrics.Collector{
		collector.NewCPUCollector(cfg.ProcRoot, clk, cfg.IncludePerCoreCPU),
		collector.NewMemoryCollector(cfg.ProcRoot, clk),
		collector.NewDiskIOCollector(cfg.ProcRoot, clk, topo.DiskLookup),
		collector.NewNetworkCollector(cfg.ProcRoot, clk, topo.NICLookup),
	}
	fsCol := collector.NewFilesystemCollector(clk, topo.Filesystems, nil)
	sensorCol := collector.NewSensorCollector(clk, topo.Sensors)

	writer := history.NewRawWriter(db, cfg, health, logger, clk)
	rollup := history.NewRollupEngine(db, cfg, health, logger)
	retention := history.NewRetentionEngine(db, cfg, health, logger)
	maint := history.NewMaintenanceRunner(db, rollup, retention, logger)
	hub := scheduler.NewLiveHub(health, nil, nil)
	qs := history.NewQueryService(db, registry, cfg, health)
	clockFloor, err := qs.ClockFloor(ctx)
	if err != nil {
		return fmt.Errorf("read historical clock floor: %w", err)
	}

	sched := scheduler.New(scheduler.Deps{
		Config:          cfg,
		Clock:           clk,
		Logger:          logger,
		Health:          health,
		Registry:        registry,
		Discovery:       disc,
		Normalizer:      norm,
		FastCollectors:  fastCols,
		FSCollector:     fsCol,
		SensorCollector: sensorCol,
		RawWriter:       writer,
		Maintenance:     maint,
		LiveHub:         hub,
		ClockFloor:      clockFloor,
	})

	if checkMode {
		// Two rapid persist cycles ensure rate counters (CPU, disk, net) also verify baseline->rate transition,
		// while gauges (memory, load, filesystems, sensors) land on the first pass.
		if err := sched.RunOnce(ctx); err != nil {
			logger.Warn("check cycle reported partial warnings/errors", slog.String("error", err.Error()))
		}
		time.Sleep(150 * time.Millisecond)
		if err := sched.RunPersistOnce(ctx); err != nil && !errors.Is(err, scheduler.ErrClockUntrusted) {
			return fmt.Errorf("second check persist cycle: %w", err)
		}

		hostInfo, err := qs.GetHost(ctx)
		var hostResult *history.HostResponse
		switch {
		case err == nil:
			hostResult = &hostInfo
		case !errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("read host info: %w", err)
		}
		resources, err := qs.ListResources(ctx, "", false)
		if err != nil {
			return fmt.Errorf("list resources: %w", err)
		}
		healthInfo, err := qs.GetHealth(ctx)
		if err != nil {
			return fmt.Errorf("read health info: %w", err)
		}

		counts := make(map[string]int)
		for _, r := range resources {
			counts[r.Kind]++
		}

		out := struct {
			Host           *history.HostResponse  `json:"host"`
			ResourceCounts map[string]int         `json:"resource_counts"`
			Health         history.HealthResponse `json:"health"`
		}{
			Host:           hostResult,
			ResourceCounts: counts,
			Health:         healthInfo,
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	logger.Info("vektord starting",
		slog.String("db_path", db.Path()),
		slog.Duration("persist_interval", cfg.PersistInterval),
		slog.Duration("live_interval", cfg.LiveInterval),
		slog.Duration("discovery_interval", cfg.DiscoveryInterval),
	)

	return runDaemon(ctx, cfg.HTTP, logger, sched, qs, sessions, hub)
}

func applyHTTPEnv(cfg *config.HTTPConfig) error {
	cfg.ListenAddr = envOrDefault("VEKTOR_HTTP_ADDR", cfg.ListenAddr)
	cfg.PublicOrigin = os.Getenv("VEKTOR_PUBLIC_ORIGIN")
	cfg.TLSCert = os.Getenv("VEKTOR_TLS_CERT")
	cfg.TLSKey = os.Getenv("VEKTOR_TLS_KEY")
	if value := os.Getenv("VEKTOR_SESSION_TTL"); value != "" {
		d, err := time.ParseDuration(value)
		if err != nil {
			return errors.New("invalid VEKTOR_SESSION_TTL")
		}
		cfg.SessionTTL = d
	}
	if value := os.Getenv("VEKTOR_MAX_LIVE_CLIENTS"); value != "" {
		count, err := strconv.Atoi(value)
		if err != nil {
			return errors.New("invalid VEKTOR_MAX_LIVE_CLIENTS")
		}
		cfg.MaxLiveClients = count
	}
	return nil
}

func applyEnvDefaults(cfg *config.Config) {
	if v := os.Getenv("VEKTOR_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("VEKTOR_PERSIST_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.PersistInterval = d
		}
	}
	if v := os.Getenv("VEKTOR_LIVE_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.LiveInterval = d
		}
	}
	if v := os.Getenv("VEKTOR_DISCOVERY_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.DiscoveryInterval = d
		}
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
