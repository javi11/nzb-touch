package config

import (
	"context"
	"os"
	"time"

	"github.com/javi11/nntppool"
	"gopkg.in/yaml.v3"
)

// Logger interface compatible with slog.Logger
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
	DebugContext(ctx context.Context, msg string, args ...any)
	InfoContext(ctx context.Context, msg string, args ...any)
	WarnContext(ctx context.Context, msg string, args ...any)
	ErrorContext(ctx context.Context, msg string, args ...any)
}

type Config struct {
	// By default the number of connections for download providers is the sum of all MaxConnections
	DownloadWorkers   int                             `yaml:"download_workers"`
	DownloadProviders []nntppool.UsenetProviderConfig `yaml:"download_providers"`

	// Scanner configuration
	Scanner Scanner `yaml:"scanner"`
}

type Scanner struct {
	Enabled           bool          `yaml:"enabled"`
	WatchDirectories  []string      `yaml:"watch_directories"`
	ScanInterval      time.Duration `yaml:"scan_interval"` // duration string like "5m", "1h"
	MaxFilesPerDay    int           `yaml:"max_files_per_day"`
	ConcurrentJobs    int           `yaml:"concurrent_jobs"`
	DatabasePath      string        `yaml:"database_path"`      // Path to SQLite database file
	ReprocessInterval time.Duration `yaml:"reprocess_interval"` // Duration after which to reprocess an item ("0" to disable)
	FailedDirectory   string        `yaml:"failed_directory"`   // Directory where failed NZBs are moved to
}

type Option func(*Config)

var (
	providerConfigDefault = nntppool.Provider{
		MaxConnections:                 10,
		MaxConnectionIdleTimeInSeconds: 2400,
	}
	downloadWorkersDefault = 10
	scannerDefault         = Scanner{
		Enabled:           false,
		ScanInterval:      30 * time.Minute, // Default: 30 minutes
		MaxFilesPerDay:    50,               // Default: 50 files per day
		ConcurrentJobs:    1,                // Default: 1 concurrent job
		DatabasePath:      "queue.db",       // Default database path
		ReprocessInterval: 0,                // Default: don't reprocess (0 = disabled)
		FailedDirectory:   "",               // Default: no failed directory
	}
)

func mergeWithDefault(config ...Config) Config {
	if len(config) == 0 {
		return Config{
			DownloadProviders: []nntppool.UsenetProviderConfig{},
			DownloadWorkers:   downloadWorkersDefault,
			Scanner: Scanner{
				Enabled:           scannerDefault.Enabled,
				ScanInterval:      scannerDefault.ScanInterval,
				MaxFilesPerDay:    scannerDefault.MaxFilesPerDay,
				ConcurrentJobs:    scannerDefault.ConcurrentJobs,
				DatabasePath:      scannerDefault.DatabasePath,
				ReprocessInterval: scannerDefault.ReprocessInterval,
				FailedDirectory:   scannerDefault.FailedDirectory,
			},
		}
	}

	cfg := config[0]

	downloadWorkers := 0
	for i, p := range cfg.DownloadProviders {
		if p.MaxConnections == 0 {
			p.MaxConnections = providerConfigDefault.MaxConnections
		}

		if p.MaxConnectionIdleTimeInSeconds == 0 {
			p.MaxConnectionIdleTimeInSeconds = providerConfigDefault.MaxConnectionIdleTimeInSeconds
		}

		cfg.DownloadProviders[i] = p
		downloadWorkers += p.MaxConnections
	}

	if cfg.DownloadWorkers == 0 {
		cfg.DownloadWorkers = downloadWorkers
	}

	// Apply scanner defaults if not set
	if cfg.Scanner.ScanInterval == 0 {
		cfg.Scanner.ScanInterval = scannerDefault.ScanInterval
	}

	if cfg.Scanner.MaxFilesPerDay == 0 {
		cfg.Scanner.MaxFilesPerDay = scannerDefault.MaxFilesPerDay
	}

	if cfg.Scanner.ConcurrentJobs == 0 {
		cfg.Scanner.ConcurrentJobs = scannerDefault.ConcurrentJobs
	}

	if cfg.Scanner.DatabasePath == "" {
		cfg.Scanner.DatabasePath = scannerDefault.DatabasePath
	}

	if cfg.Scanner.ReprocessInterval == 0 {
		cfg.Scanner.ReprocessInterval = scannerDefault.ReprocessInterval
	}

	return cfg
}

func NewFromFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}

	return mergeWithDefault(cfg), nil
}

// GetScanInterval returns the scan interval duration
func (c *Config) GetScanInterval() (time.Duration, error) {
	return c.Scanner.ScanInterval, nil
}

// GetReprocessInterval returns the reprocess interval duration
func (c *Config) GetReprocessInterval() (time.Duration, error) {
	return c.Scanner.ReprocessInterval, nil
}
