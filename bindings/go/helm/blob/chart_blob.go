package blob

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/compression"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
)

const (
	TarGzSuffix = ".tar.gz"
	TGZSuffix   = ".tgz"
)

var ErrNoChartFound = errors.New("no chart found in tar archive")

// extractResult holds the lazily-extracted chart and provenance blobs.
type extractResult struct {
	chartBlob blob.ReadOnlyBlob
	provBlob  blob.ReadOnlyBlob
}

// ChartBlob wraps a tar archive ReadOnlyBlob returned by the Helm ResourceRepository
// and provides structured access to the chart (.tgz) and optional provenance (.prov) files
// contained within it.
type ChartBlob struct {
	blob.ReadOnlyBlob
	extract func() (*extractResult, error)
}

// NewChartBlob creates a new ChartBlob from a tar archive blob.
func NewChartBlob(tarBlob blob.ReadOnlyBlob) *ChartBlob {
	cb := &ChartBlob{
		ReadOnlyBlob: tarBlob,
	}
	cb.extract = sync.OnceValues(func() (*extractResult, error) {
		if tarBlob == nil {
			return nil, fmt.Errorf("tar blob is required")
		}
		chart, prov, err := extractFromTar(tarBlob)
		if err != nil {
			return nil, err
		}
		return &extractResult{chartBlob: chart, provBlob: prov}, nil
	})
	return cb
}

// ChartArchive returns the chart .tgz blob extracted from the tar archive.
func (c *ChartBlob) ChartArchive() (blob.ReadOnlyBlob, error) {
	result, err := c.extract()
	if err != nil {
		return nil, err
	}
	return result.chartBlob, nil
}

// ProvFile returns the provenance .prov blob if present in the tar archive.
// Returns nil if no provenance file was found.
func (c *ChartBlob) ProvFile() (blob.ReadOnlyBlob, error) {
	result, err := c.extract()
	if err != nil {
		return nil, err
	}
	return result.provBlob, nil
}

// extractFromTar reads a tar archive blob and extracts the chart tgz and optional prov file.
// This function will read the complete component of the chart file into memory.
func extractFromTar(tarBlob blob.ReadOnlyBlob) (chartBlob blob.ReadOnlyBlob, provBlob blob.ReadOnlyBlob, err error) {
	rc, err := tarBlob.ReadCloser()
	if err != nil {
		return nil, nil, fmt.Errorf("error opening tar blob: %w", err)
	}
	defer func() {
		if err := rc.Close(); err != nil {
			slog.Warn("error closing tar blob reader", "error", err)
		}
	}()

	tr := tar.NewReader(rc)
	for {
		hdr, err := tr.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break // End of archive
			}
			return nil, nil, fmt.Errorf("error reading tar blob: %w", err)
		}

		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, nil, fmt.Errorf("error reading chart tarball: %w", err)
		}

		switch {
		case strings.HasSuffix(hdr.Name, TarGzSuffix):
			fallthrough
		case strings.HasSuffix(hdr.Name, TGZSuffix):
			if chartBlob != nil {
				return nil, nil, fmt.Errorf("tar archive contains multiple chart entries; expected exactly one chart archive")
			}
			chartBlob = inmemory.New(
				bytes.NewReader(data),
				inmemory.WithMediaType(compression.MediaTypeGzip),
				inmemory.WithSize(int64(len(data))),
			)
		case strings.HasSuffix(hdr.Name, ".prov"):
			provBlob = inmemory.New(
				bytes.NewReader(data),
				inmemory.WithMediaType(filesystem.DefaultFileMediaType),
				inmemory.WithSize(int64(len(data))),
			)
		}
	}

	if chartBlob == nil {
		return nil, nil, ErrNoChartFound
	}

	return chartBlob, provBlob, nil
}
