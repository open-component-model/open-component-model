package tar

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"sync"
	"sync/atomic"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/errdef"

	"ocm.software/open-component-model/bindings/go/oci/internal/introspection"
)

// NewOCILayoutWriter creates a new [oras.Target] that writes an OCI image layout in tar format.
//
// The writer operates in two phases:
//
// Write phase: callers invoke [OCILayoutWriter.Push] (possibly concurrently) to add blobs.
// Each Push atomically reserves a byte range in scratch and writes its tar entry (header + data)
// to that exclusive region via [io.WriterAt], so multiple goroutines can write simultaneously
// without any serialization.
//
// Finalize phase: [OCILayoutWriter.Close] appends index.json and oci-layout metadata to scratch,
// then seeks to the beginning and streams the complete tar to output. If scratch implements
// [io.Closer], it is closed after streaming (even on error).
//
// The caller is responsible for creating the buffer and cleaning it up if it does not implement
// [io.Closer]. For a convenience constructor that manages a temp file automatically,
// see [NewOCILayoutWriterWithTempFile].
func NewOCILayoutWriter(output io.Writer, buf OCILayoutBuffer) *OCILayoutWriter {
	return &OCILayoutWriter{
		buf:         buf,
		output:      output,
		tagResolver: newMemoryResolver(),
		written:     make(map[digest.Digest]ociImageSpecV1.Descriptor),
		index: &ociImageSpecV1.Index{
			Versioned: specs.Versioned{
				SchemaVersion: 2, // historical value
			},
			Manifests: []ociImageSpecV1.Descriptor{},
		},
	}
}

// OCILayoutBuffer is the minimal interface required by [OCILayoutWriter] for its scratch buffer.
//
// During the write phase ([OCILayoutWriter.Push]), multiple goroutines call WriteAt concurrently
// to non-overlapping byte regions. During the read phase ([OCILayoutWriter.Close]), the buffer
// is seeked to the start and read sequentially to stream the final tar to the output writer.
//
// Implementations include:
//   - [*os.File] — backed by a temp file on disk, lowest memory usage
//
// If the implementation also satisfies [io.Closer], Close is called automatically
// when [OCILayoutWriter.Close] completes (regardless of success or failure).
type OCILayoutBuffer interface {
	io.WriterAt
	io.ReadSeeker
}

// NewOCILayoutWriterWithTempFile is a convenience constructor that creates a temp file as the scratch buffer.
// The temp file is automatically removed from disk when [OCILayoutWriter.Close] is called.
// This is the best choice when memory is constrained and disk I/O is acceptable.
func NewOCILayoutWriterWithTempFile(output io.Writer, dir string) (*OCILayoutWriter, error) {
	tmpFile, err := os.CreateTemp(dir, "oci-layout-buffer-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file for OCI layout tar: %w", err)
	}

	return NewOCILayoutWriter(output, &removingCloser{File: tmpFile}), nil
}

// removingCloser wraps an [*os.File] so that [removingCloser.Close] both closes the
// file descriptor and removes the file from disk.
type removingCloser struct {
	*os.File
}

// Close closes the underlying file and removes it from disk. Both errors (if any)
// are joined and returned.
func (rc *removingCloser) Close() error {
	name := rc.Name()
	err := rc.File.Close()
	return errors.Join(err, os.Remove(name))
}

// OCILayoutWriter writes an OCI image layout as a tar archive with support for concurrent blob pushes.
//
// It implements [oras.Target] so it can be used as a destination for [oras.CopyGraph].
// See [NewOCILayoutWriter] for details on the two-phase (write/finalize) lifecycle.
type OCILayoutWriter struct {
	buf        OCILayoutBuffer // concurrent-write scratch buffer
	output     io.Writer       // final destination for the tar stream
	nextOffset atomic.Int64    // next available byte offset (lock-free reservation via Add)

	indexMu sync.RWMutex          // protects index
	index   *ociImageSpecV1.Index // OCI image index, populated by tag operations

	tagResolver *memoryResolver // maps references to descriptors

	writtenMu sync.RWMutex                                // protects written
	written   map[digest.Digest]ociImageSpecV1.Descriptor // descriptors that have been pushed

	closedMu sync.Mutex // protects closed; prevents Push after Close
	closed   bool
}

// Fetch is only implemented to satisfy the oras.Target interface.
func (s *OCILayoutWriter) Fetch(_ context.Context, _ ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	return nil, errdef.ErrUnsupported
}

// Resolve resolves a reference string to the descriptor it was tagged with.
// Returns [errdef.ErrNotFound] if the reference has not been tagged.
func (s *OCILayoutWriter) Resolve(ctx context.Context, reference string) (ociImageSpecV1.Descriptor, error) {
	return s.tagResolver.Resolve(ctx, reference)
}

func (s *OCILayoutWriter) Close() (closeErr error) {
	s.closedMu.Lock()
	if s.closed {
		s.closedMu.Unlock()
		return nil
	}
	s.closed = true
	s.closedMu.Unlock()

	s.indexMu.Lock()
	defer s.indexMu.Unlock()

	if closer, ok := s.buf.(io.Closer); ok {
		defer func() {
			closeErr = errors.Join(closeErr, closer.Close())
		}()
	}

	// Build the final index.json and oci-layout entries into a buffer using a standard tar.Writer.
	var metaBuf bytes.Buffer
	tw := tar.NewWriter(&metaBuf)

	indexJSON, err := json.Marshal(s.index)
	if err != nil {
		return fmt.Errorf("failed to marshal index: %w", err)
	}
	if err := tw.WriteHeader(&tar.Header{
		Name: ociImageSpecV1.ImageIndexFile,
		Size: int64(len(indexJSON)),
	}); err != nil {
		return fmt.Errorf("failed to write index file to tar: %w", err)
	}
	if _, err := tw.Write(indexJSON); err != nil {
		return fmt.Errorf("failed to write index file content to tar: %w", err)
	}

	layout := ociImageSpecV1.ImageLayout{
		Version: ociImageSpecV1.ImageLayoutVersion,
	}
	layoutJSON, err := json.Marshal(layout)
	if err != nil {
		return fmt.Errorf("failed to marshal OCI layout file: %w", err)
	}
	if err := tw.WriteHeader(&tar.Header{
		Name: ociImageSpecV1.ImageLayoutFile,
		Size: int64(len(layoutJSON)),
	}); err != nil {
		return fmt.Errorf("failed to write layout file to tar: %w", err)
	}
	if _, err := tw.Write(layoutJSON); err != nil {
		return fmt.Errorf("failed to write layout file content to tar: %w", err)
	}

	if err := tw.Close(); err != nil {
		return fmt.Errorf("failed to close tar writer for metadata: %w", err)
	}

	// Write the metadata at the current offset in the scratch buffer
	metaBytes := metaBuf.Bytes()
	offset := s.nextOffset.Add(int64(len(metaBytes))) - int64(len(metaBytes))
	if _, err := s.buf.WriteAt(metaBytes, offset); err != nil {
		return fmt.Errorf("failed to write metadata to scratch buffer: %w", err)
	}

	// Seek to the beginning and copy the entire scratch buffer to the output
	if _, err := s.buf.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek scratch buffer: %w", err)
	}
	if _, err := io.Copy(s.output, s.buf); err != nil {
		return fmt.Errorf("failed to copy scratch buffer to output: %w", err)
	}

	return nil
}

// Push writes a blob to the tar archive. It is safe to call Push concurrently from multiple goroutines.
//
// Each call atomically reserves a byte range in the scratch buffer and writes the tar header + data
// to that exclusive region. If Push returns an error (e.g. digest mismatch), the reserved region
// contains invalid data; callers should not call Close() after a failed Push.
func (s *OCILayoutWriter) Push(ctx context.Context, expected ociImageSpecV1.Descriptor, data io.Reader) error {
	s.closedMu.Lock()
	closed := s.closed
	s.closedMu.Unlock()
	if closed {
		return fmt.Errorf("push to closed writer: %w", errdef.ErrUnsupported)
	}

	blobPath, err := blobPath(expected.Digest)
	if err != nil {
		return err
	}

	// Serialize the tar header into bytes
	headerBytes, err := serializeTarHeader(blobPath, expected.Size)
	if err != nil {
		return fmt.Errorf("failed to serialize tar header: %w", err)
	}

	// Calculate padded data size (tar entries are padded to 512-byte boundaries)
	paddedDataSize := (expected.Size + 511) &^ 511
	entrySize := int64(len(headerBytes)) + paddedDataSize

	// Atomically reserve a region in the temp file (no lock needed)
	offset := s.nextOffset.Add(entrySize) - entrySize

	// Write header at reserved offset (concurrent-safe: exclusive region)
	if _, err := s.buf.WriteAt(headerBytes, offset); err != nil {
		return fmt.Errorf("failed to write tar header to scratch buffer: %w", err)
	}

	// Write blob data right after the header
	dataOffset := offset + int64(len(headerBytes))
	verify := content.NewVerifyReader(data, expected)

	written, err := copyToWriterAt(s.buf, verify, dataOffset, expected.Size)
	if err != nil {
		return fmt.Errorf("failed to write content to scratch buffer: %w", err)
	}
	if err := verify.Verify(); err != nil {
		return fmt.Errorf("failed to verify content: %w", err)
	}

	// Zero-pad to 512-byte boundary
	if pad := paddedDataSize - written; pad > 0 {
		if _, err := s.buf.WriteAt(zeroPad[:pad], dataOffset+written); err != nil {
			return fmt.Errorf("failed to write padding to scratch buffer: %w", err)
		}
	}

	if introspection.IsOCICompliantManifest(expected) {
		if err := s.tag(ctx, expected, expected.Digest.String()); err != nil {
			return fmt.Errorf("failed to tag manifest by digest: %w", err)
		}
	}

	s.writtenMu.Lock()
	s.written[expected.Digest] = expected
	s.writtenMu.Unlock()

	return nil
}

// Exists returns true if the described content has been pushed.
func (s *OCILayoutWriter) Exists(_ context.Context, target ociImageSpecV1.Descriptor) (bool, error) {
	s.writtenMu.RLock()
	defer s.writtenMu.RUnlock()
	_, ok := s.written[target.Digest]
	return ok, nil
}

func (s *OCILayoutWriter) Tag(ctx context.Context, desc ociImageSpecV1.Descriptor, reference string) error {
	s.closedMu.Lock()
	closed := s.closed
	s.closedMu.Unlock()
	if closed {
		return fmt.Errorf("tag on closed writer: %w", errdef.ErrUnsupported)
	}

	if reference == "" {
		return errdef.ErrMissingReference
	}

	exists, err := s.Exists(ctx, desc)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%s: %s: %w", desc.Digest, desc.MediaType, errdef.ErrNotFound)
	}

	return s.tag(ctx, desc, reference)
}

func (s *OCILayoutWriter) tag(ctx context.Context, desc ociImageSpecV1.Descriptor, reference string) error {
	dgst := desc.Digest.String()
	if reference != dgst {
		// also tag desc by its digest
		if err := s.tagResolver.Tag(ctx, desc, dgst); err != nil {
			return err
		}
	}
	if err := s.tagResolver.Tag(ctx, desc, reference); err != nil {
		return err
	}
	return s.updateIndex()
}

func (s *OCILayoutWriter) Tags(_ context.Context, _ string, fn func(tags []string) error) error {
	s.indexMu.RLock()
	defer s.indexMu.RUnlock()

	arts := s.index.Manifests
	if len(arts) == 0 {
		return nil
	}

	tags := make([]string, 0, len(arts))
	for _, art := range arts {
		if art.Annotations != nil {
			if refName, ok := art.Annotations[ociImageSpecV1.AnnotationRefName]; ok {
				tags = append(tags, refName)
			}
		}
	}

	return fn(tags)
}

func (s *OCILayoutWriter) updateIndex() error {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()

	var manifests []ociImageSpecV1.Descriptor
	tagged := newSet[digest.Digest]()
	refMap := s.tagResolver.Map()

	// 1. Add descriptors that are associated with tags
	// Note: One descriptor can be associated with multiple tags.
	for ref, desc := range refMap {
		if ref != desc.Digest.String() {
			annotations := make(map[string]string, len(desc.Annotations)+1)
			maps.Copy(annotations, desc.Annotations)
			annotations[ociImageSpecV1.AnnotationRefName] = ref
			desc.Annotations = annotations
			manifests = append(manifests, desc)
			// mark the digest as tagged for deduplication in step 2
			tagged.Add(desc.Digest)
		}
	}
	// 2. Add descriptors that are not associated with any tag
	for ref, desc := range refMap {
		if ref == desc.Digest.String() && !tagged.Contains(desc.Digest) {
			// skip tagged ones since they have been added in step 1
			manifests = append(manifests, deleteAnnotationRefName(desc))
		}
	}

	s.index.Manifests = manifests
	return nil
}

// zeroPad is a static 512-byte zero block used for tar entry padding.
// Tar entries are padded to 512-byte boundaries; the maximum padding needed is 511 bytes.
var zeroPad [512]byte

var _ content.Pusher = &OCILayoutWriter{}

// copyBufPool reuses 32KB byte slices for copyToWriterAt.
var copyBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 32*1024)
		return &b
	},
}

// copyToWriterAt copies up to size bytes from r into w starting at the given offset,
// using a pooled buffer to avoid per-call allocations.
func copyToWriterAt(w io.WriterAt, r io.Reader, offset, size int64) (int64, error) {
	bufp := copyBufPool.Get().(*[]byte)
	defer copyBufPool.Put(bufp)
	buf := *bufp
	var total int64
	for total < size {
		n, err := r.Read(buf[:min(int64(len(buf)), size-total)])
		if n > 0 {
			if _, ew := w.WriteAt(buf[:n], offset+total); ew != nil {
				return total, ew
			}
			total += int64(n)
		}
		if err != nil {
			if err == io.EOF {
				return total, nil
			}
			return total, err
		}
	}
	return total, nil
}

// headerBufPool reuses bytes.Buffer instances for tar header serialization.
var headerBufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// serializeTarHeader writes a tar header into a buffer and returns a copy of the serialized bytes.
func serializeTarHeader(name string, size int64) ([]byte, error) {
	buf := headerBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer headerBufPool.Put(buf)

	tw := tar.NewWriter(buf)
	if err := tw.WriteHeader(&tar.Header{
		Name: name,
		Size: size,
	}); err != nil {
		return nil, err
	}
	// After WriteHeader, buf contains the header bytes. The tar.Writer expects
	// `size` bytes of data to follow, but we only want the header portion.
	// We return a copy since the pooled buffer will be reused.
	return bytes.Clone(buf.Bytes()), nil
}

// blobPath calculates blob path from the given digest.
func blobPath(dgst digest.Digest) (string, error) {
	if err := dgst.Validate(); err != nil {
		return "", fmt.Errorf("cannot calculate blob path from invalid digest %s: %w: %w",
			dgst.String(), errdef.ErrInvalidDigest, err)
	}
	return path.Join(ociImageSpecV1.ImageBlobsDir, dgst.Algorithm().String(), dgst.Encoded()), nil
}

// memoryResolver is a memory based resolver.
type memoryResolver struct {
	lock  sync.RWMutex
	index map[string]ociImageSpecV1.Descriptor
	tags  map[digest.Digest]set[string]
}

// newMemoryResolver creates a new memoryResolver resolver.
func newMemoryResolver() *memoryResolver {
	return &memoryResolver{
		index: make(map[string]ociImageSpecV1.Descriptor),
		tags:  make(map[digest.Digest]set[string]),
	}
}

// Resolve resolves a reference to a descriptor.
func (m *memoryResolver) Resolve(_ context.Context, reference string) (ociImageSpecV1.Descriptor, error) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	desc, ok := m.index[reference]
	if !ok {
		return ociImageSpecV1.Descriptor{}, errdef.ErrNotFound
	}
	return desc, nil
}

// Tag tags a descriptor with a reference string.
func (m *memoryResolver) Tag(_ context.Context, desc ociImageSpecV1.Descriptor, reference string) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.index[reference] = desc
	tagSet, ok := m.tags[desc.Digest]
	if !ok {
		tagSet = newSet[string]()
		m.tags[desc.Digest] = tagSet
	}
	tagSet.Add(reference)
	return nil
}

// Untag removes a reference from index map.
func (m *memoryResolver) Untag(reference string) {
	m.lock.Lock()
	defer m.lock.Unlock()

	desc, ok := m.index[reference]
	if !ok {
		return
	}
	delete(m.index, reference)
	tagSet := m.tags[desc.Digest]
	tagSet.Delete(reference)
	if len(tagSet) == 0 {
		delete(m.tags, desc.Digest)
	}
}

// Map dumps the memory into a built-in map structure.
// Like other operations, calling Map() is go-routine safe.
func (m *memoryResolver) Map() map[string]ociImageSpecV1.Descriptor {
	m.lock.RLock()
	defer m.lock.RUnlock()

	return maps.Clone(m.index)
}

// TagSet returns the set of tags of the descriptor.
func (m *memoryResolver) TagSet(desc ociImageSpecV1.Descriptor) set[string] {
	m.lock.RLock()
	defer m.lock.RUnlock()

	tagSet := m.tags[desc.Digest]
	return maps.Clone(tagSet)
}

// set represents a set data structure.
type set[T comparable] map[T]struct{}

// newSet New returns an initialized set.
func newSet[T comparable]() set[T] {
	return make(set[T])
}

// Add adds item into the set s.
func (s set[T]) Add(item T) {
	s[item] = struct{}{}
}

// Contains returns true if the set s contains item.
func (s set[T]) Contains(item T) bool {
	_, ok := s[item]
	return ok
}

// Delete deletes an item from the set.
func (s set[T]) Delete(item T) {
	delete(s, item)
}

// deleteAnnotationRefName deletes the AnnotationRefName from the annotation map
// of desc.
func deleteAnnotationRefName(desc ociImageSpecV1.Descriptor) ociImageSpecV1.Descriptor {
	if _, ok := desc.Annotations[ociImageSpecV1.AnnotationRefName]; !ok {
		// no ops
		return desc
	}

	size := len(desc.Annotations) - 1
	if size == 0 {
		desc.Annotations = nil
		return desc
	}

	annotations := make(map[string]string, size)
	for k, v := range desc.Annotations {
		if k != ociImageSpecV1.AnnotationRefName {
			annotations[k] = v
		}
	}
	desc.Annotations = annotations
	return desc
}
