package blob

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
)

// ChartBlob wraps a tar archive ReadOnlyBlob returned by the Helm ResourceRepository
// and provides structured access to the chart (.tgz) and optional provenance (.prov) files
// contained within it.
type ChartBlob struct {
	blob.ReadOnlyBlob
	extract sync.Once
	err     error

	chartBlob blob.ReadOnlyBlob
	provBlob  blob.ReadOnlyBlob
}

// NewChartBlob creates a new ChartBlob from a tar archive blob.
func NewChartBlob(tarBlob blob.ReadOnlyBlob) *ChartBlob {
	return &ChartBlob{
		ReadOnlyBlob: tarBlob,
	}
}

// ChartArchive returns the chart .tgz blob extracted from the tar archive.
func (c *ChartBlob) ChartArchive() (blob.ReadOnlyBlob, error) {
	if err := c.ensureExtracted(); err != nil {
		return nil, err
	}
	return c.chartBlob, nil
}

// ProvFile returns the provenance .prov blob if present in the tar archive.
// Returns nil if no provenance file was found.
func (c *ChartBlob) ProvFile() (blob.ReadOnlyBlob, error) {
	if err := c.ensureExtracted(); err != nil {
		return nil, err
	}
	return c.provBlob, nil
}

func (c *ChartBlob) ensureExtracted() error {
	c.extract.Do(func() {
		c.chartBlob, c.provBlob, c.err = extractFromTar(c.ReadOnlyBlob)
	})
	return c.err
}

// extractFromTar reads a tar archive blob and extracts the chart tgz and optional prov file.
func extractFromTar(tarBlob blob.ReadOnlyBlob) (chartBlob blob.ReadOnlyBlob, provBlob blob.ReadOnlyBlob, err error) {
	rc, err := tarBlob.ReadCloser()
	if err != nil {
		return nil, nil, fmt.Errorf("error opening tar blob: %w", err)
	}
	defer func() { _ = rc.Close() }()

	tr := tar.NewReader(rc)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("error reading tar entry: %w", err)
		}

		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, nil, fmt.Errorf("error reading tar entry %s: %w", header.Name, err)
		}

		switch {
		case strings.HasSuffix(header.Name, ".tgz"):
			chartBlob = inmemory.New(
				newByteReader(data),
				inmemory.WithMediaType("application/gzip"),
				inmemory.WithSize(int64(len(data))),
			)
		case strings.HasSuffix(header.Name, ".prov"):
			provBlob = inmemory.New(
				newByteReader(data),
				inmemory.WithMediaType("application/octet-stream"),
				inmemory.WithSize(int64(len(data))),
			)
		}
	}

	if chartBlob == nil {
		return nil, nil, fmt.Errorf("no chart (.tgz) found in tar archive")
	}

	return chartBlob, provBlob, nil
}

// byteReader wraps a byte slice as an io.Reader for inmemory.New.
type byteReader struct {
	data   []byte
	offset int
}

func newByteReader(data []byte) *byteReader {
	return &byteReader{data: data}
}

func (r *byteReader) Read(p []byte) (n int, err error) {
	if r.offset >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.offset:])
	r.offset += n
	return n, nil
}
