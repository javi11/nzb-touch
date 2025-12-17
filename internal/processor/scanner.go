package processor

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/javi11/nzb-touch/internal/nzb"
	"github.com/opencontainers/selinux/pkg/pwalkdir"
)

// DirectoryScanner handles scanning directories for NZB files
type DirectoryScanner struct {
	queue             *Queue
	processor         *Processor
	watchDirs         []string
	interval          time.Duration
	maxFilesPerDay    int
	reprocessInterval time.Duration
	failedDirectory   string
	checkPercent      int
	missingPercent    int
	processingQueue   chan string
	stopChan          chan struct{}
}

// NewDirectoryScanner creates a new directory scanner
func NewDirectoryScanner(
	processor *Processor,
	watchDirs []string,
	interval time.Duration,
	maxFilesPerDay int,
	concurrentProcessing int,
	dbPath string,
	reprocessInterval time.Duration,
	failedDirectory string,
	checkPercent int,
	missingPercent int,
) (*DirectoryScanner, error) {
	if concurrentProcessing <= 0 {
		concurrentProcessing = 1
	}

	// Create queue with SQLite persistence
	queue, err := NewQueue(dbPath)
	if err != nil {
		return nil, err
	}

	return &DirectoryScanner{
		queue:             queue,
		processor:         processor,
		watchDirs:         watchDirs,
		interval:          interval,
		maxFilesPerDay:    maxFilesPerDay,
		reprocessInterval: reprocessInterval,
		failedDirectory:   failedDirectory,
		checkPercent:      checkPercent,
		missingPercent:    missingPercent,
		processingQueue:   make(chan string, concurrentProcessing),
		stopChan:          make(chan struct{}),
	}, nil
}

// Start begins scanning directories at the configured interval
func (s *DirectoryScanner) Start(ctx context.Context) error {
	// Start processor workers
	for i := 0; i < cap(s.processingQueue); i++ {
		go s.processFiles(ctx)
	}

	// Run initial scan
	s.scanDirectories(ctx)

	// Setup ticker for periodic scans
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.scanDirectories(ctx)
		case <-s.stopChan:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Stop stops the scanner and closes the database connection
func (s *DirectoryScanner) Stop() {
	close(s.stopChan)
	if s.queue != nil {
		_ = s.queue.Close()
	}
}

// scanDirectories scans each watched directory for NZB files
func (s *DirectoryScanner) scanDirectories(ctx context.Context) {
	slog.InfoContext(ctx, "Starting directory scan")

	// Scan watched directories for new files
	for _, dir := range s.watchDirs {
		err := pwalkdir.Walk(dir, func(path string, info fs.DirEntry, err error) error {
			// Check for errors or context cancellation
			if err != nil {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}

			// Skip directories
			if info.IsDir() {
				return nil
			}

			// Check if file is an NZB
			if !strings.EqualFold(filepath.Ext(path), ".nzb") {
				return nil
			}

			// Check if file is already in queue
			if s.queue.Contains(path) {
				return nil
			}

			// Add file to queue
			if s.queue.Add(path) {
				slog.InfoContext(ctx, "Found new NZB file", "path", path)

				// Check if we're under the daily limit
				if s.queue.GetProcessedToday() < s.maxFilesPerDay {
					// Send to processing queue
					select {
					case s.processingQueue <- path:
						slog.InfoContext(ctx, "Queued file for processing", "path", path)
					default:
						slog.InfoContext(ctx, "Processing queue is full, file will be processed later", "path", path)
					}
				} else {
					slog.InfoContext(ctx, "Daily processing limit reached, file will be processed tomorrow", "path", path)
				}
			}

			return nil
		})

		if err != nil {
			slog.ErrorContext(ctx, "Error scanning directory", "dir", dir, "error", err)
		}
	}

	// Check for items that need reprocessing
	if s.reprocessInterval > 0 {
		s.checkForReprocessItems(ctx)
	}

	// Clean up old processed items (keep for 30 days)
	pruned := s.queue.PruneOldItems(30 * 24 * time.Hour)
	if pruned > 0 {
		slog.InfoContext(ctx, "Pruned old items from queue", "count", pruned)
	}

	slog.InfoContext(ctx, "Directory scan completed")
}

// checkForReprocessItems checks for items that need to be reprocessed
func (s *DirectoryScanner) checkForReprocessItems(ctx context.Context) {
	// Get items that are due for reprocessing
	itemsToReprocess := s.queue.GetItemsDueForReprocessing(s.reprocessInterval)

	if len(itemsToReprocess) == 0 {
		return
	}

	slog.InfoContext(ctx, "Found items to reprocess", "count", len(itemsToReprocess))

	// Check daily limit
	availableSlots := s.maxFilesPerDay - s.queue.GetProcessedToday()
	if availableSlots <= 0 {
		slog.InfoContext(ctx, "Daily processing limit reached, items will be reprocessed tomorrow")
		return
	}

	// Limit the number of items to reprocess based on available slots
	if len(itemsToReprocess) > availableSlots {
		itemsToReprocess = itemsToReprocess[:availableSlots]
	}

	// Queue items for reprocessing
	for _, item := range itemsToReprocess {
		// Check that the file still exists
		if _, err := os.Stat(item.FilePath); os.IsNotExist(err) {
			slog.InfoContext(ctx, "File no longer exists, skipping reprocessing", "path", item.FilePath)
			continue
		}

		slog.InfoContext(ctx, "Queuing item for reprocessing",
			"path", item.FilePath,
			"last_processed", item.ProcessedAt,
			"process_count", item.ProcessCount)

		// Send to processing queue
		select {
		case s.processingQueue <- item.FilePath:
			// Successfully queued
		default:
			// Queue is full, stop adding more
			slog.InfoContext(ctx, "Processing queue is full, remaining items will be reprocessed later")
			return
		}
	}

	slog.InfoContext(ctx, "All items queued for reprocessing")
}

// processFiles is a worker that processes files from the queue
func (s *DirectoryScanner) processFiles(ctx context.Context) {
	for {
		select {
		case filePath := <-s.processingQueue:
			// Skip if we've hit the daily limit
			if s.queue.GetProcessedToday() >= s.maxFilesPerDay {
				slog.InfoContext(ctx, "Daily processing limit reached, skipping file", "path", filePath)
				continue
			}

			// Process the file
			err := s.processFile(ctx, filePath)
			if err != nil {
				slog.ErrorContext(ctx, "Error processing file", "path", filePath, "error", err)

				// Move the failed file to the failed directory if configured
				if s.failedDirectory != "" {
					if moveErr := s.moveToFailedDirectory(filePath); moveErr != nil {
						slog.ErrorContext(ctx, "Failed to move file to failed directory",
							"path", filePath,
							"target_dir", s.failedDirectory,
							"error", moveErr)
					}
				}
			}

			// Mark as processed regardless of success
			// This prevents retrying files that cause errors
			s.queue.MarkProcessed(filePath)

		case <-s.stopChan:
			return
		case <-ctx.Done():
			return
		}
	}
}

// moveToFailedDirectory moves a failed NZB file to the configured failed directory
// preserving the original directory structure
func (s *DirectoryScanner) moveToFailedDirectory(filePath string) error {
	// If failed directory not configured, just return
	if s.failedDirectory == "" {
		return nil
	}

	// Create the failed directory if it doesn't exist
	if err := os.MkdirAll(s.failedDirectory, 0755); err != nil {
		return err
	}

	// Find the base watch directory containing this file
	var basePath string
	for _, watchDir := range s.watchDirs {
		absWatchDir, err := filepath.Abs(watchDir)
		if err != nil {
			continue
		}

		absFilePath, err := filepath.Abs(filePath)
		if err != nil {
			continue
		}

		if strings.HasPrefix(absFilePath, absWatchDir) {
			basePath = absWatchDir
			break
		}
	}

	// If we couldn't find a matching watch directory, just use the file name
	var targetPath string
	if basePath != "" {
		// Get the relative path from the watch directory
		absFilePath, _ := filepath.Abs(filePath)
		relPath, err := filepath.Rel(basePath, absFilePath)
		if err != nil {
			// Fall back to just the file name if we can't get the relative path
			relPath = filepath.Base(filePath)
		}

		// Create the target path with the relative structure
		targetPath = filepath.Join(s.failedDirectory, relPath)
	} else {
		// Just use the file name
		targetPath = filepath.Join(s.failedDirectory, filepath.Base(filePath))
	}

	// Create parent directories if needed
	targetDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return err
	}

	// Move the file
	if err := os.Rename(filePath, targetPath); err != nil {
		// If rename fails (e.g., across different filesystems), try copy and delete
		if err := copyFile(filePath, targetPath); err != nil {
			return err
		}

		// Delete original after successful copy
		return os.Remove(filePath)
	}

	slog.Info("Moved failed NZB file", "from", filePath, "to", targetPath)
	return nil
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = destFile.ReadFrom(sourceFile)
	return err
}

// processFile processes a single NZB file
func (s *DirectoryScanner) processFile(ctx context.Context, filePath string) error {
	slog.InfoContext(ctx, "Processing NZB file", "path", filePath)

	// Load and parse NZB file
	nzbData, err := nzb.LoadFromFile(filePath)
	if err != nil {
		return err
	}

	// Display NZB information
	nzbData.PrintInfo()

	// Process the NZB file
	return s.processor.ProcessNZB(ctx, nzbData.Nzb, s.checkPercent, s.missingPercent)
}
