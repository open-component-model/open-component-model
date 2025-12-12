package tar

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/nlepage/go-tarfs"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content/oci"

	"ocm.software/open-component-model/bindings/go/blob"
)

// CloseableReadOnlyStore wraps an oci.ReadOnlyStore and provides a Close method.
// This close method may be used to close the underlying reader.
type CloseableReadOnlyStore struct {
	*oci.ReadOnlyStore
	close func() error
	Index ociImageSpecV1.Index
}

func (s *CloseableReadOnlyStore) Close() error {
	if s.close != nil {
		return s.close()
	}
	return nil
}

// ReadOCILayout reads an OCI layout from a tarball blob.
// It detects if the blob is gzipped and decompresses it if necessary.
// It returns a CloseableReadOnlyStore that can be used to access the OCI layout.
// The caller is responsible for closing the store.
// The Read method is size-aware and will limit the reader to the size of the blob if known.
func ReadOCILayout(ctx context.Context, b blob.ReadOnlyBlob) (*CloseableReadOnlyStore, error) {
	var header [2]byte

	var closer func() error

	src, err := b.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("failed to create reader: %w", err)
	}
	closer = src.Close

	size := blob.SizeUnknown
	if srcSizeAware, ok := src.(blob.SizeAware); ok {
		if size = srcSizeAware.Size(); size < int64(len(header)) {
			return nil, fmt.Errorf("source is too small for gzip detection: %d < %d", size, cap(header))
		}
	}

	// Read the first two bytes for gzip detection
	n, err := io.ReadFull(src, header[:])
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, fmt.Errorf("failed to read data for gzip detection: %w", err)
	}

	// Reconstruct reader with the first two bytes prepended
	reader := io.MultiReader(bytes.NewReader(header[:n]), src)

	// Limit the reader to the size of the blob if it's known
	if size > blob.SizeUnknown {
		reader = io.LimitReader(reader, size)
	}

	const gzipMagic1, gzipMagic2 = 0x1F, 0x8B
	if n == 2 && header[0] == gzipMagic1 && header[1] == gzipMagic2 { //nolint:gosec // false positive
		var gzReader *gzip.Reader
		if gzReader, err = gzip.NewReader(reader); err != nil {
			return nil, fmt.Errorf("failed to initialize gzip reader: %w", err)
		}
		// Make sure to close the original source reader
		closer = func() error {
			return errors.Join(gzReader.Close(), src.Close())
		}
		reader = gzReader
	}

	tfs, err := tarfs.New(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to create tarfs: %w", err)
	}

	store, err := oci.NewFromFS(ctx, tfs)
	if err != nil {
		return nil, fmt.Errorf("failed to create OCI store: %w", err)
	}

	idxFile, err := tfs.Open(ociImageSpecV1.ImageIndexFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open index file: %w", err)
	}
	defer func() {
		_ = idxFile.Close()
	}()
	idx := ociImageSpecV1.Index{}
	if err := json.NewDecoder(idxFile).Decode(&idx); err != nil {
		return nil, fmt.Errorf("failed to decode index file: %w", err)
	}

	return &CloseableReadOnlyStore{
		ReadOnlyStore: store,
		close:         closer,
		Index:         idx,
	}, nil
}

// MainArtifacts returns the main artifacts from the OCI layout.
// It uses the Index from the CloseableReadOnlyStore to get the main artifacts.
// If the reference name is not set or cannot be assumed,
// this is an easy way to retrieve top level artifacts for reading in case the original reference is not known.
//
// For example, if an OCI Layout was downloaded from "ghcr.io/open-component-model/ocm-layout:v1.0.0",
// and the index contains multiple manifests, this function will return a single top-level artifact
// referencing the main index behind the given reference.
func (s *CloseableReadOnlyStore) MainArtifacts(ctx context.Context) []ociImageSpecV1.Descriptor {
	return TopLevelArtifacts(ctx, s.ReadOnlyStore, s.Index.Manifests)
}
