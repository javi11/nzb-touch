package nzbtouch

import (
	"context"
	"log/slog"
	"os"

	"github.com/javi11/nntppool"
	"github.com/javi11/nzb-touch/internal/config"
	"github.com/javi11/nzb-touch/internal/nzb"
	"github.com/javi11/nzb-touch/internal/processor"
	"github.com/spf13/cobra"
)

var (
	nzbFile    string
	configFile string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "nzbtouch",
	Short: "NZB Touch - Download NZB articles from Usenet",
	Long: `NZB Touch is a tool for downloading NZB articles from Usenet servers.
It can be used to test download speeds, verify article availability, or 
validate NZB files without storing the downloaded content.`,
	Run: func(cmd *cobra.Command, args []string) {
		if nzbFile == "" {
			slog.Error("Error: NZB file is required")
			_ = cmd.Help()
			os.Exit(1)
		}

		if configFile == "" {
			slog.Error("Error: Config file is required")
			_ = cmd.Help()
			os.Exit(1)
		}

		// Read config file
		cfg, err := config.NewFromFile(configFile)
		if err != nil {
			slog.Error("Failed to load config", "error", err)
			os.Exit(2)
		}

		// Load and parse NZB file
		nzbData, err := nzb.LoadFromFile(nzbFile)
		if err != nil {
			slog.Error("Failed to load NZB file", "error", err)
			os.Exit(3)
		}

		// Display NZB information
		nzbData.PrintInfo()

		// Create NNTP connection pool
		pool, err := nntppool.NewConnectionPool(
			nntppool.Config{Providers: cfg.DownloadProviders},
		)
		if err != nil {
			slog.Error("Error creating connection pool", "error", err)
			os.Exit(4)
		}
		defer pool.Quit()

		// Create processor with configured download workers
		proc := processor.New(pool, nzbData.TotalSegments, cfg.DownloadWorkers)

		// Start download
		ctx := context.Background()
		if err := proc.ProcessNZB(ctx, nzbData.Nzb); err != nil {
			slog.Error("Error processing NZB", "error", err)
			os.Exit(5)
		}
	},
}

func init() {
	rootCmd.Flags().StringVarP(&nzbFile, "nzb", "n", "", "Path to NZB file (required)")
	rootCmd.Flags().StringVarP(&configFile, "config", "c", "", "Path to YAML config file (required)")

	_ = rootCmd.MarkFlagRequired("nzb")
	_ = rootCmd.MarkFlagRequired("config")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		slog.Error("Command execution failed", "error", err)
		os.Exit(1)
	}
}
