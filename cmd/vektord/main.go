package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

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

	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "vektord: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	cfg := config.Default()
	applyEnvDefaults(&cfg)

	logLevelStr := envOrDefault("VEKTOR_LOG_LEVEL", "info")
	var checkMode bool

	fs := flag.NewFlagSet("vektord", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&cfg.DataDir, "data-dir", cfg.DataDir, "Directory for SQLite database and state files (env VEKTOR_DATA_DIR)")
	fs.DurationVar(&cfg.PersistInterval, "persist-interval", cfg.PersistInterval, "Persistence collection interval (env VEKTOR_PERSIST_INTERVAL)")
	fs.DurationVar(&cfg.LiveInterval, "live-interval", cfg.LiveInterval, "On-demand live collection interval (env VEKTOR_LIVE_INTERVAL)")
	fs.DurationVar(&cfg.DiscoveryInterval, "discovery-interval", cfg.DiscoveryInterval, "Resource discovery interval (env VEKTOR_DISCOVERY_INTERVAL)")
	fs.StringVar(&logLevelStr, "log-level", logLevelStr, "Structured log level: debug, info, warn, error (env VEKTOR_LOG_LEVEL)")
	fs.BoolVar(&checkMode, "check", false, "Run discovery + single collection/write cycle, print JSON status, and exit")
	if err := fs.Parse(args); err != nil {
		return err
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

	writer := history.NewRawWriter(db, health, logger, clk)
	rollup := history.NewRollupEngine(db, cfg, health, logger)
	retention := history.NewRetentionEngine(db, cfg, health, logger)
	maint := history.NewMaintenanceRunner(db, rollup, retention, logger)
	hub := scheduler.NewLiveHub(health, nil, nil)
	qs := history.NewQueryService(db, registry, cfg, health, clk)

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
	})

	if checkMode {
		// Two rapid persist cycles ensure rate counters (CPU, disk, net) also verify baseline->rate transition,
		// while gauges (memory, load, filesystems, sensors) land on the first pass.
		if err := sched.RunOnce(ctx); err != nil {
			logger.Warn("check cycle reported partial warnings/errors", slog.String("error", err.Error()))
		}
		time.Sleep(150 * time.Millisecond)
		if err := sched.RunPersistOnce(ctx); err != nil {
			return fmt.Errorf("second check persist cycle: %w", err)
		}

		hostInfo, err := qs.GetHost(ctx)
		if err != nil {
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
			Host           history.HostResponse   `json:"host"`
			ResourceCounts map[string]int         `json:"resource_counts"`
			Health         history.HealthResponse `json:"health"`
		}{
			Host:           hostInfo,
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

	err = sched.Run(ctx)
	if errors.Is(err, context.Canceled) {
		logger.Info("vektord stopped cleanly on signal")
		return nil
	}
	return err
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
