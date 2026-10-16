package tar

import (
	stdtar "archive/tar"
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
)

// ArtifactSetContainsDigest reports whether the archive stores a blob whose bytes hash
// to want. In an OCI layout every blob is stored at a path derived from its own digest,
// so want alone names the file; the bytes are hashed to confirm and the filename is
// never trusted. The tar is streamed once, every other blob body is skipped, and only a
// matched entry is hashed, so memory stays bounded regardless of artifact size.
//
// This is the integrity binding for legacy v1 OCI artifact sets, whose localReference is
// the archive hash rather than the signed manifest root: a crafted archive that merely
// claims the signed root but carries no blob hashing to it is rejected here, not left for
// a later content-addressed read.
func ArtifactSetContainsDigest(b blob.ReadOnlyBlob, want digest.Digest) (bool, error) {
	if err := want.Validate(); err != nil {
		return false, fmt.Errorf("invalid digest %q: %w", want, err)
	}
	rc, err := b.ReadCloser()
	if err != nil {
		return false, fmt.Errorf("open artifact set: %w", err)
	}
	defer func() { _ = rc.Close() }()

	r, err := gunzipIfNeeded(rc)
	if err != nil {
		return false, err
	}

	algo, encoded := want.Algorithm().String(), want.Encoded()
	// OCM v1 writes blobs/<algo>.<hex>; OCI image layouts use blobs/<algo>/<hex>.
	dot := "blobs/" + algo + "." + encoded
	slash := "blobs/" + algo + "/" + encoded

	tr := stdtar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("read artifact set: %w", err)
		}
		if name := path.Clean(hdr.Name); name != dot && name != slash {
			continue
		}
		v := want.Algorithm().Digester()
		// LimitReader bounds the hash to the entry's declared size: the tar reader already
		// stops at the entry boundary, so this hashes exactly the blob and cannot run away
		// on a crafted archive.
		if _, err := io.Copy(v.Hash(), io.LimitReader(tr, hdr.Size)); err != nil {
			return false, fmt.Errorf("hash blob: %w", err)
		}
		if v.Digest() == want {
			return true, nil
		}
		// A decoy at the derived path must not shadow a real blob later in the tar.
	}
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
