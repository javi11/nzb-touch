package nzb

import (
	"fmt"
	"os"

	"github.com/Tensai75/nzbparser"
)

// NZB represents a parsed NZB file with access to its details
type NZB struct {
	*nzbparser.Nzb
}

// LoadFromFile loads and parses an NZB file from the given file path
func LoadFromFile(nzbFilePath string) (*NZB, error) {
	file, err := os.Open(nzbFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open NZB file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	nzb, err := nzbparser.Parse(file)
	if err != nil {
		return nil, fmt.Errorf("failed to parse NZB file: %w", err)
	}

	// Scan for additional information
	nzbparser.ScanNzbFile(nzb)
	nzbparser.MakeUnique(nzb)

	return &NZB{Nzb: nzb}, nil
}

// PrintInfo prints information about the NZB file
func (n *NZB) PrintInfo() {
	fmt.Printf("NZB Info: %d files, %d segments, total size: %d bytes\n",
		n.TotalFiles, n.TotalSegments, n.Bytes)
}

// ForEachSegment executes the provided function for each segment in the NZB
func (n *NZB) ForEachSegment(fn func(nzbparser.NzbFile, nzbparser.NzbSegment) error) error {
	for _, file := range n.Files {
		for _, segment := range file.Segments {
			if err := fn(file, segment); err != nil {
				return err
			}
		}
	}
	return nil
}
