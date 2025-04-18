package processor

import (
	"database/sql"
	"log/slog"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// QueueItem represents an item in the processing queue
type QueueItem struct {
	FilePath     string    // Path to the NZB file
	Added        time.Time // When the item was added to the queue
	Processed    bool      // Whether the item has been processed
	ProcessedAt  time.Time // When the item was processed
	ProcessCount int       // Number of times this item has been processed
}

// Queue manages the processing queue with thread-safe operations
type Queue struct {
	db *sql.DB      // SQLite database connection
	mu sync.RWMutex // Mutex for thread-safe access
}

// NewQueue creates a new processing queue with SQLite persistence
func NewQueue(dbPath string) (*Queue, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	// Create table if it doesn't exist
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS queue (
			file_path TEXT PRIMARY KEY,
			added TIMESTAMP NOT NULL,
			processed BOOLEAN NOT NULL DEFAULT 0,
			processed_at TIMESTAMP,
			process_count INTEGER NOT NULL DEFAULT 0
		)
	`)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	// Create indexes
	_, err = db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_queue_processed_at ON queue(processed_at);
		CREATE INDEX IF NOT EXISTS idx_queue_processed ON queue(processed);
	`)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	return &Queue{
		db: db,
	}, nil
}

// Close closes the database connection
func (q *Queue) Close() error {
	return q.db.Close()
}

// Add adds a file to the queue if it doesn't exist
func (q *Queue) Add(filePath string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Check if the file already exists
	var exists bool
	err := q.db.QueryRow("SELECT EXISTS(SELECT 1 FROM queue WHERE file_path = ?)", filePath).Scan(&exists)
	if err != nil {
		slog.Error("Failed to check if file exists in queue", "error", err)
		return false
	}

	if exists {
		return false
	}

	// Add the file to the queue
	_, err = q.db.Exec(
		"INSERT INTO queue (file_path, added, processed, process_count) VALUES (?, ?, ?, ?)",
		filePath, time.Now(), false, 0,
	)
	if err != nil {
		slog.Error("Failed to add file to queue", "error", err)
		return false
	}

	return true
}

// MarkProcessed marks a file as processed
func (q *Queue) MarkProcessed(filePath string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	now := time.Now()

	// Get current process count
	var count int
	err := q.db.QueryRow("SELECT COALESCE(process_count, 0) FROM queue WHERE file_path = ?", filePath).Scan(&count)
	if err != nil {
		slog.Error("Failed to get process count", "error", err)
		return false
	}

	// Increment process count
	count++

	// Update the record
	result, err := q.db.Exec(
		"UPDATE queue SET processed = 1, processed_at = ?, process_count = ? WHERE file_path = ?",
		now, count, filePath,
	)
	if err != nil {
		slog.Error("Failed to mark file as processed", "error", err)
		return false
	}

	rows, err := result.RowsAffected()
	if err != nil {
		slog.Error("Failed to get rows affected", "error", err)
		return false
	}

	return rows > 0
}

// Contains checks if a file is in the queue
func (q *Queue) Contains(filePath string) bool {
	q.mu.RLock()
	defer q.mu.RUnlock()

	var exists bool
	err := q.db.QueryRow("SELECT EXISTS(SELECT 1 FROM queue WHERE file_path = ?)", filePath).Scan(&exists)
	if err != nil {
		slog.Error("Failed to check if file exists in queue", "error", err)
		return false
	}

	return exists
}

// GetPendingItems returns a list of items that haven't been processed
func (q *Queue) GetPendingItems() []*QueueItem {
	q.mu.RLock()
	defer q.mu.RUnlock()

	rows, err := q.db.Query("SELECT file_path, added FROM queue WHERE processed = 0")
	if err != nil {
		slog.Error("Failed to query pending items", "error", err)
		return nil
	}
	defer func() {
		_ = rows.Close()
	}()

	var pendingItems []*QueueItem
	for rows.Next() {
		item := &QueueItem{}
		err := rows.Scan(&item.FilePath, &item.Added)
		if err != nil {
			slog.Error("Failed to scan row", "error", err)
			continue
		}
		pendingItems = append(pendingItems, item)
	}

	return pendingItems
}

// GetItemsDueForReprocessing returns processed items that need to be reprocessed based on a time interval
func (q *Queue) GetItemsDueForReprocessing(reprocessInterval time.Duration) []*QueueItem {
	// If reprocessInterval is 0 or negative, don't reprocess anything
	if reprocessInterval <= 0 {
		return nil
	}

	q.mu.RLock()
	defer q.mu.RUnlock()

	// Calculate the cutoff time
	cutoffTime := time.Now().Add(-reprocessInterval)

	// Query for items that were processed before the cutoff time
	rows, err := q.db.Query(`
		SELECT file_path, added, processed_at, process_count 
		FROM queue 
		WHERE processed = 1 
		AND processed_at < ?
	`, cutoffTime)

	if err != nil {
		slog.Error("Failed to query items for reprocessing", "error", err)
		return nil
	}
	defer func() {
		_ = rows.Close()
	}()

	var reprocessItems []*QueueItem
	for rows.Next() {
		item := &QueueItem{Processed: true}
		err := rows.Scan(&item.FilePath, &item.Added, &item.ProcessedAt, &item.ProcessCount)
		if err != nil {
			slog.Error("Failed to scan row for reprocessing", "error", err)
			continue
		}
		reprocessItems = append(reprocessItems, item)
	}

	return reprocessItems
}

// GetProcessedToday returns the count of items processed today
func (q *Queue) GetProcessedToday() int {
	q.mu.RLock()
	defer q.mu.RUnlock()

	startOfDay := time.Now().Truncate(24 * time.Hour)
	endOfDay := startOfDay.Add(24 * time.Hour)

	var count int
	err := q.db.QueryRow(
		"SELECT COUNT(*) FROM queue WHERE processed = 1 AND processed_at >= ? AND processed_at < ?",
		startOfDay, endOfDay,
	).Scan(&count)

	if err != nil {
		slog.Error("Failed to count processed items today", "error", err)
		return 0
	}

	return count
}

// PruneOldItems removes items older than the specified duration
func (q *Queue) PruneOldItems(olderThan time.Duration) int {
	q.mu.Lock()
	defer q.mu.Unlock()

	cutoff := time.Now().Add(-olderThan)

	result, err := q.db.Exec(
		"DELETE FROM queue WHERE processed = 1 AND processed_at < ?",
		cutoff,
	)
	if err != nil {
		slog.Error("Failed to prune old items", "error", err)
		return 0
	}

	rows, err := result.RowsAffected()
	if err != nil {
		slog.Error("Failed to get rows affected", "error", err)
		return 0
	}

	return int(rows)
}
