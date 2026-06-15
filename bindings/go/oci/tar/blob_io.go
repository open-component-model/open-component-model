package tar

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
)

type CopyToOCILayoutOptions struct {
	// ExtendedCopyGraphOptions drives the copy of base into the layout via
	// oras.ExtendedCopyGraph: predecessors (e.g. OCI referrers) ride along with
	// base, so a referrer whose subject edge points "backwards" at base — which
	// a plain CopyGraph would never reach — still travels. The zero value uses
	// oras's defaults: src.Predecessors and unbounded Depth.
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

	// Apply any additional tags.
	for _, tag := range opts.Tags {
		if err = errors.Join(err, target.Tag(ctx, base, tag)); err != nil {
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
// layout's single top-level manifest or index (or the one tagged via
// `org.opencontainers.image.ref.name` when multiple are present), and copies
// its full graph into dst via [oras.ExtendedCopyGraph]. Predecessors of the
// top-level descriptor (e.g. OCI referrers carried in the source layout) ride
// along: oras walks them via src.Predecessors and copies each as its own
// root, so a referrer still lands when the artifact root is already present
// in dst.
//
// [CopyOCILayoutWithIndexOptions.MutateParentFunc] runs once against the
// top-level descriptor before copy. Typical use: inject annotations onto the
// root manifest/index. The mutated descriptor reaches the destination because
// a small in-memory proxy serves the original bytes for the post-mutation
// digest, and the destination's Push records the mutated Descriptor in its
// layout's index.json (registry destinations drop annotations at the wire
// boundary; layout destinations preserve them).
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

	index, proxy, err := proxyOCIStore(ctx, ociStore, &opts)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to create proxy for OCI store: %w", err)
	}

	// Walk: ExtendedCopyGraph reaches the mutated root only as the Subject of
	// its referrer — the un-mutated copy embedded in the referrer body. The
	// referrer is processed first, its Subject pushed to dst, and dst's
	// tagResolver records that un-mutated descriptor. Swap it for the mutated
	// one in FindSuccessors so the destination index.json keeps the injected
	// annotations.
	upstream := opts.ExtendedCopyGraphOptions
	innerFindSuccessors := upstream.FindSuccessors
	upstream.FindSuccessors = func(ctx context.Context, fetcher content.Fetcher, desc ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error) {
		var (
			successors []ociImageSpecV1.Descriptor
			err        error
		)
		if innerFindSuccessors != nil {
			successors, err = innerFindSuccessors(ctx, fetcher, desc)
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

	if err := oras.ExtendedCopyGraph(ctx, proxy, dst, index, upstream); err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to copy graph for index from oci layout %v: %w", index, err)
	}

	return index, nil
}

func proxyOCIStore(ctx context.Context, ociStore *CloseableReadOnlyStore, opts *CopyOCILayoutWithIndexOptions) (ociImageSpecV1.Descriptor, content.ReadOnlyGraphStorage, error) {
	// if our store only has one single descriptor, we dont need to copy the top level index of the layout.
	// instead we can use whatever top level descriptor (manifest or index) is located as singleton in the layout index.
	if len(ociStore.Index.Manifests) == 1 {
		return proxyOCIStoreWithTopLevelDescriptor(ctx, 0, ociStore, opts)
	}
	var topLevelNamedDescriptors []int
	for idx, manifest := range ociStore.Index.Manifests {
		if manifest.Annotations != nil && manifest.Annotations[ociImageSpecV1.AnnotationRefName] != "" {
			topLevelNamedDescriptors = append(topLevelNamedDescriptors, idx)
		}
	}
	if len(topLevelNamedDescriptors) == 1 {
		return proxyOCIStoreWithTopLevelDescriptor(ctx, topLevelNamedDescriptors[0], ociStore, opts)
	}

	// we need this specifically for docker (one manifest),
	// and oras / ocm packaging compat (many manifests, exactly one ref.name)
	return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf(
		"multiple manifests found in oci store, "+
			"but no manifest could be identified as the top level parent."+
			"the store must either contain exactly one top level manifest in its index,"+
			" or at most one manifest with the annotation %s", ociImageSpecV1.AnnotationRefName,
	)
}

func proxyOCIStoreWithTopLevelDescriptor(ctx context.Context, idx int, ociStore *CloseableReadOnlyStore, opts *CopyOCILayoutWithIndexOptions) (_ ociImageSpecV1.Descriptor, _ content.ReadOnlyGraphStorage, err error) {
	topLevelDesc := ociStore.Index.Manifests[idx]
	descStream, err := ociStore.Fetch(ctx, topLevelDesc)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("failed to fetch top level descriptor from store: %w", err)
	}
	defer func() {
		err = errors.Join(err, descStream.Close())
	}()
	descRaw, err := content.ReadAll(descStream, topLevelDesc)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("failed to read top level descriptor stream: %w", err)
	}

	switch topLevelDesc.MediaType {
	case ociImageSpecV1.MediaTypeImageManifest:
		var manifest ociImageSpecV1.Manifest
		if err := json.Unmarshal(descRaw, &manifest); err != nil {
			return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("failed to unmarshal manifest: %w", err)
		}
		if err := opts.MutateParentFunc(&topLevelDesc); err != nil {
			return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("failed to mutate manifest descriptor before copy: %w", err)
		}
	case ociImageSpecV1.MediaTypeImageIndex:
		var index ociImageSpecV1.Index
		if err := json.Unmarshal(descRaw, &index); err != nil {
			return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("failed to unmarshal index: %w", err)
		}
		if err := opts.MutateParentFunc(&topLevelDesc); err != nil {
			return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("failed to mutate index descriptor before copy: %w", err)
		}
	}

	proxy := &descriptorStoreProxy{
		raw:                  descRaw,
		desc:                 topLevelDesc,
		ReadOnlyGraphStorage: ociStore,
	}
	return topLevelDesc, proxy, nil
}

// mediaTypeOCIArtifactManifest is the deprecated OCI artifact manifest (image-spec v1.1.0-rc1/rc2); the oras-go constant lives in an internal package.
const mediaTypeOCIArtifactManifest = "application/vnd.oci.artifact.manifest.v1+json"

// successorsWithoutSubject works like [content.Successors] but never returns
// the Subject of an OCI image manifest, image index, or (deprecated) artifact
// manifest. Other descriptor types fall through to [content.Successors] since
// they have no Subject field in their schema.
func successorsWithoutSubject(ctx context.Context, fetcher content.Fetcher, node ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error) {
	switch node.MediaType {
	case ociImageSpecV1.MediaTypeImageManifest:
		raw, err := content.FetchAll(ctx, fetcher, node)
		if err != nil {
			return nil, err
		}
		var manifest ociImageSpecV1.Manifest
		if err := json.Unmarshal(raw, &manifest); err != nil {
			return nil, err
		}
		return append([]ociImageSpecV1.Descriptor{manifest.Config}, manifest.Layers...), nil
	case ociImageSpecV1.MediaTypeImageIndex:
		raw, err := content.FetchAll(ctx, fetcher, node)
		if err != nil {
			return nil, err
		}
		var index ociImageSpecV1.Index
		if err := json.Unmarshal(raw, &index); err != nil {
			return nil, err
		}
		return index.Manifests, nil
	case mediaTypeOCIArtifactManifest:
		raw, err := content.FetchAll(ctx, fetcher, node)
		if err != nil {
			return nil, err
		}
		var manifest struct {
			Blobs []ociImageSpecV1.Descriptor `json:"blobs,omitempty"`
		}
		if err := json.Unmarshal(raw, &manifest); err != nil {
			return nil, err
		}
		return manifest.Blobs, nil
	}
	return content.Successors(ctx, fetcher, node)
}

// Subject returns the subject descriptor declared by desc, or nil if it has
// none — a non-nil result means desc is a referrer. It decodes only the subject
// field from the body of an image manifest, image index, or (deprecated)
// artifact manifest; any other media type has no subject and is not fetched.
//
// This replicates oras-go's internal manifestutil.Subject, which is not
// importable from outside oras-go.
func Subject(ctx context.Context, fetcher content.Fetcher, desc ociImageSpecV1.Descriptor) (*ociImageSpecV1.Descriptor, error) {
	switch desc.MediaType {
	case ociImageSpecV1.MediaTypeImageManifest, ociImageSpecV1.MediaTypeImageIndex, mediaTypeOCIArtifactManifest:
		raw, err := content.FetchAll(ctx, fetcher, desc)
		if err != nil {
			return nil, err
		}
		var manifest struct {
			Subject *ociImageSpecV1.Descriptor `json:"subject,omitempty"`
		}
		if err := json.Unmarshal(raw, &manifest); err != nil {
			return nil, err
		}
		return manifest.Subject, nil
	default:
		return nil, nil
	}
}

// descriptorStoreProxy serves the original bytes of the layout's top-level
// descriptor for its post-MutateParentFunc digest, so the destination's Push
// records the mutated Descriptor (annotations, platform) in its layout's
// index.json. Predecessor walks (Predecessors) flow through to the underlying
// store unchanged.
type descriptorStoreProxy struct {
	raw  []byte
	desc ociImageSpecV1.Descriptor
	content.ReadOnlyGraphStorage
}

func (p *descriptorStoreProxy) Exists(ctx context.Context, desc ociImageSpecV1.Descriptor) (bool, error) {
	if p.desc.Digest.String() == desc.Digest.String() {
		return true, nil
	}
	return p.ReadOnlyGraphStorage.Exists(ctx, desc)
}

func (p *descriptorStoreProxy) Fetch(ctx context.Context, desc ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	if p.desc.Digest.String() == desc.Digest.String() {
		return io.NopCloser(bytes.NewReader(p.raw)), nil
	}
	return p.ReadOnlyGraphStorage.Fetch(ctx, desc)
}
