package tar

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
)

type CopyToOCILayoutOptions struct {
	oras.CopyGraphOptions
	Tags    []string
	TempDir string
}

// CopyToOCILayoutInMemory streams the contents of an OCI graph from the given
// ReadOnlyStorage into an in-memory OCI layout archive (gzipped tar), returning
// a Blob that can be read by consumers. The actual copy happens asynchronously
// in a goroutine; if the caller never reads from the returned Blob, the copy
// will block.
//
// Returns an inmemory.Blob wrapping the read side of a pipe, with media type
// [layout.MediaTypeOCIImageLayoutTarGzipV1].
func CopyToOCILayoutInMemory(ctx context.Context, src content.ReadOnlyStorage, base ociImageSpecV1.Descriptor, opts CopyToOCILayoutOptions) (*inmemory.Blob, error) {
	r, w := io.Pipe()

	go copyToOCILayoutInMemoryAsync(ctx, src, base, opts, w)

	downloaded := inmemory.New(r, inmemory.WithMediaType(layout.MediaTypeOCIImageLayoutTarGzipV1))
	return downloaded, nil
}

// copyToOCILayoutInMemoryAsync performs the actual OCI‐layout archive creation
// and writes it into the provided PipeWriter. Any error (from CopyGraph,
// gzip, or OCILayoutWriter) is joined and propagated via the pipe's [io.PipeWriter.CloseWithError],
// causing any reader to receive an error when reading from the pipe.
func copyToOCILayoutInMemoryAsync(ctx context.Context, src content.ReadOnlyStorage, base ociImageSpecV1.Descriptor, opts CopyToOCILayoutOptions, w *io.PipeWriter) {
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

	// Copy the image graph into the layout.
	if err = errors.Join(err, oras.CopyGraph(ctx, src, target, base, opts.CopyGraphOptions)); err != nil {
		return
	}

	// Apply any additional tags.
	for _, tag := range opts.Tags {
		if err = errors.Join(err, target.Tag(ctx, base, tag)); err != nil {
			return
		}
	}
}

// Referrer pairs a descriptor with its raw content bytes — extra content the
// caller wants pushed alongside the artifact root that the source OCI layout
// store does not already hold (typically an OCI referrer manifest plus the
// blobs it references).
type Referrer struct {
	Descriptor ociImageSpecV1.Descriptor
	Raw        []byte
}

// ReferrersFunc returns referrers to be copied as additional children of top in
// the same CopyGraph traversal — e.g. OCI referrers, which CopyGraph does not
// follow by default.
type ReferrersFunc func(ctx context.Context, top ociImageSpecV1.Descriptor) ([]Referrer, error)

type CopyOCILayoutWithIndexOptions struct {
	oras.CopyGraphOptions
	MutateParentFunc func(*ociImageSpecV1.Descriptor) error
	ReferrersFunc    []ReferrersFunc
}

// CopyOCILayoutWithIndex reads an OCI layout tarball from src, picks the
// layout's single top-level manifest or index (or the one tagged via
// `org.opencontainers.image.ref.name` when multiple are present), and copies
// its full graph into dst via [oras.CopyGraph].
//
// Two hooks let the caller adjust the copy:
//
//   - [CopyOCILayoutWithIndexOptions.MutateParentFunc] runs once against the
//     top-level descriptor before copy. Typical use: inject annotations onto
//     the root manifest/index. Required.
//   - [CopyOCILayoutWithIndexOptions.ReferrersFunc] returns extra [Referrer]s
//     to be pushed alongside the artifact in the same traversal — e.g. OCI
//     referrer manifests, which [oras.CopyGraph]'s default `FindSuccessors`
//     does not follow via the `subject` field. Each returned referrer is
//     served from an in-memory proxy and injected as an additional child of
//     the root.
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

	if err := oras.CopyGraph(ctx, proxy, dst, index, opts.CopyGraphOptions); err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to copy graph for index from oci layout %v: %w", index, err)
	}

	return index, nil
}

func proxyOCIStore(ctx context.Context, ociStore *CloseableReadOnlyStore, opts *CopyOCILayoutWithIndexOptions) (ociImageSpecV1.Descriptor, content.ReadOnlyStorage, error) {
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

func proxyOCIStoreWithTopLevelDescriptor(ctx context.Context, idx int, ociStore *CloseableReadOnlyStore, opts *CopyOCILayoutWithIndexOptions) (_ ociImageSpecV1.Descriptor, _ content.ReadOnlyStorage, err error) {
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

	referrers, referrerDescriptors, err := resolveReferrers(ctx, opts.ReferrersFunc, topLevelDesc)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, nil, err
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
		// rootChildren are what FindSuccessors returns for the (mutated) root: the manifest's
		// own config + layers, plus the referrers the caller wants attached. CopyGraph's default
		// successor traversal would re-fetch the (now-stale) manifest bytes from the source store
		// and miss the injected referrers — supplying the children explicitly avoids both.
		rootChildren := append(append([]ociImageSpecV1.Descriptor{manifest.Config}, manifest.Layers...), referrerDescriptors...)
		opts.FindSuccessors = findSuccessorsForRoot(topLevelDesc, rootChildren)
	case ociImageSpecV1.MediaTypeImageIndex:
		var index ociImageSpecV1.Index
		if err := json.Unmarshal(descRaw, &index); err != nil {
			return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("failed to unmarshal index: %w", err)
		}
		if err := opts.MutateParentFunc(&topLevelDesc); err != nil {
			return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("failed to mutate index descriptor before copy: %w", err)
		}
		rootChildren := append(append([]ociImageSpecV1.Descriptor{}, index.Manifests...), referrerDescriptors...)
		opts.FindSuccessors = findSuccessorsForRoot(topLevelDesc, rootChildren)
	}

	proxy := &descriptorStoreProxy{
		raw:             descRaw,
		desc:            topLevelDesc,
		ReadOnlyStorage: ociStore,
		referrers:       referrers,
	}
	return topLevelDesc, proxy, nil
}

// resolveReferrers invokes each ReferrersFunc against topLevelDesc and returns
// the flattened list of referrers plus a parallel slice of just their
// descriptors. The full Referrer slice is what the proxy serves on Fetch; the
// descriptor slice is what gets injected as additional successors of the root
// in CopyGraph.
func resolveReferrers(ctx context.Context, funcs []ReferrersFunc, topLevelDesc ociImageSpecV1.Descriptor) ([]Referrer, []ociImageSpecV1.Descriptor, error) {
	var referrers []Referrer
	var referrerDescriptors []ociImageSpecV1.Descriptor
	for _, fn := range funcs {
		refs, err := fn(ctx, topLevelDesc)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to resolve referrer: %w", err)
		}
		referrers = append(referrers, refs...)
		for _, ref := range refs {
			referrerDescriptors = append(referrerDescriptors, ref.Descriptor)
		}
	}
	return referrers, referrerDescriptors, nil
}

// findSuccessorsForRoot returns a CopyGraph FindSuccessors that returns the
// pre-computed rootChildren when called on topLevelDesc and falls back to
// [successorsWithoutSubject] for every other descriptor — so referrers whose
// Subject points back to topLevelDesc are not re-traversed.
func findSuccessorsForRoot(topLevelDesc ociImageSpecV1.Descriptor, rootChildren []ociImageSpecV1.Descriptor) func(ctx context.Context, fetcher content.Fetcher, desc ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error) {
	return func(ctx context.Context, fetcher content.Fetcher, desc ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error) {
		if content.Equal(desc, topLevelDesc) {
			return rootChildren, nil
		}
		return successorsWithoutSubject(ctx, fetcher, desc)
	}
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

type descriptorStoreProxy struct {
	raw       []byte
	desc      ociImageSpecV1.Descriptor
	referrers []Referrer
	content.ReadOnlyStorage
}

func (p *descriptorStoreProxy) Exists(ctx context.Context, desc ociImageSpecV1.Descriptor) (bool, error) {
	if p.desc.Digest.String() == desc.Digest.String() {
		return true, nil
	}
	if slices.ContainsFunc(p.referrers, func(r Referrer) bool {
		return r.Descriptor.Digest == desc.Digest
	}) {
		return true, nil
	}
	return p.ReadOnlyStorage.Exists(ctx, desc)
}

func (p *descriptorStoreProxy) Fetch(ctx context.Context, desc ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	if p.desc.Digest.String() == desc.Digest.String() {
		return io.NopCloser(bytes.NewReader(p.raw)), nil
	}
	for _, ref := range p.referrers {
		if ref.Descriptor.Digest == desc.Digest {
			return io.NopCloser(bytes.NewReader(ref.Raw)), nil
		}
	}
	return p.ReadOnlyStorage.Fetch(ctx, desc)
}
