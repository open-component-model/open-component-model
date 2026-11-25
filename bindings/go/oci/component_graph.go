package oci

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/oras-project/oras-go/v3/content"
	"github.com/oras-project/oras-go/v3/errdef"
	slogcontext "github.com/veqryn/slog-context"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/internal/log"
	ocidescriptor "ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
	ocistream "ocm.software/open-component-model/bindings/go/oci/stream"
)

// signatureGraph hides access referrers from every walk over the graph. What
// remains reachable from a normalized manifest are its signature referrers.
// Access referrers are regenerated per repository and must not be copied.
//
// It embeds the interface, not a concrete store, so that a
// registry.ReferrerLister of the store is not promoted: ORAS prefers Referrers
// over Predecessors and would bypass the filter.
type signatureGraph struct{ content.ReadOnlyGraphStorage }

// Predecessors returns the referrers of desc without the access referrers.
//
// The artifact type a listing reports decides without a request. Listings
// omit it for an image index that has none, and referrers tag schema indexes
// are written by clients that may omit it, so a manifest or index listed
// without one is fetched. That fetch is not shared with a later copy. Such a
// referrer whose content does not exist is returned undecided.
func (g signatureGraph) Predecessors(ctx context.Context, desc ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error) {
	predecessors, err := g.ReadOnlyGraphStorage.Predecessors(ctx, desc)
	if err != nil {
		return nil, err
	}
	kept := make([]ociImageSpecV1.Descriptor, 0, len(predecessors))
	for _, p := range predecessors {
		artifactType := p.ArtifactType
		if artifactType == "" && declaresArtifactType(p.MediaType) {
			raw, err := content.FetchAll(ctx, g.ReadOnlyGraphStorage, p)
			// CTF stores report missing content as fs.ErrNotExist.
			if errors.Is(err, errdef.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
				// Retention can remove a manifest that is still listed, which
				// must not block a copy. Without content the referrer cannot
				// reach a target, so it is kept for skipMissingReferrers to
				// skip with its warning.
				kept = append(kept, p)
				continue
			}
			if err != nil {
				// A referrer kept without inspection would become a selectable
				// access referrer on the target, so a failed fetch fails the walk.
				return nil, fmt.Errorf("failed to inspect referrer %s of %s: %w", p.Digest, desc.Digest, err)
			}
			header, err := decodeManifestHeader(raw)
			if err != nil {
				return nil, fmt.Errorf("failed to inspect referrer %s of %s: %w", p.Digest, desc.Digest, err)
			}
			artifactType = header.listedArtifactType(p.MediaType)
		}
		if artifactType == ocidescriptor.ArtifactTypeAccessDescriptor {
			continue
		}
		kept = append(kept, p)
	}
	return kept, nil
}

// DownloadComponentVersionGraph returns the OCI graph a component version in the normalized layout
// is stored as, without its access referrers: the normalized manifest as root, and its signature
// referrers. Nothing is downloaded, the returned stream reads from the store of the repository.
// If the repository is pinned to an artifact of component, that artifact is the root instead of
// what the version tag resolves to.
//
// It fails for a component version that is not stored in the normalized layout.
func (repo *Repository) DownloadComponentVersionGraph(ctx context.Context, component, version string) (_ ocistream.ResourceStream, err error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)
	done := log.Operation(ctx, "download component version graph", slog.String("component", component), slog.String("version", version))
	defer func() {
		done(err)
	}()

	root, store, err := repo.storedRoot(ctx, component, version)
	if err != nil {
		return nil, err
	}
	if root.layout != LayoutNormalized {
		return nil, fmt.Errorf("component version %s:%s is not stored in the %s layout, so it has no normalized manifest to transfer with its signature referrers", component, version, LayoutNormalized)
	}

	return &ocistream.OCIResourceStream{
		ReadOnlyGraphStorage: signatureGraph{store},
		Descriptor:           root.descriptor,
		ExtendedCopyOpts:     repo.extendedCopyGraphOptions(),
		TempDir:              repo.tempDir,
		Tags:                 []string{version},
	}, nil
}

// AddComponentVersionFromGraph adds desc like [Repository.AddComponentVersion], but copies the
// normalized manifest with its signature referrers from graph, the graph of that component
// version in its source repository (see [Repository.DownloadComponentVersionGraph]), instead of
// building the manifest. Signatures over the manifest digest, e.g. by cosign, stay valid that way.
// The access referrer is still written by this repository, as it holds accesses of this repository.
//
// The copy fails before writing if desc does not normalize to the normalized manifest of graph, or
// if that manifest does not fit the referrer tracking policy of this repository. The version is
// tagged last, once its signature referrers and its access referrer exist.
//
// A repository that does not write the normalized layout cannot hold the normalized manifest. It
// adds desc with AddComponentVersion and logs that the signature referrers are left behind.
func (repo *Repository) AddComponentVersionFromGraph(ctx context.Context, desc *descriptor.Descriptor, graph ocistream.ResourceStream) (err error) {
	if graph == nil {
		return errors.New("the graph of the component version is required")
	}
	component, version := desc.Component.Name, desc.Component.Version
	if repo.layout != LayoutNormalized {
		repo.logger.WarnContext(ctx, "target repository does not write the normalized layout, signature referrers of the normalized source are not carried",
			slog.String("component", component), slog.String("version", version), slog.String("layout", repo.layout.String()),
			slog.String("hint", "set componentVersionLayout: normalized in the oci.storage.config.ocm.software/v1alpha1 configuration to keep the normalized layout"))
		return repo.AddComponentVersion(ctx, desc)
	}

	ctx = slogcontext.NewCtx(ctx, repo.logger)
	done := log.Operation(ctx, "add component version from graph", slog.String("component", component), slog.String("version", version))
	defer func() {
		done(err)
	}()
	return repo.addComponentVersion(ctx, desc, graph)
}

// rejectAccessReferrers fails for a manifest or index that carries the access
// artifact type, before next is called. A listing can report another artifact
// type than the manifest has, which lets an access referrer pass
// [signatureGraph]. The target registry derives the type from the copied
// manifest, so the copy has to fail instead.
func rejectAccessReferrers(next findSuccessorsFunc) findSuccessorsFunc {
	if next == nil {
		next = content.Successors
	}
	return func(ctx context.Context, fetcher content.Fetcher, desc ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error) {
		if !declaresArtifactType(desc.MediaType) {
			return next(ctx, fetcher, desc)
		}
		// The copy proxy caches this fetch, and content.Successors reads the manifest from that cache.
		raw, err := content.FetchAll(ctx, fetcher, desc)
		if err != nil {
			return nil, err
		}
		header, err := decodeManifestHeader(raw)
		if err != nil {
			return nil, fmt.Errorf("failed to inspect %s: %w", desc.Digest, err)
		}
		if artifactType := header.listedArtifactType(desc.MediaType); artifactType == ocidescriptor.ArtifactTypeAccessDescriptor {
			return nil, fmt.Errorf("access referrer %s must not be copied: its artifact type %q is reserved for the access descriptor each repository generates itself", desc.Digest, artifactType)
		}
		return next(ctx, fetcher, desc)
	}
}

// declaresArtifactType reports whether content of the media type declares the
// artifact type registries list it by, so that it can be listed as an access
// referrer. Besides image manifests and indexes that is the withdrawn OCI
// artifact manifest: the read path never selects one, but it would be listed
// as a candidate on every read of the target.
func declaresArtifactType(mediaType string) bool {
	switch mediaType {
	case ociImageSpecV1.MediaTypeImageManifest, ociImageSpecV1.MediaTypeImageIndex, mediaTypeArtifactManifest:
		return true
	default:
		return false
	}
}
