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

// ocmArtifactDescriptorFile is the descriptor name of the OCM-native artifact set
// format, as written by the legacy OCM v1 library (its artifactset
// ArtifactSetDescriptorFileName constant). The OCI format uses index.json instead
// (the default); both files are OCI image indexes.
//
// This follows what v1 actually writes, not the spec: the OCM spec names this file
// "artifact-set-descriptor.json", a name no implementation writes.
const ocmArtifactDescriptorFile = "artifact-descriptor.json"

// ArtifactSetRoots returns the root artifact digests an OCI layout archive declares
// in its descriptor (index.json for the OCI format, artifact-descriptor.json for the
// OCM format): the OCM main-artifact annotation if present, plus every top-level
// manifest. It reads only the descriptor, never the blobs, so it stays cheap
// regardless of artifact size and tolerates the non-standard blob paths legacy v1
// artifact sets use.
//
// This is a declaration check, not a content check: the returned digests are what the
// descriptor declares, not verified against the blobs the archive actually carries.
// Callers use it to confirm a recorded (signed) digest is one the archive declares;
// integrity of the referenced manifest and layer blobs is enforced by content-addressing
// when the artifact is consumed (fetch-by-digest), not here. A crafted archive that
// declares the signed root but carries mismatched blobs therefore passes this check and
// is rejected only at that later read.
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
			return nil, fmt.Errorf("archive declares no %s or %s", ociImageSpecV1.ImageIndexFile, ocmArtifactDescriptorFile)
		}
		if err != nil {
			return nil, fmt.Errorf("read artifact set: %w", err)
		}
		name := path.Clean(hdr.Name)
		if name != ociImageSpecV1.ImageIndexFile && name != ocmArtifactDescriptorFile {
			continue
		}
		var idx ociImageSpecV1.Index
		if err := json.NewDecoder(tr).Decode(&idx); err != nil {
			return nil, fmt.Errorf("decode %s: %w", name, err)
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
