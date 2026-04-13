package nzbtouch

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/javi11/nntppool/v2"
	"github.com/javi11/nzb-touch/internal/config"
	"github.com/spf13/cobra"
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show provider status and pool metrics",
	Long: `Connect to all configured Usenet providers and display their current
status, connection counts, and accumulated pool metrics.`,
	Run: func(cmd *cobra.Command, args []string) {
		if configFile == "" {
			slog.Error("Error: Config file is required")
			_ = cmd.Help()
			os.Exit(1)
		}

		cfg, err := config.NewFromFile(configFile)
		if err != nil {
			slog.Error("Failed to load config", "error", err)
			os.Exit(1)
		}

		pool, err := nntppool.NewConnectionPool(
			nntppool.Config{Providers: cfg.DownloadProviders},
		)
		if err != nil {
			slog.Error("Error creating connection pool", "error", err)
			os.Exit(1)
		}
		defer pool.Quit()

		printProviderInfo(pool.GetProvidersInfo())
		printPoolMetrics(pool.GetMetricsSnapshot())
	},
}

func printProviderInfo(providers []nntppool.ProviderInfo) {
	fmt.Println("\n=== Provider Status ===")
	fmt.Printf("%-30s %-10s %-8s %-8s %-20s\n", "HOST", "STATE", "USED", "MAX", "LAST CONNECT")
	fmt.Println("--------------------------------------------------------------------------------")

	for _, p := range providers {
		lastConnect := "-"
		if !p.LastSuccessfulConnect.IsZero() {
			lastConnect = p.LastSuccessfulConnect.Format("2006-01-02 15:04:05")
		}

		fmt.Printf("%-30s %-10s %-8d %-8d %-20s\n",
			p.Host,
			p.State.String(),
			p.UsedConnections,
			p.MaxConnections,
			lastConnect,
		)

		if p.FailureReason != "" {
			fmt.Printf("  failure: %s\n", p.FailureReason)
		}
	}
}

func printPoolMetrics(snap nntppool.PoolMetricsSnapshot) {
	fmt.Println("\n=== Pool Metrics ===")
	fmt.Printf("  Articles downloaded : %d\n", snap.ArticlesDownloaded)
	fmt.Printf("  Articles posted     : %d\n", snap.ArticlesPosted)
	fmt.Printf("  Bytes downloaded    : %s\n", formatBytes(snap.BytesDownloaded))
	fmt.Printf("  Bytes uploaded      : %s\n", formatBytes(snap.BytesUploaded))
	fmt.Printf("  Total errors        : %d\n", snap.TotalErrors)

	if len(snap.ProviderMetrics) > 0 {
		fmt.Println("\n=== Per-Provider Metrics ===")
		fmt.Printf("%-30s %-8s %-8s %-14s %-14s %-8s\n",
			"HOST", "STATE", "CONN", "DOWNLOADED", "BYTES DL", "ERRORS")
		fmt.Println("--------------------------------------------------------------------------------")

		for _, pm := range snap.ProviderMetrics {
			fmt.Printf("%-30s %-8s %d/%-6d %-14d %-14s %-8d\n",
				pm.Host,
				pm.State,
				pm.ActiveConnections,
				pm.MaxConnections,
				pm.ArticlesDownloaded,
				formatBytes(pm.BytesDownloaded),
				pm.TotalErrors,
			)
		}
	}
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}

	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func init() {
	statsCmd.Flags().StringVarP(&configFile, "config", "c", "", "Path to YAML config file (required)")
	_ = statsCmd.MarkFlagRequired("config")

	rootCmd.AddCommand(statsCmd)
}
