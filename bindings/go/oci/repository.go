// Package oci provides functionality for storing and retrieving Open Component Model (OCM) components
// using the Open Container Initiative (OCI) registry format. It implements the OCM repository interface
// using OCI registries as the underlying storage mechanism.

package oci

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	slogcontext "github.com/veqryn/slog-context"
	"golang.org/x/sync/errgroup"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ociblob "ocm.software/open-component-model/bindings/go/oci/blob"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	internaldigest "ocm.software/open-component-model/bindings/go/oci/internal/digest"
	"ocm.software/open-component-model/bindings/go/oci/internal/fetch"
	"ocm.software/open-component-model/bindings/go/oci/internal/identity"
	"ocm.software/open-component-model/bindings/go/oci/internal/introspection"
	"ocm.software/open-component-model/bindings/go/oci/internal/lister"
	complister "ocm.software/open-component-model/bindings/go/oci/internal/lister/component"
	"ocm.software/open-component-model/bindings/go/oci/internal/log"
	"ocm.software/open-component-model/bindings/go/oci/internal/pack"
	"ocm.software/open-component-model/bindings/go/oci/internal/validate"
	"ocm.software/open-component-model/bindings/go/oci/looseref"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	accessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	descriptor2 "ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
	indexv1 "ocm.software/open-component-model/bindings/go/oci/spec/index/component/v1"
	ocistream "ocm.software/open-component-model/bindings/go/oci/stream"
	"ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var (
	_            ComponentVersionRepository          = (*Repository)(nil)
	_            repository.OwnershipAwareRepository = (*Repository)(nil)
	versionRegex                                     = regexp.MustCompile(compref.VersionRegex)
)

// Repository implements the ComponentVersionRepository interface using OCI registries.
// Each component may be stored in a separate OCI repository, but ultimately the storage is determined by the Resolver.
//
// This Repository implementation validates local blob references by scanning component descriptors
// for LocalBlob access specifications and ensuring the referenced blobs exist in the OCI store.
// Local blobs uploaded via AddLocalResource must exist before calling AddComponentVersion.
//
// Note: Store implementations are expected to either allow orphaned local resources or
// regularly issue an async garbage collection to remove them due to this behavior.
// This however should not be an issue since all OCI registries implement such a garbage collection mechanism.
type Repository struct {
	scheme *runtime.Scheme

	// resolver resolves component version references to OCI stores.
	resolver Resolver

	// creatorAnnotation is the annotation used to identify the creator of the component version.
	// see OCMCreator for more information.
	creatorAnnotation string

	// ResourceCopyOptions are the options used for copying resources between stores.
	// These options are used in copyResource.
	resourceCopyOptions oras.CopyOptions

	// referrerTrackingPolicy defines how OCI referrers are used to track component versions.
	referrerTrackingPolicy ReferrerTrackingPolicy

	// logger is the logger used for OCI operations.
	logger *slog.Logger

	// the media type of the descriptor encoding used for component versions.
	// this is used to determine the media type of the component descriptor when adding new component versions.
	descriptorEncodingMediaType string

	// unmarshalDescriptorFunc is used to unmarshal descriptors from OCI stores.
	unmarshalDescriptorFunc descriptor2.UnmarshalFunc

	// tempDir is the temporary directory used for OCI buffering operations.
	tempDir string

	// globalAccessPolicy controls whether global access references are added to local blobs.
	// Default (zero value) is Never, suppressing global access to discourage reliance on it.
	globalAccessPolicy GlobalAccessPolicy
}

// SetGlobalAccessPolicy overrides the global access policy for this repository.
func (repo *Repository) SetGlobalAccessPolicy(policy GlobalAccessPolicy) {
	repo.globalAccessPolicy = policy
}

// AddComponentVersion adds a new component version to the repository.
func (repo *Repository) AddComponentVersion(ctx context.Context, descriptor *descriptor.Descriptor) (err error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)
	component, version := descriptor.Component.Name, descriptor.Component.Version
	done := log.Operation(ctx, "add component version", slog.String("component", component), slog.String("version", version))
	defer func() {
		done(err)
	}()

	reference, store, err := repo.getStore(ctx, component, version)
	if err != nil {
		return err
	}

	// Scan component descriptor for local blob references
	localBlobs := scanLocalBlobs(descriptor)

	// Validate that all referenced local blobs exist in the store
	additionalManifests, additionalLayers, err := identifyLocalBlobManifestsAndLayers(ctx, store, localBlobs)
	if err != nil {
		return fmt.Errorf("failed to validate local blobs: %w", err)
	}

	manifest, err := AddDescriptorToStore(ctx, store, descriptor, AddDescriptorOptions{
		Scheme:                        repo.scheme,
		Author:                        repo.creatorAnnotation,
		AdditionalDescriptorManifests: additionalManifests,
		AdditionalLayers:              additionalLayers,
		ReferrerTrackingPolicy:        repo.referrerTrackingPolicy,
		DescriptorEncodingMediaType:   repo.descriptorEncodingMediaType,
	})
	if err != nil {
		return fmt.Errorf("failed to add descriptor to store: %w", err)
	}

	if err := store.Tag(ctx, *manifest, reference); err != nil {
		return fmt.Errorf("failed to tag manifest: %w", err)
	}

	return nil
}

func (repo *Repository) ListComponentVersions(ctx context.Context, component string) (_ []string, err error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)
	done := log.Operation(ctx, "list component versions",
		slog.String("component", component))
	defer func() {
		done(err)
	}()

	_, store, err := repo.getStore(ctx, component, "latest")
	if err != nil {
		return nil, err
	}

	list, err := lister.New(store)
	if err != nil {
		return nil, fmt.Errorf("failed to create lister: %w", err)
	}

	opts := lister.Options{
		SortPolicy: lister.SortPolicyLooseSemverDescending,
		TagListerOptions: lister.TagListerOptions{
			VersionResolver: complister.ReferenceTagVersionResolver(component, store),
		},
		ReferrerListerOptions: lister.ReferrerListerOptions{
			ArtifactType:    descriptor2.MediaTypeComponentDescriptorV2,
			Subject:         indexv1.Descriptor,
			VersionResolver: complister.ReferrerAnnotationVersionResolver(component),
		},
	}

	switch repo.referrerTrackingPolicy {
	case ReferrerTrackingPolicyByIndexAndSubject:
		opts.LookupPolicy = lister.LookupPolicyReferrerWithTagFallback
	case ReferrerTrackingPolicyNone:
		opts.LookupPolicy = lister.LookupPolicyTagOnly
	}

	return list.List(ctx, opts)
}

// CheckHealth checks if the repository is accessible and properly configured.
func (repo *Repository) CheckHealth(ctx context.Context) (err error) {
	return repo.resolver.Ping(slogcontext.NewCtx(ctx, repo.logger))
}

// GetComponentVersion retrieves a component version from the repository.
func (repo *Repository) GetComponentVersion(ctx context.Context, component, version string) (desc *descriptor.Descriptor, err error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)
	done := log.Operation(ctx, "get component version",
		slog.String("component", component),
		slog.String("version", version))
	defer func() {
		done(err)
	}()

	reference, store, err := repo.getStore(ctx, component, version)
	if err != nil {
		return nil, err
	}

	desc, _, _, err = getDescriptorFromStore(ctx, store, reference, repo.unmarshalDescriptorFunc)
	if errors.Is(err, errdef.ErrNotFound) {
		return desc, errors.Join(repository.ErrNotFound, fmt.Errorf("component version %s/%s not found: %w", component, version, err))
	}
	return desc, err
}

// AddLocalResource adds a local resource to the repository. An ownership referrer
// (ADR 0016) that already travels inside the resource's layout is copied across
// with it (transfer). Creating a fresh ownership referrer for an opted-in
// resource is a separate step, driven by the caller via [Repository.AddOwnership]
// once the artifact has been uploaded.
func (repo *Repository) AddLocalResource(
	ctx context.Context,
	component, version string,
	resource *descriptor.Resource,
	b blob.ReadOnlyBlob,
) (_ *descriptor.Resource, err error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)
	done := log.Operation(ctx, "add local resource",
		slog.String("component", component),
		slog.String("version", version),
		log.IdentityLogAttr("resource", resource.ToIdentity()))
	defer func() {
		done(err)
	}()

	resource = resource.DeepCopy()

	if err := repo.uploadAndUpdateLocalArtifact(ctx, component, version, resource, b); err != nil {
		return nil, err
	}

	return resource, nil
}

func (repo *Repository) AddLocalSource(ctx context.Context, component, version string, source *descriptor.Source, content blob.ReadOnlyBlob) (newRes *descriptor.Source, err error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)
	done := log.Operation(ctx, "add local source",
		slog.String("component", component),
		slog.String("version", version),
		log.IdentityLogAttr("source", source.ToIdentity()))
	defer func() {
		done(err)
	}()

	source = source.DeepCopy()

	// Sources never get an ownership referrer; the referrer logic short-circuits
	// for non-resource artifacts anyway.
	if err := repo.uploadAndUpdateLocalArtifact(ctx, component, version, source, content); err != nil {
		return nil, err
	}

	return source, nil
}

func (repo *Repository) ProcessResourceDigest(ctx context.Context, res *descriptor.Resource) (_ *descriptor.Resource, err error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)
	done := log.Operation(ctx, "process resource digest",
		log.IdentityLogAttr("resource", res.ToIdentity()))
	defer func() {
		done(err)
	}()
	res = res.DeepCopy()
	switch typed := res.Access.(type) {
	case *v2.LocalBlob:
		if typed.GlobalAccess == nil {
			return nil, fmt.Errorf("local blob access does not have a global access and cannot be used")
		}
		globalAccess, err := repo.scheme.NewObject(typed.GlobalAccess.GetType())
		if err != nil {
			return nil, fmt.Errorf("error creating typed global blob access with help of scheme: %w", err)
		}
		if err := repo.scheme.Convert(typed.GlobalAccess, globalAccess); err != nil {
			return nil, fmt.Errorf("error converting global blob access: %w", err)
		}
		res.Access = globalAccess
		return repo.ProcessResourceDigest(ctx, res)
	case *accessv1.OCIImage:
		return repo.processOCIImageDigest(ctx, res, typed)
	default:
		return nil, fmt.Errorf("unsupported resource access type: %T", typed)
	}
}

func (repo *Repository) processOCIImageDigest(ctx context.Context, res *descriptor.Resource, typed *accessv1.OCIImage) (*descriptor.Resource, error) {
	src, err := repo.resolver.StoreForReference(ctx, typed.ImageReference)
	if err != nil {
		return nil, err
	}

	resolved, err := looseref.ParseReference(typed.ImageReference)
	if err != nil {
		return nil, fmt.Errorf("error parsing image reference %q: %w", typed.ImageReference, err)
	}

	var pinnedDigest digest.Digest
	if dig, err := resolved.Digest(); err == nil {
		pinnedDigest = dig
	}

	var desc ociImageSpecV1.Descriptor
	if pinnedDigest.String() == "" {
		if desc, err = src.Resolve(ctx, resolved.ReferenceOrTag()); err != nil {
			return nil, fmt.Errorf("failed to resolve reference to process digest %q: %w", typed.ImageReference, err)
		}
	} else {
		if desc, err = src.Resolve(ctx, pinnedDigest.String()); err != nil {
			return nil, fmt.Errorf("failed to verify existence of pinned digest %q (derived from %q): %w", pinnedDigest, resolved, err)
		}
	}

	// if the resource did not have a digest, we apply the digest from the descriptor
	// if it did, we verify it against the received descriptor.
	if res.Digest == nil {
		res.Digest = &descriptor.Digest{}
		if err := internaldigest.Apply(res.Digest, desc.Digest); err != nil {
			return nil, fmt.Errorf("failed to apply digest to resource: %w", err)
		}
	} else if err := internaldigest.Verify(res.Digest, desc.Digest); err != nil {
		return nil, fmt.Errorf("failed to verify digest of resource %q: %w", res.ToIdentity(), err)
	}

	if pinnedDigest != "" && pinnedDigest.String() != desc.Digest.String() {
		return nil, fmt.Errorf("expected pinned digest %q (derived from %q) but got %q", pinnedDigest, resolved, desc.Digest)
	}

	resolved.Reference.Reference = desc.Digest.String()

	// in any case, after successful processing, we can pin the access
	typed.ImageReference = resolved.String()
	res.Access = typed

	return res, nil
}

// scanLocalBlobs scans the component descriptor for all LocalBlob access specifications and returns them
func scanLocalBlobs(desc *descriptor.Descriptor) []descriptor.Artifact {
	var artifacts []descriptor.Artifact
	// the conversion is either a noop if its already a local blob,
	// or it will convert the access and then update it in place if successful.
	// this is on purpose, and is expected for example when external plugins
	// call this function with lazily serialized access specs.
	//
	// Scan resources for LocalBlob access specs
	for i := range desc.Component.Resources {
		resource := &desc.Component.Resources[i]
		var lb v2.LocalBlob
		if err := v2.Scheme.Convert(resource.Access, &lb); err == nil {
			resource.Access = &lb
			artifacts = append(artifacts, resource)
		}
	}

	// Scan sources for LocalBlob access specs
	for i := range desc.Component.Sources {
		source := &desc.Component.Sources[i]
		var lb v2.LocalBlob
		if err := v2.Scheme.Convert(source.Access, &lb); err == nil {
			source.Access = &lb
			artifacts = append(artifacts, source)
		}
	}

	return artifacts
}

// identifyLocalBlobManifestsAndLayers fetches all descriptors in the store that are available
// for the given artifact list and the output is stable sorted based on the order of the artifact list.
func identifyLocalBlobManifestsAndLayers(ctx context.Context, store oras.Target, artifacts []descriptor.Artifact) (manifests []ociImageSpecV1.Descriptor, layers []ociImageSpecV1.Descriptor, err error) {
	eg, egctx := errgroup.WithContext(ctx)

	// Pre-allocate result slice to maintain order
	results := make([]ociImageSpecV1.Descriptor, len(artifacts))

	for idx, artifact := range artifacts {
		eg.Go(func() error {
			localBlob := artifact.GetAccess().(*v2.LocalBlob)
			// resolution of a blob will always cause a octet stream media type as its just a blob.
			// if we would use Exists(), then we would need to store the size in the local blob spec
			// but since we dont do that we have to take actual uploaded size of the descriptor
			// from the API again. Thats why we need to call Resolve and get the descriptor
			// instead of just checking existence of the blob.
			resolve := store.Resolve
			if !introspection.IsOCICompliantMediaType(localBlob.MediaType) {
				if bs, ok := store.(interface{ Blobs() registry.BlobStore }); ok {
					//  TODO(jakobmoellerdev): currently, the blobs store is required
					//    because the oras remote repo always hardcodes resolves against manifests
					//    however we really want to resolve against the blob store here for non
					//    manifest blobs. Mid-Term the oras.Target interface is insufficient
					//    and CTFs also need to implement this BlobStore and then we can drop this
					//    assert.
					resolve = bs.Blobs().Resolve
				}
			}

			desc, err := resolve(egctx, localBlob.LocalReference)
			if err != nil {
				return fmt.Errorf("failed to resolve descriptor for local blob %s: %w", localBlob.LocalReference, err)
			}
			// a resolved blob will always have the octet stream media type, which needs
			// to be overwritten with the media type from the local blob if its present.
			if localBlob.MediaType != "" {
				desc.MediaType = localBlob.MediaType
			}
			if err := identity.Adopt(&desc, artifact); err != nil {
				return fmt.Errorf("failed to adopt descriptor for local blob %s: %w", localBlob.LocalReference, err)
			}

			// Store result at the correct index
			results[idx] = desc
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, nil, fmt.Errorf("failed to validate existence of local blobs: %w", err)
	}

	// Process results in order to maintain stable ordering based on input artifacts
	for _, desc := range results {
		// Categorize descriptor based on whether it's an OCI-compliant manifest or a layer
		if introspection.IsOCICompliantManifest(desc) {
			manifests = append(manifests, desc)
		} else {
			layers = append(layers, desc)
		}
	}

	return manifests, layers, nil
}

func (repo *Repository) uploadAndUpdateLocalArtifact(
	ctx context.Context,
	component string,
	version string,
	artifact descriptor.Artifact,
	b blob.ReadOnlyBlob,
) error {
	reference, store, err := repo.getStore(ctx, component, version)
	if err != nil {
		return err
	}

	if err := ociblob.UpdateArtifactWithInformationFromBlob(artifact, b); err != nil {
		return fmt.Errorf("failed to update artifact with data from blob: %w", err)
	}

	artifactBlob, err := ociblob.NewArtifactBlob(artifact, b)
	if err != nil {
		return fmt.Errorf("failed to create resource blob: %w", err)
	}

	packOptions := pack.Options{
		AccessScheme:       repo.scheme,
		CopyGraphOptions:   repo.resourceCopyOptions.CopyGraphOptions,
		BaseReference:      reference,
		GlobalAccessPolicy: repo.globalAccessPolicy,
	}
	configureReferrerCopy(&packOptions, artifact)
	_, err = pack.ArtifactBlob(ctx, store, artifactBlob, packOptions)
	if err != nil {
		return fmt.Errorf("failed to pack resource blob: %w", err)
	}

	return nil
}

// configureReferrerCopy requests that the pack layer copy through every referrer
// that already travels inside an artifact's incoming layout (transfer) — any
// manifest declaring a subject, e.g. an ADR-0016 ownership referrer — so links
// the source attached survive the upload. Creating a fresh ownership referrer for
// an opted-in resource is a separate, caller-driven step ([Repository.AddOwnership]);
// this only handles copy-through. Sources are not resources, so they get nothing.
func configureReferrerCopy(packOptions *pack.Options, artifact descriptor.Artifact) {
	if _, isResource := artifact.(*descriptor.Resource); !isResource {
		return
	}
	packOptions.Referrer = tar.ReferrerSource{CopyExisting: true}
}

// GetLocalResource retrieves a local resource from the repository.
func (repo *Repository) GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)
	var err error
	done := log.Operation(ctx, "get local resource",
		slog.String("component", component),
		slog.String("version", version),
		log.IdentityLogAttr("resource", identity))
	defer func() {
		done(err)
	}()

	var b fetch.LocalBlob
	var artifact descriptor.Artifact
	if b, artifact, err = repo.localArtifact(ctx, component, version, identity, annotations.ArtifactKindResource); err != nil {
		if errors.Is(err, errdef.ErrNotFound) {
			return nil, nil, errors.Join(repository.ErrNotFound, fmt.Errorf("component version %s/%s not found: %w", component, version, err))
		}
		return nil, nil, err
	}
	return b, artifact.(*descriptor.Resource), nil
}

func (repo *Repository) GetLocalSource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Source, error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)
	var err error
	done := log.Operation(ctx, "get local source",
		slog.String("component", component),
		slog.String("version", version),
		log.IdentityLogAttr("resource", identity))
	defer func() {
		done(err)
	}()

	var b fetch.LocalBlob
	var artifact descriptor.Artifact
	if b, artifact, err = repo.localArtifact(ctx, component, version, identity, annotations.ArtifactKindSource); err != nil {
		if errors.Is(err, errdef.ErrNotFound) {
			return nil, nil, errors.Join(repository.ErrNotFound, fmt.Errorf("component version %s/%s not found: %w", component, version, err))
		}
		return nil, nil, err
	}
	return b, artifact.(*descriptor.Source), nil
}

func (repo *Repository) localArtifact(ctx context.Context, component, version string, identity runtime.Identity, kind annotations.ArtifactKind) (fetch.LocalBlob, descriptor.Artifact, error) {
	reference, store, err := repo.getStore(ctx, component, version)
	if err != nil {
		return nil, nil, err
	}
	desc, manifest, index, err := getDescriptorFromStore(ctx, store, reference, repo.unmarshalDescriptorFunc)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get component version: %w", err)
	}

	var candidates []descriptor.Artifact
	switch kind {
	case annotations.ArtifactKindResource:
		for _, res := range desc.Component.Resources {
			if identity.Match(res.ToIdentity(), runtime.IdentityMatchingChainFn(runtime.IdentitySubset)) {
				candidates = append(candidates, &res)
			}
		}
	case annotations.ArtifactKindSource:
		for _, src := range desc.Component.Sources {
			if identity.Match(src.ToIdentity(), runtime.IdentityMatchingChainFn(runtime.IdentitySubset)) {
				candidates = append(candidates, &src)
			}
		}
	}
	if len(candidates) != 1 {
		return nil, nil, fmt.Errorf("found %d candidates while looking for %s %q, but expected exactly one", len(candidates), kind, identity)
	}
	artifact := candidates[0]
	meta := artifact.GetElementMeta()

	// now that we have a unique candidate, we should use its identity instead of the one requested, as
	// the requested identity might not be fully qualified.
	// For example, it is valid to ask for "name=abc", but receive an artifact with "name=abc,version=1.0.0".
	slogcontext.Debug(ctx, "found artifact in descriptor", "artifact", meta.ToIdentity())

	access := artifact.GetAccess()
	typed, err := repo.scheme.NewObject(access.GetType())
	if err != nil {
		return nil, nil, fmt.Errorf("error creating resource access: %w", err)
	}
	if err := repo.scheme.Convert(access, typed); err != nil {
		return nil, nil, fmt.Errorf("error converting resource access: %w", err)
	}

	switch typed := typed.(type) {
	case *v2.LocalBlob:
		b, err := repo.getLocalBlobFromIndexOrManifest(
			ctx, store, index, manifest, typed.LocalReference,
			artifact.GetElementMeta().Version,
		)
		return b, artifact, err
	default:
		return nil, nil, fmt.Errorf("unsupported resource access type: %T", typed)
	}
}

// getLocalBlobFromIndexOrManifest resolves and fetches a blob from either an
// OCI index or a manifest. It looks up the descriptor matching the given
// reference and then:
func (repo *Repository) getLocalBlobFromIndexOrManifest(
	ctx context.Context,
	store spec.Store,
	index *ociImageSpecV1.Index,
	manifest *ociImageSpecV1.Manifest,
	ref, version string,
) (LocalBlob, error) {
	descriptors := collectDescriptors(index, manifest)

	artifact, err := findDescriptorFromReference(descriptors, ref)
	if err != nil {
		return nil, fmt.Errorf("resolve artifact %q: %w", ref, err)
	}

	// Nested manifest: copy full OCI layout
	if index != nil && introspection.IsOCICompliantManifest(artifact) {
		// copy the full OCI manifest and its dependency graph
		// into an in-memory OCI layout. This is used when the descriptor refers
		// to another OCI-compliant manifest instead of a single layer.
		//
		// Pull any ownership referrers (ADR 0016) of the artifact into the layout
		// as well, so they travel with a by-value resource just as they do for
		// the OCI-image path (see [Repository.DownloadResourceStream]). Only the
		// main artifact is tagged, so a later re-add still resolves it as the
		// top-level parent even with the referrer present in the index.
		refs, err := lookupOwnershipReferrers(ctx, store, artifact)
		if err != nil {
			// A referrers-query hiccup must not fail an otherwise-healthy read;
			// continue without them.
			slogcontext.Log(ctx, slog.LevelWarn, "failed listing ownership referrers; continuing without them", slog.String("reference", ref), slog.Any("err", err))
		}
		return tar.CopyToOCILayoutInMemory(ctx, store, artifact, tar.CopyToOCILayoutOptions{
			CopyGraphOptions: repo.resourceCopyOptions.CopyGraphOptions,
			Tags:             []string{version},
			TempDir:          repo.tempDir,
			Referrers:        refs,
		})
	}

	// Fetch a single layer blob from the store and verify
	// that its digest matches the expected descriptor digest. This path is used
	// when the reference is a raw layer rather than a manifest.
	data, err := store.Fetch(ctx, artifact)
	if err != nil {
		return nil, fmt.Errorf("fetch layer: %w", err)
	}
	// data cannot be closed, as it is used by the blob
	b := ociblob.NewDescriptorBlob(data, artifact)
	if actual, _ := b.Digest(); actual != artifact.Digest.String() {
		return nil, fmt.Errorf("digest mismatch: expected %q, got %q", artifact.Digest, actual)
	}
	return b, nil
}

func (repo *Repository) getStore(ctx context.Context, component string, version string) (ref string, store spec.Store, err error) {
	reference := repo.resolver.ComponentVersionReference(ctx, component, version)
	if store, err = repo.resolver.StoreForReference(ctx, reference); err != nil {
		return "", nil, fmt.Errorf("failed to get store for reference: %w", err)
	}
	return reference, store, nil
}

// UploadResource uploads a [*descriptor.Resource] to the repository.
func (repo *Repository) UploadResource(ctx context.Context, res *descriptor.Resource, b blob.ReadOnlyBlob) (newRes *descriptor.Resource, err error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)
	done := log.Operation(ctx, "upload resource", log.IdentityLogAttr("resource", res.ToIdentity()))
	defer func() {
		done(err)
	}()

	res = res.DeepCopy()

	// Always carry across any referrer that already travels in the resource's
	// layout (e.g. an ADR 0016 ownership referrer): transfer copies the existing
	// referrers, while a fresh upload simply has none to copy.
	desc, access, err := repo.uploadOCIImage(ctx, res.Access, b, true)
	if err != nil {
		return nil, fmt.Errorf("failed to upload resource as OCI image: %w", err)
	}

	if res.Digest == nil {
		res.Digest = &descriptor.Digest{}
	}
	if err := internaldigest.Apply(res.Digest, desc.Digest); err != nil {
		return nil, fmt.Errorf("failed to apply digest to resource: %w", err)
	}
	res.Access = access

	return res, nil
}

// UploadSource uploads a [*descriptor.Source] to the repository.
func (repo *Repository) UploadSource(ctx context.Context, src *descriptor.Source, b blob.ReadOnlyBlob) (newSrc *descriptor.Source, err error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)
	done := log.Operation(ctx, "upload source", log.IdentityLogAttr("source", src.ToIdentity()))
	defer func() {
		done(err)
	}()

	src = src.DeepCopy()

	// Sources never carry referrers (ADR 0016 ownership applies to resources, not
	// sources), so never copy them here.
	_, access, err := repo.uploadOCIImage(ctx, src.Access, b, false)
	if err != nil {
		return nil, fmt.Errorf("failed to upload source as OCI image: %w", err)
	}
	src.Access = access

	return src, nil
}

func (repo *Repository) uploadOCIImage(ctx context.Context, newAccess runtime.Typed, b blob.ReadOnlyBlob, copyReferrers bool) (_ ociImageSpecV1.Descriptor, _ *accessv1.OCIImage, err error) {
	var access accessv1.OCIImage
	if err := repo.scheme.Convert(newAccess, &access); err != nil {
		return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("error converting resource target to OCI image: %w", err)
	}

	store, err := repo.resolver.StoreForReference(ctx, access.ImageReference)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, nil, err
	}

	ociStore, err := tar.ReadOCILayout(ctx, b)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("failed to read OCI layout: %w", err)
	}
	defer func() {
		err = errors.Join(err, ociStore.Close())
	}()

	// A referrer is tagged by digest when written into a layout, so it appears in
	// the index. Split the index into the main artifacts and the referrers
	// (manifests declaring a subject): the referrers are injected as extra
	// successors of the main in the copy below (only when copyReferrers), and
	// holding them aside keeps main selection from tripping over them.
	mainArtifacts, referrers := ociStore.MainArtifactsAndReferrers(ctx)
	if len(mainArtifacts) != 1 {
		return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("expected exactly one main artifact in OCI layout, but got %d", len(mainArtifacts))
	}
	main := mainArtifacts[0]

	ref, err := looseref.ParseReference(access.ImageReference)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("failed to parse target access image reference %q: %w", access.ImageReference, err)
	}
	if err := ref.ValidateReferenceAsTag(); err != nil {
		return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("can only copy %q if it is tagged: %w", access.ImageReference, err)
	}

	// Copy the main artifact. When the resource's layout carries referrers, copy
	// them in the same traversal: their subject edge points back at main, which
	// CopyGraph's default successor resolution does not follow, so inject them as
	// extra successors of main (see [tar.InjectReferrersAsSuccessors]).
	copyOpts := repo.resourceCopyOptions.CopyGraphOptions
	if copyReferrers && len(referrers) > 0 {
		copyOpts.FindSuccessors = tar.InjectReferrersAsSuccessors(main, referrers)
	}

	if err := oras.CopyGraph(ctx, ociStore, store, main, copyOpts); err != nil {
		return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("failed to upload resource via copy: %w", err)
	}

	if err := store.Tag(ctx, main, ref.Tag); err != nil {
		return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("failed to tag main artifact with tag %q: %w", ref.Tag, err)
	}

	return main, &access, nil
}

// AddOwnership attaches an ownership referrer (ADR 0016) to
// resource and pushes it, linking the resource back to the owning component
// version. It implements the constructor's [OwnershipAwareRepository] capability.
//
// The subject the referrer points at and the store it is pushed into are derived
// from the resource's access type (see [Repository.resolveOwnershipSubject]):
//
//   - a by-value [v2.LocalBlob] just uploaded into this repository's own component
//     store is resolved by digest there;
//   - a by-reference [accessv1.OCIImage] kept in its hosting registry is resolved
//     by image reference there. That image is left byte-for-byte unchanged — only
//     the new referrer manifest (and the shared empty config/layer blob) is pushed,
//     so OCI-level signatures over the image stay valid.
//
// The referrer is content-addressed and deterministic, so re-running converges on
// the same digest and the registry deduplicates it. It is a no-op when the subject
// is not an OCI manifest (raw blobs get no referrer).
//
// The opt-in decision (options.ownershipPolicy: Always) is made by the caller — the
// constructor gates on the runtime resource options at its call site
// (processResource) — so this method unconditionally builds the referrer for the
// resource it is handed. credentials are unused: every store is resolved through
// this repository's own resolver, already carrying credentials.
func (repo *Repository) AddOwnership(ctx context.Context, component, version string, resource *descriptor.Resource, _ runtime.Typed) (err error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)
	done := log.Operation(ctx, "add ownership referrer",
		slog.String("component", component),
		slog.String("version", version),
		log.IdentityLogAttr("resource", resource.ToIdentity()))
	defer func() {
		done(err)
	}()

	store, subject, err := repo.resolveOwnershipSubject(ctx, component, version, resource)
	if err != nil {
		return err
	}
	return repo.buildAndPushOwnershipReferrer(ctx, store, subject, resource, component, version)
}

// resolveOwnershipSubject locates the manifest an ownership referrer (ADR 0016)
// points at and the store the referrer is pushed into, dispatching on the
// resource's access type:
//
//   - by-value ([v2.LocalBlob]): the uploaded local resource's access is a local
//     blob whose reference is the digest of its just-pushed manifest in this
//     repository's own component store; that manifest is the subject. It is resolved
//     directly by digest rather than via the component-version descriptor, which is
//     not written until AddComponentVersion — i.e. after the constructor has already
//     attached ownership for each resource.
//   - by-reference ([accessv1.OCIImage]): the resource stays in its hosting
//     registry; the referrer is pushed there with the referenced image as subject.
func (repo *Repository) resolveOwnershipSubject(ctx context.Context, component, version string, resource *descriptor.Resource) (spec.Store, ociImageSpecV1.Descriptor, error) {
	if v2.IsLocalBlob(resource.Access) {
		var localBlob v2.LocalBlob
		if err := repo.scheme.Convert(resource.Access, &localBlob); err != nil {
			return nil, ociImageSpecV1.Descriptor{}, fmt.Errorf("by-value ownership referrer requires a local blob access: %w", err)
		}
		_, store, err := repo.getStore(ctx, component, version)
		if err != nil {
			return nil, ociImageSpecV1.Descriptor{}, err
		}
		subject, err := store.Resolve(ctx, localBlob.LocalReference)
		if err != nil {
			return nil, ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to resolve uploaded artifact %q for ownership referrer: %w", localBlob.LocalReference, err)
		}
		return store, subject, nil
	}

	var access accessv1.OCIImage
	if err := repo.scheme.Convert(resource.Access, &access); err != nil {
		return nil, ociImageSpecV1.Descriptor{}, fmt.Errorf("error converting resource access to OCI image for ownership referrer: %w", err)
	}
	store, err := repo.resolver.StoreForReference(ctx, access.ImageReference)
	if err != nil {
		return nil, ociImageSpecV1.Descriptor{}, err
	}
	ref, err := looseref.ParseReference(access.ImageReference)
	if err != nil {
		return nil, ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to parse image reference %q: %w", access.ImageReference, err)
	}
	subject, err := store.Resolve(ctx, ref.ReferenceOrTag())
	if err != nil {
		return nil, ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to resolve subject %q for ownership referrer: %w", access.ImageReference, err)
	}
	return store, subject, nil
}

// buildAndPushOwnershipReferrer builds the ownership referrer (ADR 0016) linking
// subject back to the owning component version and pushes it into store. It is a
// no-op when subject is not an OCI manifest (raw blobs get no referrer).
func (repo *Repository) buildAndPushOwnershipReferrer(ctx context.Context, store spec.Store, subject ociImageSpecV1.Descriptor, resource *descriptor.Resource, component, version string) error {
	referrers, err := pack.OwnershipReferrer(resource, component, version)(ctx, subject)
	if err != nil {
		return fmt.Errorf("failed to build ownership referrer: %w", err)
	}
	// OwnershipReferrer skips non-manifest subjects (raw blobs get no referrer).
	if len(referrers) == 0 {
		slogcontext.Log(ctx, slog.LevelDebug, "subject is not an OCI manifest; skipping ownership referrer", log.DescriptorLogAttr(subject))
		return nil
	}
	// OCI registries reject a manifest that references blobs not yet present
	// (MANIFEST_BLOB_UNKNOWN), so push the referrer's referenced content — its
	// empty config/layer blob — before the referrer manifest itself. Collect the
	// manifests, push the blobs, then push the manifests; this stays correct
	// regardless of the order pack returns them in.
	var manifests []tar.Referrer
	for _, r := range referrers {
		if introspection.IsOCICompliantManifest(r.Descriptor) {
			manifests = append(manifests, r)
			continue
		}
		if err := store.Push(ctx, r.Descriptor, bytes.NewReader(r.Raw)); err != nil && !errors.Is(err, errdef.ErrAlreadyExists) {
			return fmt.Errorf("failed to push ownership referrer blob %s: %w", r.Descriptor.Digest, err)
		}
	}
	for _, r := range manifests {
		if err := store.Push(ctx, r.Descriptor, bytes.NewReader(r.Raw)); err != nil && !errors.Is(err, errdef.ErrAlreadyExists) {
			return fmt.Errorf("failed to push ownership referrer %s: %w", r.Descriptor.Digest, err)
		}
	}
	return nil
}

// DownloadResource downloads a [*descriptor.Resource] from the repository.
func (repo *Repository) DownloadResource(ctx context.Context, res *descriptor.Resource) (data blob.ReadOnlyBlob, err error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)
	done := log.Operation(ctx, "download resource", log.IdentityLogAttr("resource", res.ToIdentity()))
	defer func() {
		done(err)
	}()

	if res.Access.GetType().IsEmpty() {
		return nil, fmt.Errorf("resource access type is empty")
	}
	// DownloadResourceStream attaches any ownership referrers (ADR 0016) to the
	// stream for locally-owned resources; Materialize then pulls them into the
	// layout so a downstream upload can transfer them with the artifact.
	stream, err := repo.DownloadResourceStream(ctx, res)
	if err != nil {
		return nil, err
	}
	return stream.Materialize(ctx)
}

// lookupOwnershipReferrers lists the ownership referrers (ADR 0016) of subject
// in store. It returns nil (no error) when store cannot answer referrer
// queries, leaving the caller to proceed without them.
func lookupOwnershipReferrers(ctx context.Context, store content.ReadOnlyStorage, subject ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error) {
	graphStore, ok := store.(content.ReadOnlyGraphStorage)
	if !ok {
		slogcontext.Log(ctx, slog.LevelDebug, "source store does not support referrer discovery; skipping ownership referrers", slog.String("store", fmt.Sprintf("%T", store)))
		return nil, nil
	}
	return registry.Referrers(ctx, graphStore, subject, annotations.OwnershipArtifactType)
}

// DownloadSource downloads a [*descriptor.Source] from the repository.
func (repo *Repository) DownloadSource(ctx context.Context, src *descriptor.Source) (data blob.ReadOnlyBlob, err error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)
	done := log.Operation(ctx, "download source", log.IdentityLogAttr("resource", src.ToIdentity()))
	defer func() {
		done(err)
	}()

	if src.Access.GetType().IsEmpty() {
		return nil, fmt.Errorf("source access type is empty")
	}
	return repo.download(ctx, src.Access)
}

// download downloads an artifact specified by an access from the repository into a blob.ReadOnlyBlob.
// It delegates to downloadStream and materializes the result into a tar-based OCI layout blob.
func (repo *Repository) download(ctx context.Context, access runtime.Typed) (data blob.ReadOnlyBlob, err error) {
	stream, err := repo.downloadStream(ctx, access)
	if err != nil {
		return nil, err
	}
	return stream.Materialize(ctx)
}

// getDescriptorOCIImageManifest retrieves the manifest for a given reference from the store.
// It handles both OCI image indexes and OCI image manifests.
func getDescriptorOCIImageManifest(ctx context.Context, store spec.Store, reference string) (manifest ociImageSpecV1.Manifest, index *ociImageSpecV1.Index, err error) {
	slogcontext.Log(ctx, slog.LevelDebug, "resolving descriptor", slog.String("reference", reference))
	base, err := store.Resolve(ctx, reference)
	if err != nil {
		return ociImageSpecV1.Manifest{}, nil, fmt.Errorf("failed to resolve reference %q: %w", reference, err)
	}
	slogcontext.Log(ctx, slog.LevelDebug, "fetching descriptor", log.DescriptorLogAttr(base))
	manifestRaw, err := store.Fetch(ctx, ociImageSpecV1.Descriptor{
		MediaType: base.MediaType,
		Digest:    base.Digest,
		Size:      base.Size,
	})
	if err != nil {
		return ociImageSpecV1.Manifest{}, nil, err
	}
	defer func() {
		err = errors.Join(err, manifestRaw.Close())
	}()

	switch base.MediaType {
	case ociImageSpecV1.MediaTypeImageIndex:
		if err := json.NewDecoder(manifestRaw).Decode(&index); err != nil {
			return ociImageSpecV1.Manifest{}, nil, err
		}
		if len(index.Manifests) == 0 {
			return ociImageSpecV1.Manifest{}, nil, fmt.Errorf("index has no manifests")
		}
		descriptorManifest := index.Manifests[0]
		if descriptorManifest.MediaType != ociImageSpecV1.MediaTypeImageManifest {
			return ociImageSpecV1.Manifest{}, nil, fmt.Errorf("index manifest is not an OCI image manifest")
		}
		indexManifestRaw, err := store.Fetch(ctx, descriptorManifest)
		defer func() {
			err = errors.Join(err, indexManifestRaw.Close())
		}()
		if err != nil {
			return ociImageSpecV1.Manifest{}, nil, fmt.Errorf("failed to fetch manifest: %w", err)
		}
		if err := json.NewDecoder(indexManifestRaw).Decode(&manifest); err != nil {
			return ociImageSpecV1.Manifest{}, nil, err
		}
	case ociImageSpecV1.MediaTypeImageManifest:
		if err := json.NewDecoder(manifestRaw).Decode(&manifest); err != nil {
			return ociImageSpecV1.Manifest{}, nil, err
		}
	default:
		return ociImageSpecV1.Manifest{}, nil, fmt.Errorf("unsupported media type %q", base.MediaType)
	}

	return manifest, index, nil
}

func collectDescriptors(index *ociImageSpecV1.Index, manifest *ociImageSpecV1.Manifest) []ociImageSpecV1.Descriptor {
	if index == nil {
		return manifest.Layers
	}
	descs := make([]ociImageSpecV1.Descriptor, 0, len(index.Manifests)+len(manifest.Layers))
	descs = append(descs, index.Manifests...)
	descs = append(descs, manifest.Layers...)
	return descs
}

func findDescriptorFromReference(descriptors []ociImageSpecV1.Descriptor, reference string) (ociImageSpecV1.Descriptor, error) {
	asDigest := digest.Digest(reference)
	if err := asDigest.Validate(); err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to validate reference %q as digest: %w", reference, err)
	}

	for _, desc := range descriptors {
		if desc.Digest == asDigest {
			return desc, nil
		}
	}
	return ociImageSpecV1.Descriptor{}, fmt.Errorf("no matching descriptor found for reference %s", reference)
}

func (repo *Repository) AddComponentVersionAlias(ctx context.Context, component, versionOrAlias, alias string) (err error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)
	done := log.Operation(ctx, "add component version alias",
		slog.String("component", component),
		slog.String("versionOrAlias", versionOrAlias),
		slog.String("alias", alias))
	defer func() {
		done(err)
	}()

	if versionRegex.MatchString(alias) {
		return fmt.Errorf("alias %q uses semantic version format and cannot be used as an alias (use non-semver names like 'edge' or 'latest' instead)", alias)
	}

	reference, store, err := repo.getStore(ctx, component, versionOrAlias)
	if err != nil {
		return fmt.Errorf("failed to get store for component version %s/%s: %w", component, versionOrAlias, err)
	}

	base, err := store.Resolve(ctx, reference)
	if err != nil {
		if errors.Is(err, errdef.ErrNotFound) {
			return errors.Join(repository.ErrNotFound,
				fmt.Errorf("component version %s/%s not found: %w", component, versionOrAlias, err))
		}
		return fmt.Errorf("failed to resolve component version %s/%s: %w", component, versionOrAlias, err)
	}

	if _, err := validate.ComponentVersionDescriptor(ctx, store, base, component, reference); err != nil {
		return fmt.Errorf("reference %q does not point to a valid OCM component version: %w", reference, err)
	}

	if err := store.Tag(ctx, base, alias); err != nil {
		return fmt.Errorf("failed to tag component version %s/%s with alias %q: %w",
			component, versionOrAlias, alias, err)
	}

	return nil
}

// DownloadResourceStream returns a lazy ResourceStream for the given resource.
// No data is downloaded — content streams on demand via Fetch calls.
func (repo *Repository) DownloadResourceStream(ctx context.Context, res *descriptor.Resource) (ocistream.ResourceStream, error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)
	if res.Access.GetType().IsEmpty() {
		return nil, fmt.Errorf("resource access type is empty")
	}
	stream, err := repo.downloadStream(ctx, res.Access)
	if err != nil {
		return nil, err
	}

	// TODO(ocm-project#1025): ownership referrers (ADR 0016) are intentionally NOT
	// fetched here, which is asymmetric with getLocalBlobFromIndexOrManifest (the
	// by-value path), which does pull them. DownloadResource is also used during
	// by-value construction (constructor.processResourceByValue), where pulling an
	// external image's pre-existing referrers into the downloaded layout would break
	// the subsequent AddLocalResource pack. By-value resources carry their referrers
	// via the local-blob path instead. Resolving the asymmetry needs a properly
	// scoped trigger that distinguishes transfer-of-owned-artifact from
	// ingest-of-external-image before this path can fetch them.
	return stream, nil
}

func (repo *Repository) downloadStream(ctx context.Context, access runtime.Typed) (ocistream.ResourceStream, error) {
	typed, err := repo.scheme.NewObject(access.GetType())
	if err != nil {
		return nil, fmt.Errorf("error creating resource access: %w", err)
	}
	if err := repo.scheme.Convert(access, typed); err != nil {
		return nil, fmt.Errorf("error converting resource access: %w", err)
	}

	switch typed := typed.(type) {
	case *v2.LocalBlob:
		if typed.GlobalAccess == nil {
			return nil, fmt.Errorf("local blob access does not have a global access and cannot be used")
		}
		globalAccess, err := repo.scheme.NewObject(typed.GlobalAccess.GetType())
		if err != nil {
			return nil, fmt.Errorf("error creating typed global blob access with help of scheme: %w", err)
		}
		if err := repo.scheme.Convert(typed.GlobalAccess, globalAccess); err != nil {
			return nil, fmt.Errorf("error converting global blob access: %w", err)
		}
		return repo.downloadStream(ctx, globalAccess)
	case *accessv1.OCIImage:
		src, err := repo.resolver.StoreForReference(ctx, typed.ImageReference)
		if err != nil {
			return nil, err
		}

		resolved, err := looseref.ParseReference(typed.ImageReference)
		if err != nil {
			return nil, fmt.Errorf("error parsing image reference %q: %w", typed.ImageReference, err)
		}

		reference := resolved.ReferenceOrTag()

		desc, err := src.Resolve(ctx, reference)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve reference %q: %w", typed.ImageReference, err)
		}

		var tags []string
		if resolved.Tag != "" {
			tags = []string{typed.ImageReference}
		}
		return &ocistream.OCIResourceStream{
			ReadOnlyStorage: src,
			Descriptor:      desc,
			CopyOpts:        repo.resourceCopyOptions.CopyGraphOptions,
			TempDir:         repo.tempDir,
			Tags:            tags,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported resource access type: %T", typed)
	}
}

// UploadResourceStream streams content from a ResourceStream directly into the repository
// using oras.CopyGraph. No tar materialization occurs.
func (repo *Repository) UploadResourceStream(ctx context.Context, res *descriptor.Resource, rs ocistream.ResourceStream) (*descriptor.Resource, error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)

	var access accessv1.OCIImage
	if err := repo.scheme.Convert(res.Access, &access); err != nil {
		return nil, fmt.Errorf("error converting resource target to OCI image: %w", err)
	}

	ref, err := looseref.ParseReference(access.ImageReference)
	if err != nil {
		return nil, fmt.Errorf("failed to parse target access image reference %q: %w", access.ImageReference, err)
	}

	store, err := repo.resolver.StoreForReference(ctx, access.ImageReference)
	if err != nil {
		return nil, err
	}

	if err := oras.CopyGraph(ctx, rs, store, rs.Root(), repo.resourceCopyOptions.CopyGraphOptions); err != nil {
		return nil, fmt.Errorf("failed to stream resource via copy: %w", err)
	}

	if ref.Tag != "" {
		// if we have a tag, we should also tag in the upload
		if err := store.Tag(ctx, rs.Root(), ref.Tag); err != nil {
			return nil, fmt.Errorf("failed to tag artifact with tag %q: %w", ref.Tag, err)
		}
	}

	res = res.DeepCopy()
	if res.Digest == nil {
		res.Digest = &descriptor.Digest{}
	}
	if err := internaldigest.Apply(res.Digest, rs.Root().Digest); err != nil {
		return nil, fmt.Errorf("failed to apply digest to resource: %w", err)
	}

	// if we don't have a pinned access we can pin it now.
	if ref.Reference.Reference == "" {
		ref.Reference.Reference = rs.Root().Digest.String()
	}

	access.ImageReference = ref.String()
	res.Access = &access

	return res, nil
}
