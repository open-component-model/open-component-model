package tar

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/errdef"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
)

const (
	// maxLayoutManifestSize bounds the manifests, indexes and layout indexes a
	// LayoutVerifier holds in memory.
	maxLayoutManifestSize int64 = 4 << 20
	// maxLayoutRetainedSize bounds the bytes of all manifest-shaped blobs a
	// LayoutVerifier holds in memory at once, which a layout could otherwise
	// inflate with any number of such blobs.
	maxLayoutRetainedSize = 16 * maxLayoutManifestSize
	// annotationMainArtifact is set by legacy artifact sets on their index to the
	// digest of the artifact they hold.
	annotationMainArtifact = "software.ocm/main"
)

// LayoutVerifier verifies an OCI layout or a legacy artifact set (see
// [ctf.IsArtifactSetMediaType]) while it is read: that the artifact it holds has the
// expected root digest, and that every blob of that artifact's graph is present and
// matches its digest. Only manifests and indexes are held in memory, layers never are.
//
// It implements the ArtifactVerifier of package repository/integrity.
type LayoutVerifier struct{}

// Accepts reports whether mediaType is that of an OCI layout or a legacy artifact set.
func (LayoutVerifier) Accepts(mediaType string) bool {
	return layout.IsOCILayoutMediaType(mediaType) || ctf.IsArtifactSetMediaType(mediaType)
}

// Verify returns rc wrapped so that it serves the archive unchanged and fails, once
// the archive is read to its end, if it does not hold the artifact root. Closing the
// reader before its end reads the rest of the archive to complete the verification.
// size is the byte size of the archive or [blob.SizeUnknown]; when known, the read
// that reaches it already reports the verification result, so consumers reading
// exactly size bytes see a failure before they close.
func (LayoutVerifier) Verify(rc io.ReadCloser, root digest.Digest, size int64) io.ReadCloser {
	return newLayoutVerifier(rc, root, size)
}

func newLayoutVerifier(rc io.ReadCloser, root digest.Digest, size int64) *layoutVerifier {
	pr, pw := io.Pipe()
	v := &layoutVerifier{base: rc, pw: pw, size: size, result: make(chan error, 1)}
	go func() {
		err := inspectLayout(pr, root)
		_ = pr.CloseWithError(err)
		v.result <- err
	}()
	return v
}

// layoutVerifier tees everything read from base to an inspector reading the
// archive from the other end of a pipe.
type layoutVerifier struct {
	base   io.ReadCloser
	pw     *io.PipeWriter
	result chan error
	// size is the expected archive size, or negative if unknown.
	size int64
	// read counts the bytes read from base.
	read int64

	// err is sticky: the first failure of reading or verifying.
	err error
	// readErr is the error base failed with, if any.
	readErr error
	// eof is set once base is read to its end and the archive verified.
	eof bool
	// collected is set once the inspector reported its result.
	collected bool
}

func (v *layoutVerifier) Read(p []byte) (int, error) {
	if v.err != nil {
		return 0, v.err
	}
	if v.eof {
		return 0, io.EOF
	}

	n, err := v.base.Read(p)
	v.read += int64(n)
	if v.size >= 0 && v.read > v.size {
		v.err = fmt.Errorf("layout exceeds its size of %d bytes", v.size)
		_ = v.pw.CloseWithError(v.err)
		_ = v.collect()
		return n, v.err
	}
	if n > 0 {
		if _, writeErr := v.pw.Write(p[:n]); writeErr != nil {
			// the inspector gave up on the archive, it reports why.
			v.err = v.collect()
			if v.err == nil {
				v.err = writeErr
			}
			return n, v.err
		}
	}
	if err == nil && v.size >= 0 && v.read == v.size {
		// The archive is complete at size; base is not read further but closed in Close.
		_ = v.pw.Close()
		if v.err = v.collect(); v.err != nil {
			return n, v.err
		}
		v.eof = true
		return n, nil
	}

	switch {
	case errors.Is(err, io.EOF):
		_ = v.pw.Close()
		if v.err = v.collect(); v.err != nil {
			return n, v.err
		}
		v.eof = true
		return n, io.EOF
	case err != nil:
		_ = v.pw.CloseWithError(err)
		_ = v.collect()
		v.readErr, v.err = err, err
		return n, err
	}
	return n, nil
}

// Close completes the verification of an archive that was not read to its end (e.g.
// by a tar reader that stops at the end marker) and closes base.
func (v *layoutVerifier) Close() error {
	if v.err == nil && !v.eof {
		_, _ = blob.CopyReader(io.Discard, v)
		if v.readErr != nil {
			v.err = fmt.Errorf("layout verification incomplete: %w", v.readErr)
		}
	}
	if !v.collected {
		_ = v.pw.CloseWithError(errors.New("layout reader closed"))
		_ = v.collect()
	}
	return errors.Join(v.err, v.base.Close())
}

func (v *layoutVerifier) collect() error {
	if v.collected {
		return nil
	}
	v.collected = true
	return <-v.result
}

// inspectLayout reads an OCI layout or legacy artifact set tar, optionally gzipped,
// from r to its end and verifies that it holds the complete artifact root.
func inspectLayout(r io.Reader, root digest.Digest) error {
	buffered := bufio.NewReader(r)
	var archive io.Reader = buffered
	if magic, _ := buffered.Peek(2); len(magic) == 2 && magic[0] == 0x1F && magic[1] == 0x8B {
		gz, err := gzip.NewReader(buffered)
		if err != nil {
			return fmt.Errorf("failed to read gzip header of layout: %w", err)
		}
		defer gz.Close()
		archive = gz
	}

	ins := &layoutInspection{
		names:    make(map[string]struct{}),
		indexes:  make(map[string]*ociImageSpecV1.Index, 1),
		seen:     make(map[digest.Digest]int64),
		retained: make(retainedFetcher),
	}
	tr := tar.NewReader(archive)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read layout: %w", err)
		}
		if err := ins.entry(hdr, tr); err != nil {
			return err
		}
	}
	// What follows the end marker (padding, the gzip trailer) is read as well, so
	// that a corrupt gzip checksum surfaces and the whole archive passes through.
	if _, err := blob.CopyReader(io.Discard, archive); err != nil {
		return fmt.Errorf("failed to read layout: %w", err)
	}
	if _, err := blob.CopyReader(io.Discard, buffered); err != nil {
		return fmt.Errorf("failed to read layout: %w", err)
	}

	return ins.verify(context.Background(), root)
}

type layoutInspection struct {
	// names holds the entries read, to refuse ambiguous archives.
	names map[string]struct{}
	// indexes holds index.json and artifact-descriptor.json.
	indexes map[string]*ociImageSpecV1.Index
	// seen maps the digests of all blobs read to their size.
	seen map[digest.Digest]int64
	// retained holds the blobs that may be manifests or indexes.
	retained retainedFetcher
	// retainedSize is the total size of retained.
	retainedSize int64
	buf          bytes.Buffer
}

func (ins *layoutInspection) entry(hdr *tar.Header, r io.Reader) error {
	if hdr.Typeflag == tar.TypeDir {
		return nil
	}
	// Names are resolved like go-tarfs (used by ReadOCILayout) resolves them, so that
	// aliases such as .//index.json, which it serves in place of an earlier entry,
	// count as repeated entries. Rooting the name first also maps absolute and
	// escaping names onto the layout root instead of letting them pass unchecked.
	name := strings.TrimPrefix(path.Clean("/"+hdr.Name), "/")
	isIndex := name == ociImageSpecV1.ImageIndexFile || name == ctf.ArtifactSetDescriptorFileName
	blobName, isBlob := strings.CutPrefix(name, ctf.BlobsDirectoryName+"/")
	if !isIndex && !isBlob {
		return nil
	}
	if hdr.Typeflag != tar.TypeReg {
		return fmt.Errorf("layout entry %s is not a regular file", hdr.Name)
	}
	if _, repeated := ins.names[name]; repeated {
		return fmt.Errorf("layout contains entry %s more than once", hdr.Name)
	}
	ins.names[name] = struct{}{}

	if isIndex {
		if hdr.Size > maxLayoutManifestSize {
			return fmt.Errorf("layout index %s exceeds %d bytes", name, maxLayoutManifestSize)
		}
		var idx ociImageSpecV1.Index
		if err := json.NewDecoder(io.LimitReader(r, maxLayoutManifestSize)).Decode(&idx); err != nil {
			return fmt.Errorf("failed to decode layout index %s: %w", name, err)
		}
		ins.indexes[name] = &idx
		return nil
	}
	return ins.blob(hdr, blobName, r)
}

// blob verifies a blob stored as blobs/<algorithm>/<encoded> (OCI layout) or
// blobs/<algorithm>.<encoded> (legacy artifact set) against the digest its name gives.
func (ins *layoutInspection) blob(hdr *tar.Header, blobName string, r io.Reader) error {
	var dig digest.Digest
	if algorithm, encoded, nested := strings.Cut(blobName, "/"); nested {
		dig = digest.NewDigestFromEncoded(digest.Algorithm(algorithm), encoded)
	} else {
		dig = digest.Digest(ctf.ToDigest(blobName))
	}
	if err := dig.Validate(); err != nil {
		if errors.Is(err, digest.ErrDigestUnsupported) {
			return fmt.Errorf("layout entry %s uses unsupported digest algorithm %s", hdr.Name, dig.Algorithm())
		}
		return fmt.Errorf("layout entry %s does not name a valid digest: %w", hdr.Name, err)
	}

	digester := dig.Algorithm().Digester()
	small := hdr.Size <= maxLayoutManifestSize
	if small {
		ins.buf.Reset()
		ins.buf.Grow(int(hdr.Size))
		if _, err := ins.buf.ReadFrom(r); err != nil {
			return fmt.Errorf("failed to read layout entry %s: %w", hdr.Name, err)
		}
		// hash.Hash.Write never returns an error.
		_, _ = digester.Hash().Write(ins.buf.Bytes())
	} else if _, err := blob.CopyReader(digester.Hash(), r); err != nil {
		return fmt.Errorf("failed to read layout entry %s: %w", hdr.Name, err)
	}
	if got := digester.Digest(); got != dig {
		return fmt.Errorf("layout entry %s does not match its digest: got %s", hdr.Name, got)
	}

	ins.seen[dig] = hdr.Size
	if small && isManifest(ins.buf.Bytes()) {
		ins.retainedSize += hdr.Size
		if ins.retainedSize > maxLayoutRetainedSize {
			return fmt.Errorf("layout manifests and indexes exceed %d bytes", maxLayoutRetainedSize)
		}
		ins.retained[dig] = bytes.Clone(ins.buf.Bytes())
	}
	return nil
}

// verify checks, once the whole archive is read, that its main artifact is root and
// that every blob of root's graph was read with the size the graph references.
func (ins *layoutInspection) verify(ctx context.Context, root digest.Digest) error {
	idx, ok := ins.indexes[ociImageSpecV1.ImageIndexFile]
	if !ok {
		if idx, ok = ins.indexes[ctf.ArtifactSetDescriptorFileName]; !ok {
			return fmt.Errorf("layout has no %s or %s", ociImageSpecV1.ImageIndexFile, ctf.ArtifactSetDescriptorFileName)
		}
	}
	for _, manifest := range idx.Manifests {
		if manifest.MediaType == "" || manifest.Size <= 0 {
			return fmt.Errorf("layout index entry %s has no media type or size", manifest.Digest)
		}
	}

	top, err := selectRoot(ctx, ins.retained, idx.Manifests)
	if err != nil {
		return err
	}
	if top.Digest != root {
		return fmt.Errorf("layout main artifact is %s, expected %s", top.Digest, root)
	}
	// Consumers may pick the main artifact through TopLevelArtifacts rather than
	// selectRoot; both must agree, or a consumer could unpack an unverified artifact.
	for _, manifest := range idx.Manifests {
		if isManifestMediaType(manifest.MediaType) {
			if _, ok := ins.retained[manifest.Digest]; !ok {
				return fmt.Errorf("layout manifest %s listed in the index cannot be inspected", manifest.Digest)
			}
		}
	}
	if top := TopLevelArtifacts(ctx, ins.retained, idx.Manifests); len(top) == 1 && top[0].Digest != root {
		return fmt.Errorf("layout main artifact is %s, expected %s", top[0].Digest, root)
	}
	if main := idx.Annotations[annotationMainArtifact]; main != "" && main != root.String() {
		return fmt.Errorf("layout annotation %s names %s, expected %s", annotationMainArtifact, main, root)
	}

	visited := make(map[digest.Digest]struct{}, len(ins.seen))
	pending := []ociImageSpecV1.Descriptor{top}
	for len(pending) > 0 {
		node := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if _, done := visited[node.Digest]; done {
			continue
		}
		visited[node.Digest] = struct{}{}

		size, ok := ins.seen[node.Digest]
		if !ok {
			return fmt.Errorf("layout blob %s referenced by the artifact graph is missing", node.Digest)
		}
		if size != node.Size {
			return fmt.Errorf("layout blob %s has size %d, but the artifact graph references %d bytes", node.Digest, size, node.Size)
		}
		children, err := content.Successors(ctx, ins.retained, node)
		if err != nil {
			return fmt.Errorf("layout manifest %s cannot be read: %w", node.Digest, err)
		}
		pending = append(pending, children...)
	}
	return nil
}

// isManifest reports whether data may be an OCI or Docker manifest or index, all of
// which declare schema version 2.
func isManifest(data []byte) bool {
	var versioned specs.Versioned
	return json.Unmarshal(data, &versioned) == nil && versioned.SchemaVersion == 2
}

func isManifestMediaType(mediaType string) bool {
	switch mediaType {
	case ociImageSpecV1.MediaTypeImageManifest, ociImageSpecV1.MediaTypeImageIndex,
		mediaTypeDockerManifest, mediaTypeDockerManifestList:
		return true
	}
	return false
}

// retainedFetcher serves the manifests and indexes retained from a layout.
type retainedFetcher map[digest.Digest][]byte

func (f retainedFetcher) Fetch(_ context.Context, target ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	data, ok := f[target.Digest]
	if !ok {
		return nil, fmt.Errorf("%s: %w", target.Digest, errdef.ErrNotFound)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}
