package processor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"

	"github.com/Tensai75/nzbparser"
	"github.com/javi11/nntppool"
	"github.com/k0kubun/go-ansi"
	"github.com/schollz/progressbar/v3"
	"github.com/sourcegraph/conc/pool"
)

// SegmentError represents a download error for a specific segment
type SegmentError struct {
	SegmentID string
	Err       error
}

func (e *SegmentError) Error() string {
	return fmt.Sprintf("error downloading segment %s: %v", e.SegmentID, e.Err)
}

// Processor handles the downloading of NZB files
type Processor struct {
	nntpClient  nntppool.UsenetConnectionPool
	concurrency int
}

// New creates a new processor with the specified configuration
func New(nntpClient nntppool.UsenetConnectionPool, totalSegments int, concurrency int) *Processor {
	if concurrency <= 0 {
		concurrency = 10
	}

	return &Processor{
		nntpClient:  nntpClient,
		concurrency: concurrency,
	}
}

// ProcessNZB downloads all articles in the NZB file
func (p *Processor) ProcessNZB(ctx context.Context, nzb *nzbparser.Nzb, checkPercent int) (err error) {
	// Create a new worker pool with the configured concurrency
	workerPool := pool.New().WithMaxGoroutines(p.concurrency).WithContext(ctx).WithCancelOnError()
	defer func() {
		err = workerPool.Wait()
	}()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Process each file
	for _, file := range nzb.Files {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		slog.InfoContext(ctx, fmt.Sprintf("Checking file %s", file.Filename))

		// Determine which segments to check based on checkPercent
		totalSegments := len(file.Segments)
		segmentsToCheck := totalSegments
		if checkPercent < 100 {
			segmentsToCheck = (totalSegments * checkPercent) / 100
			if segmentsToCheck == 0 {
				segmentsToCheck = 1 // Always check at least one segment
			}
		}

		// Select random segment indices without duplicates
		selectedIndices := make(map[int]bool)
		if segmentsToCheck < totalSegments {
			// Generate random indices without duplicates
			for len(selectedIndices) < segmentsToCheck {
				idx := rand.Intn(totalSegments)
				selectedIndices[idx] = true
			}
		} else {
			// Check all segments
			for i := 0; i < totalSegments; i++ {
				selectedIndices[i] = true
			}
		}

		slog.InfoContext(ctx, fmt.Sprintf("Checking %d of %d segments (%d%%)", segmentsToCheck, totalSegments, checkPercent))

		bar := progressbar.NewOptions(int(file.Bytes),
			progressbar.OptionSetWriter(ansi.NewAnsiStdout()), //you should install "github.com/k0kubun/go-ansi"
			progressbar.OptionEnableColorCodes(true),
			progressbar.OptionSetWidth(15),
			progressbar.OptionShowBytes(true),
			progressbar.OptionShowTotalBytes(true),
			progressbar.OptionSetTheme(progressbar.Theme{
				Saucer:        "[green]=[reset]",
				SaucerHead:    "[green]>[reset]",
				SaucerPadding: " ",
				BarStart:      "[",
				BarEnd:        "]",
			}))

		// Process each segment
		for segIdx, segment := range file.Segments {
			// Skip segments that are not selected
			if !selectedIndices[segIdx] {
				continue
			}
			// Create local variables to avoid closure problems
			fileInfo := file
			seg := segment

			// Submit task to worker pool
			workerPool.Go(func(ctx context.Context) error {
				// Process segment
				bytesDownloaded, err := p.nntpClient.Body(ctx, seg.Id, io.Discard, fileInfo.Groups)
				if err != nil {
					if errors.Is(err, context.Canceled) {
						return nil
					}

					// Log the error
					slog.ErrorContext(ctx, "Error downloading segment",
						"segment", seg.Id,
						"file", fileInfo.Filename,
						"error", err)

					cancel()

					// Store the first error we encounter
					return &SegmentError{
						SegmentID: seg.Id,
						Err:       err,
					}
				}

				// Update statistics
				_ = bar.Add(int(bytesDownloaded))
				return nil
			})
		}

		slog.InfoContext(ctx, fmt.Sprintf("File %s checked", file.Filename))
		_ = bar.Finish()
	}

	// Return the first error that occurred, if any
	return nil
}
