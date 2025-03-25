package oci

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"path"
	"sync"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/errdef"
)

// NewOCILayoutTarWriter creates a new oras.Target that writes to the given writer an oci-layout in tar format.
// The index and layout files are written to the storage when it is closed.
// This writer is bound to serialization of tar archives and thus cannot be concurrently copied to efficiently, however
// it is significantly more efficient in terms of I/O than writing an OCI Layout to the filesystem and then
// tarring the OCI Layout directory.
// As such, this writer is optimized for I/O and memory efficiency, not for concurrency.
// Note however that in most modern systems, I/O is generally fast enough (ssd) to keep up with
// network speeds and concurrent reads, so this should not be a problem in most cases,
// as long as your final goal is a tar of an oci layout and not just the oci layout itself.
func NewOCILayoutTarWriter(w io.Writer) CloseableTarget {
	return &ociLayoutTarWriter{
		writer:      tar.NewWriter(w),
		tagResolver: newMemoryResolver(),
		index: &ociImageSpecV1.Index{
			Versioned: specs.Versioned{
				SchemaVersion: 2, // historical value
			},
			Manifests: []ociImageSpecV1.Descriptor{},
		},
	}
}

// CloseableTarget is a target that can be closed after which it can no longer be operated on.
type CloseableTarget interface {
	oras.Target
	io.Closer
}

var (
	_ oras.Target = &ociLayoutTarWriter{}
)

type ociLayoutTarWriter struct {
	writeLock sync.Mutex
	writer    *tar.Writer

	indexSync sync.RWMutex
	index     *ociImageSpecV1.Index

	tagResolver *memoryResolver
}

// Fetch is only implemented to satisfy the oras.Target interface.
func (s *ociLayoutTarWriter) Fetch(ctx context.Context, target ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	return nil, errdef.ErrUnsupported
}

func (s *ociLayoutTarWriter) Resolve(ctx context.Context, reference string) (ociImageSpecV1.Descriptor, error) {
	return s.tagResolver.Resolve(ctx, reference)
}

func (s *ociLayoutTarWriter) Close() error {
	s.indexSync.Lock()
	defer s.indexSync.Unlock()
	indexJSON, err := json.Marshal(s.index)
	if err := s.writer.WriteHeader(&tar.Header{
		Name: ociImageSpecV1.ImageIndexFile,
		Size: int64(len(indexJSON)),
	}); err != nil {
		return fmt.Errorf("failed to write index file to tar: %w", err)
	}
	if _, err := io.Copy(s.writer, bytes.NewReader(indexJSON)); err != nil {
		return fmt.Errorf("failed to write layout file content to tar: %w", err)
	}

	layout := ociImageSpecV1.ImageLayout{
		Version: ociImageSpecV1.ImageLayoutVersion,
	}
	layoutJSON, err := json.Marshal(layout)
	if err != nil {
		return fmt.Errorf("failed to marshal OCI layout file: %w", err)
	}
	if err := s.writer.WriteHeader(&tar.Header{
		Name: ociImageSpecV1.ImageLayoutFile,
		Size: int64(len(layoutJSON)),
	}); err != nil {
		return fmt.Errorf("failed to write layout file to tar: %w", err)
	}
	if _, err := io.Copy(s.writer, bytes.NewReader(layoutJSON)); err != nil {
		return fmt.Errorf("failed to write layout file content to tar: %w", err)
	}

	return s.writer.Close()
}

func (s *ociLayoutTarWriter) Push(ctx context.Context, expected ociImageSpecV1.Descriptor, data io.Reader) error {
	blobPath, err := blobPath(expected.Digest)
	if err != nil {
		return err
	}

	s.writeLock.Lock()
	defer s.writeLock.Unlock()
	if err := s.writer.WriteHeader(&tar.Header{
		Name: blobPath,
		Size: expected.Size,
	}); err != nil {
		return fmt.Errorf("failed to write tar header: %w", err)
	}
	verify := content.NewVerifyReader(data, expected)
	if _, err := io.Copy(s.writer, verify); err != nil {
		return fmt.Errorf("failed to write content to tar: %w", err)
	}
	if err := verify.Verify(); err != nil {
		return fmt.Errorf("failed to verify content: %w", err)
	}
	if isManifest(expected) {
		return s.tag(ctx, expected, expected.Digest.String())
	}
	return nil
}

// Exists returns true if the described content Exists.
func (s *ociLayoutTarWriter) Exists(_ context.Context, target ociImageSpecV1.Descriptor) (bool, error) {
	s.indexSync.RLock()
	defer s.indexSync.RUnlock()
	for _, manifest := range s.index.Manifests {
		if manifest.Digest == target.Digest {
			return true, nil
		}
	}
	return false, nil
}

func (s *ociLayoutTarWriter) Tag(ctx context.Context, desc ociImageSpecV1.Descriptor, reference string) error {
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

func (s *ociLayoutTarWriter) tag(ctx context.Context, desc ociImageSpecV1.Descriptor, reference string) error {
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

func (s *ociLayoutTarWriter) updateIndex() error {

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

var _ content.Pusher = &ociLayoutTarWriter{}

// blobPath calculates blob path from the given digest.
func blobPath(dgst digest.Digest) (string, error) {
	if err := dgst.Validate(); err != nil {
		return "", fmt.Errorf("cannot calculate blob path from invalid digest %s: %w: %v",
			dgst.String(), errdef.ErrInvalidDigest, err)
	}
	return path.Join(ociImageSpecV1.ImageBlobsDir, dgst.Algorithm().String(), dgst.Encoded()), nil
}

// isManifest checks if a descriptor describes a manifest.
func isManifest(desc ociImageSpecV1.Descriptor) bool {
	switch desc.MediaType {
	case "application/vnd.docker.distribution.manifest.v2+json",
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.oci.artifact.manifest.v1+json",
		ociImageSpecV1.MediaTypeImageManifest,
		ociImageSpecV1.MediaTypeImageIndex:
		return true
	default:
		return false
	}
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
