package nzbtouch

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/javi11/nntppool/v2"
	"github.com/javi11/nzb-touch/internal/config"
	"github.com/javi11/nzb-touch/internal/processor"
	"github.com/spf13/cobra"
)

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan directories for NZB files to process",
	Long: `Continuously scan directories for NZB files and process them.
The scanner will run at the configured interval and respect daily limits.`,
	Run: func(cmd *cobra.Command, args []string) {
		if configFile == "" {
			slog.Error("Error: Config file is required")
			_ = cmd.Help()
			os.Exit(1)
		}

		// Read config file
		cfg, err := config.NewFromFile(configFile)
		if err != nil {
			slog.Error("Failed to load config", "error", err)
			os.Exit(1)
		}

		// Check if scanner is enabled in config
		if !cfg.Scanner.Enabled {
			slog.Error("Scanner is not enabled in config")
			os.Exit(1)
		}

		// Check if watch directories are configured
		if len(cfg.Scanner.WatchDirectories) == 0 {
			slog.Error("No watch directories configured")
			os.Exit(1)
		}

		// Validate check percent
		if cfg.Scanner.CheckPercent <= 0 || cfg.Scanner.CheckPercent > 100 {
			slog.Error("Error: checkpercent must be between 1 and 100")
			os.Exit(1)
		}

		// Parse scan interval
		scanInterval, err := cfg.GetScanInterval()
		if err != nil {
			slog.Error("Invalid scan interval", "error", err)
			os.Exit(1)
		}

		// Parse reprocess interval
		reprocessInterval, err := cfg.GetReprocessInterval()
		if err != nil {
			slog.Error("Invalid reprocess interval", "error", err)
			os.Exit(1)
		}

		// Create NNTP connection pool
		pool, err := nntppool.NewConnectionPool(
			nntppool.Config{Providers: cfg.DownloadProviders},
		)
		if err != nil {
			slog.Error("Error creating connection pool", "error", err)
			os.Exit(1)
		}
		defer pool.Quit()

		// Create processor
		proc := processor.New(pool, 0, cfg.DownloadWorkers)

		// Create directory scanner
		scanner, err := processor.NewDirectoryScanner(
			proc,
			cfg.Scanner.WatchDirectories,
			scanInterval,
			cfg.Scanner.MaxFilesPerDay,
			cfg.Scanner.ConcurrentJobs,
			cfg.Scanner.DatabasePath,
			reprocessInterval,
			cfg.Scanner.FailedDirectory,
			cfg.Scanner.CheckPercent,
			cfg.Scanner.MissingPercent,
		)
		if err != nil {
			slog.Error("Failed to create directory scanner", "error", err)
			os.Exit(1)
		}

		// Set up context with cancellation for graceful shutdown
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Set up signal handling for graceful shutdown
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigs
			slog.Info("Shutting down scanner...")
			cancel()
		}()

		// Start scanner and wait for it to complete
		slog.Info("Starting scanner...",
			"interval", scanInterval,
			"max_files_per_day", cfg.Scanner.MaxFilesPerDay,
			"watch_dirs", cfg.Scanner.WatchDirectories,
			"reprocess_interval", reprocessInterval,
			"failed_directory", cfg.Scanner.FailedDirectory,
		)

		err = scanner.Start(ctx)
		if err != nil && err != context.Canceled {
			slog.Error("Scanner error", "error", err)
			os.Exit(1)
		}
	},
}

func init() {
	scanCmd.Flags().StringVarP(&configFile, "config", "c", "", "Path to YAML config file (required)")
	_ = scanCmd.MarkFlagRequired("config")

	rootCmd.AddCommand(scanCmd)
}
