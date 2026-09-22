package tar

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
)

type CopyToOCILayoutOptions struct {
	oras.ExtendedCopyGraphOptions
	Tags    []string
	TempDir string
}

// CopyToOCILayoutInMemory streams the contents of an OCI graph from the given
// ReadOnlyGraphStorage into an in-memory OCI layout archive (gzipped tar),
// returning a Blob that can be read by consumers. The actual copy happens
// asynchronously in a goroutine; if the caller never reads from the returned
// Blob, the copy will block.
//
// Returns an inmemory.Blob wrapping the read side of a pipe, with media type
// [layout.MediaTypeOCIImageLayoutTarGzipV1].
func CopyToOCILayoutInMemory(ctx context.Context, src content.ReadOnlyGraphStorage, base ociImageSpecV1.Descriptor, opts CopyToOCILayoutOptions) (*inmemory.Blob, error) {
	r, w := io.Pipe()

	go copyToOCILayoutInMemoryAsync(ctx, src, base, opts, w)

	downloaded := inmemory.New(r, inmemory.WithMediaType(layout.MediaTypeOCIImageLayoutTarGzipV1))
	return downloaded, nil
}

// copyToOCILayoutInMemoryAsync performs the actual OCI‐layout archive creation
// and writes it into the provided PipeWriter. Any error (from CopyGraph,
// gzip, or OCILayoutWriter) is joined and propagated via the pipe's [io.PipeWriter.CloseWithError],
// causing any reader to receive an error when reading from the pipe.
func copyToOCILayoutInMemoryAsync(ctx context.Context, src content.ReadOnlyGraphStorage, base ociImageSpecV1.Descriptor, opts CopyToOCILayoutOptions, w *io.PipeWriter) {
	// err accumulates any error from copy, gzip, or layout writing.
	var err error
	defer func() {
		w.CloseWithError(err)
	}()

	zippedBuf := gzip.NewWriter(w)
	defer func() {
		err = errors.Join(err, zippedBuf.Close())
	}()

	// Create an OCI layout writer over the gzip stream.
	target, targetErr := NewOCILayoutWriterWithTempFile(zippedBuf, opts.TempDir)
	if targetErr != nil {
		err = targetErr
		return
	}
	defer func() {
		err = errors.Join(err, target.Close())
	}()

	if err = errors.Join(err, oras.ExtendedCopyGraph(ctx, src, target, base, opts.ExtendedCopyGraphOptions)); err != nil {
		return
	}

	// base is the only thing that knows which manifest the caller wanted.
	// Tag is what carries a descriptor's annotations into index.json.
	root := base
	root.Annotations = maps.Clone(base.Annotations)
	if root.Annotations == nil {
		root.Annotations = make(map[string]string, 1)
	}
	root.Annotations[annotations.OCMLayoutRoot] = "true"

	for _, ref := range append([]string{base.Digest.String()}, opts.Tags...) {
		if err = errors.Join(err, target.Tag(ctx, root, ref)); err != nil {
			return
		}
	}
}

type CopyOCILayoutWithIndexOptions struct {
	oras.ExtendedCopyGraphOptions
	// MutateParentFunc runs once against the layout's top-level descriptor
	// before the copy. Callers may mutate Annotations and Platform; they must
	// not change Digest, Size, or MediaType. Those three participate in OCI
	// subject-edge equality, so altering them would invalidate any inbound
	// referrer pointing at this descriptor. The constraint is documentation
	// only — there is no runtime enforcement.
	MutateParentFunc func(*ociImageSpecV1.Descriptor) error
}

// CopyOCILayoutWithIndex reads an OCI layout tarball from src, picks the
// layout's top-level manifest or index via [pickTopLevelDescriptor], and copies
// its full graph into dst via [oras.ExtendedCopyGraph], including referrers.
//
// Returns the descriptor of the root that was copied.
func CopyOCILayoutWithIndex(ctx context.Context, dst content.Storage, src blob.ReadOnlyBlob, opts CopyOCILayoutWithIndexOptions) (top ociImageSpecV1.Descriptor, err error) {
	ociStore, err := ReadOCILayout(ctx, src)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to read OCI layout: %w", err)
	}
	defer func() {
		err = errors.Join(err, ociStore.Close())
	}()

	index, err := pickTopLevelDescriptor(ctx, ociStore)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, err
	}
	// We call the mutateParentFunc here instead of directly in FindSuccessors
	// as the FindSuccessors path is only reached if there is a referrer in the
	// source layout.
	if opts.MutateParentFunc != nil {
		if err := opts.MutateParentFunc(&index); err != nil {
			return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to mutate top level descriptor before copy: %w", err)
		}
	}

	// ExtendedCopyGraph reaches the mutated root descriptor (the descriptor,
	// NOT the manifest) only as the Subject descriptor of its referrer. The
	// Subject descriptor is not mutated.
	// Swap the Subject descriptor with the mutated one.
	extendedCopyOpts := opts.ExtendedCopyGraphOptions
	extendedCopyOpts.FindSuccessors = func(ctx context.Context, fetcher content.Fetcher, desc ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error) {
		var (
			successors []ociImageSpecV1.Descriptor
			err        error
		)
		if opts.FindSuccessors != nil {
			successors, err = opts.FindSuccessors(ctx, fetcher, desc)
		} else {
			successors, err = content.Successors(ctx, fetcher, desc)
		}
		if err != nil {
			return nil, err
		}
		for i := range successors {
			if successors[i].Digest == index.Digest {
				successors[i] = index
			}
		}
		return successors, nil
	}

	if err := oras.ExtendedCopyGraph(ctx, ociStore, dst, index, extendedCopyOpts); err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to copy graph for index from oci layout %v: %w", index, err)
	}

	return index, nil
}

// pickTopLevelDescriptor works out which manifest in the layout was requested,
// in order: the only one; the one marked with [annotations.OCMLayoutRoot]; the
// one named by `org.opencontainers.image.ref.name`, only ever set for tag-based
// references; else [TopLevelArtifacts] for a layout carrying neither. Errors
// when nothing settles it, because a wrong guess silently packs the wrong
// artifact.
func pickTopLevelDescriptor(ctx context.Context, ociStore *CloseableReadOnlyStore) (ociImageSpecV1.Descriptor, error) {
	if len(ociStore.Index.Manifests) == 1 {
		return ociStore.Index.Manifests[0], nil
	}
	if root, ok := markedRoot(ociStore.Index.Manifests); ok {
		return root, nil
	}
	var named []int
	for idx, manifest := range ociStore.Index.Manifests {
		if manifest.Annotations != nil && manifest.Annotations[ociImageSpecV1.AnnotationRefName] != "" {
			named = append(named, idx)
		}
	}
	if len(named) == 1 {
		return ociStore.Index.Manifests[named[0]], nil
	}
	// Nothing was written down, so infer it: an index and the children it lists
	// collapse to the index. Last, because this branch is the one that guesses.
	if top := TopLevelArtifacts(ctx, ociStore, ociStore.Index.Manifests); len(top) == 1 {
		return top[0], nil
	}
	return ociImageSpecV1.Descriptor{}, fmt.Errorf(
		"multiple manifests found in oci store, "+
			"but no manifest could be identified as the top level parent."+
			" the store must either contain exactly one top level manifest in its index,"+
			" one marked with the annotation %s, at most one manifest with the annotation %s,"+
			" or a single artifact containing all the others",
		annotations.OCMLayoutRoot, ociImageSpecV1.AnnotationRefName,
	)
}

// Docker manifest media types. Carry no subject field, so they are forwarded
// to [content.Successors] for layer/child enumeration with a nil subject.
const (
	mediaTypeDockerManifest     = "application/vnd.docker.distribution.manifest.v2+json"
	mediaTypeDockerManifestList = "application/vnd.docker.distribution.manifest.list.v2+json"
)

// extractSubjectAndSuccessors decodes desc once and returns its subject (nil if desc is not a
// referrer) and its containment successors (config+layers, child manifests, or
// blobs depending on media type). Docker manifest types have no subject and
// are forwarded to [content.Successors]. Any other media type returns
// (nil, nil, nil) — it is not fetched and contributes no edges.
func extractSubjectAndSuccessors(ctx context.Context, fetcher content.Fetcher, desc ociImageSpecV1.Descriptor) (*ociImageSpecV1.Descriptor, []ociImageSpecV1.Descriptor, error) {
	switch desc.MediaType {
	case ociImageSpecV1.MediaTypeImageManifest:
		raw, err := content.FetchAll(ctx, fetcher, desc)
		if err != nil {
			return nil, nil, err
		}
		var manifest ociImageSpecV1.Manifest
		if err := json.Unmarshal(raw, &manifest); err != nil {
			return nil, nil, err
		}
		return manifest.Subject, append([]ociImageSpecV1.Descriptor{manifest.Config}, manifest.Layers...), nil
	case ociImageSpecV1.MediaTypeImageIndex:
		raw, err := content.FetchAll(ctx, fetcher, desc)
		if err != nil {
			return nil, nil, err
		}
		var index ociImageSpecV1.Index
		if err := json.Unmarshal(raw, &index); err != nil {
			return nil, nil, err
		}
		return index.Subject, index.Manifests, nil
	case mediaTypeDockerManifest, mediaTypeDockerManifestList:
		successors, err := content.Successors(ctx, fetcher, desc)
		return nil, successors, err
	}
	return nil, nil, nil
}
