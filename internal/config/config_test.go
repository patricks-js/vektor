package config

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultConfigValid(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Default().Validate() returned unexpected error: %v", err)
	}
	if got, want := cfg.EffectiveDBPath(), filepath.Join("./data", "vektor.db"); got != want {
		t.Fatalf("EffectiveDBPath() = %q, want %q", got, want)
	}
	cfg.DBPath = "/var/lib/vektor/custom.db"
	if got := cfg.EffectiveDBPath(); got != "/var/lib/vektor/custom.db" {
		t.Fatalf("EffectiveDBPath() with explicit DBPath = %q", got)
	}
}

func TestConfigValidationRejectsDangerousOrInvalidValues(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name:    "persist too frequent",
			mutate:  func(c *Config) { c.PersistInterval = 2 * time.Second },
			wantErr: "persist_interval",
		},
		{
			name:    "live too frequent",
			mutate:  func(c *Config) { c.LiveInterval = 100 * time.Millisecond },
			wantErr: "live_interval",
		},
		{
			name:    "negative discovery",
			mutate:  func(c *Config) { c.DiscoveryInterval = -5 * time.Second },
			wantErr: "discovery_interval",
		},
		{
			name:    "raw retention below 1h",
			mutate:  func(c *Config) { c.RawRetention = 30 * time.Minute },
			wantErr: "raw_retention",
		},
		{
			name:    "medium below raw",
			mutate:  func(c *Config) { c.MediumRetention = 12 * time.Hour },
			wantErr: "medium_retention",
		},
		{
			name:    "long below medium",
			mutate:  func(c *Config) { c.LongRetention = 2 * 24 * time.Hour },
			wantErr: "long_retention",
		},
		{
			name:    "invalid concurrency",
			mutate:  func(c *Config) { c.MaxFastConcurrency = 64 },
			wantErr: "max_fast_concurrency",
		},
		{
			name:    "invalid sqlite synchronous",
			mutate:  func(c *Config) { c.SQLiteSynchronous = "OFF" },
			wantErr: "sqlite_synchronous",
		},
		{
			name:    "empty data dir and db path",
			mutate:  func(c *Config) { c.DataDir = ""; c.DBPath = "" },
			wantErr: "data_dir or db_path",
		},
		{
			name:    "evict shorter than persist",
			mutate:  func(c *Config) { c.CounterEvictAfter = 5 * time.Second },
			wantErr: "counter_evict_after",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			tc.mutate(&cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not mention %q", err.Error(), tc.wantErr)
			}
		})
	}
}
