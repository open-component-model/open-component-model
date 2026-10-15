package tar

import (
	stdtar "archive/tar"
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"

	"ocm.software/open-component-model/bindings/go/blob"
)

// ocmMainArtifactAnnotation is the index-level annotation OCM v1 artifact sets use
// to name the main artifact's digest. It is distinct from the modern per-descriptor
// software.ocm.layout.root marker.
const ocmMainArtifactAnnotation = "software.ocm/main"

// ArtifactSetRoots returns the root artifact digests an OCI layout archive declares
// in its index.json: the OCM main-artifact annotation if present, plus every
// top-level manifest. It reads only index.json, never the blobs, so it stays cheap
// regardless of artifact size and tolerates the non-standard blob paths legacy v1
// artifact sets use.
func ArtifactSetRoots(b blob.ReadOnlyBlob) ([]digest.Digest, error) {
	rc, err := b.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("open artifact set: %w", err)
	}
	defer func() { _ = rc.Close() }()

	r, err := gunzipIfNeeded(rc)
	if err != nil {
		return nil, err
	}

	tr := stdtar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("archive declares no %s", ociImageSpecV1.ImageIndexFile)
		}
		if err != nil {
			return nil, fmt.Errorf("read artifact set: %w", err)
		}
		if path.Clean(hdr.Name) != ociImageSpecV1.ImageIndexFile {
			continue
		}
		var idx ociImageSpecV1.Index
		if err := json.NewDecoder(tr).Decode(&idx); err != nil {
			return nil, fmt.Errorf("decode %s: %w", ociImageSpecV1.ImageIndexFile, err)
		}
		return indexRoots(idx), nil
	}
}

func indexRoots(idx ociImageSpecV1.Index) []digest.Digest {
	roots := make([]digest.Digest, 0, len(idx.Manifests)+1)
	if main := idx.Annotations[ocmMainArtifactAnnotation]; main != "" {
		if d, err := digest.Parse(main); err == nil {
			roots = append(roots, d)
		}
	}
	for _, m := range idx.Manifests {
		roots = append(roots, m.Digest)
	}
	return roots
}

// gunzipIfNeeded wraps r in a gzip reader when it starts with the gzip magic bytes.
func gunzipIfNeeded(r io.Reader) (io.Reader, error) {
	br := bufio.NewReader(r)
	magic, err := br.Peek(2)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("peek gzip magic: %w", err)
	}
	if len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		gz, err := gzip.NewReader(br)
		if err != nil {
			return nil, fmt.Errorf("open gzip: %w", err)
		}
		return gz, nil
	}
	return br, nil
}
