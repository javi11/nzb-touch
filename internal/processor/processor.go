package processor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/Tensai75/nzbparser"
	"github.com/javi11/nntppool"
	"github.com/k0kubun/go-ansi"
	"github.com/schollz/progressbar/v3"
	"github.com/sourcegraph/conc/pool"
)

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
func (p *Processor) ProcessNZB(ctx context.Context, nzb *nzbparser.Nzb) error {
	// Create a new worker pool with the configured concurrency
	workerPool := pool.New().WithMaxGoroutines(p.concurrency).WithContext(ctx)

	// Process each file
	for _, file := range nzb.Files {
		slog.InfoContext(ctx, fmt.Sprintf("Checking file %s", file.Filename))

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
		for _, segment := range file.Segments {
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

					fmt.Printf("Error downloading segment %s: %v\n", seg.Id, err)
					return nil // We don't want to stop the entire process on segment failures
				}

				// Update statistics
				_ = bar.Add(int(bytesDownloaded))
				return nil
			})
		}

		slog.InfoContext(ctx, fmt.Sprintf("File %s checked", file.Filename))
		_ = bar.Finish()
	}

	// Wait for all downloads to complete
	err := workerPool.Wait()

	return err
}
