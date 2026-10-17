package transformer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocirepospecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// StreamLocalResource is a fused transformer that adds a by-value source
// resource (for example a wget or s3 access) to an OCI registry target as a
// local blob, without a temporary file.
//
// Download and add run in one node, so the lazy source blob that DownloadResource
// returns for wget and s3 does not cross a graph node boundary. The blob is handed
// to AddLocalResource as a size-unknown blob, which pack uploads with the chunked
// streaming push (PushStreaming) and verifies against the resource digest on the
// fly.
//
// A repository that returns materialized content (for example a non-idempotent
// wget request, which is downloaded once into a temporary file) takes the regular
// push. CTF targets do not use this transformer: the blob must be stored on local
// disk there anyway.
type StreamLocalResource struct {
	Scheme             *runtime.Scheme
	RepoProvider       repository.ComponentVersionRepositoryProvider
	ResourceRepository repository.ResourceRepository
	CredentialProvider credentials.Resolver
}

func (t *StreamLocalResource) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var transformation v1alpha1.OCIStreamLocalResource
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting stream local resource transformation: %w", err)
	}
	spec := transformation.Spec
	if spec == nil {
		return nil, fmt.Errorf("spec is required for OCIStreamLocalResource transformation")
	}
	if spec.Component == "" {
		return nil, fmt.Errorf("component name is required")
	}
	if spec.Version == "" {
		return nil, fmt.Errorf("component version is required")
	}
	if spec.Resource == nil {
		return nil, fmt.Errorf("source resource is required")
	}
	repoSpec := &spec.Repository
	component, version := spec.Component, spec.Version
	sourceResource := spec.Resource

	srcResource := descriptor.ConvertFromV2Resource(sourceResource)
	if srcResource == nil {
		return nil, fmt.Errorf("failed converting source resource from v2 format")
	}
	if srcResource.Access == nil {
		return nil, fmt.Errorf("source resource access is required")
	}

	targetCreds, err := t.resolveTargetCredentials(ctx, repoSpec)
	if err != nil {
		return nil, err
	}

	repo, err := t.RepoProvider.GetComponentVersionRepository(ctx, repoSpec, targetCreds)
	if err != nil {
		return nil, fmt.Errorf("failed getting component version repository: %w", err)
	}

	if err := applyGlobalAccessPolicy(repo, spec.GlobalAccessPolicy); err != nil {
		return nil, err
	}

	srcCreds, err := t.resolveSourceCredentials(ctx, srcResource)
	if err != nil {
		return nil, err
	}

	srcBlob, err := t.ResourceRepository.DownloadResource(ctx, srcResource, srcCreds)
	if err != nil {
		return nil, fmt.Errorf("failed opening source resource %v: %w", srcResource.ToIdentity(), err)
	}
	// Releases the temporary file of a downloaded source, or a stream that was
	// opened to read its media type but never consumed.
	defer func() {
		if c, ok := srcBlob.(io.Closer); ok {
			_ = c.Close()
		}
	}()

	// Like the split DownloadResource -> AddLocalResource path, a source without a
	// known media type packs as application/octet-stream.
	mediaType := blobMediaType(srcBlob)
	if mediaType == "" {
		mediaType = octetStream
	}

	// The access must be a *v2.LocalBlob: pack converts it with the OCI repository
	// scheme to *v2.LocalBlob, which fails for the typed runtime.LocalBlob.
	// LocalReference is set by pack from the pushed digest. ReferenceName stays
	// empty like on the split path: it names an OCI artifact, which a plain blob
	// is not. The copied source digest makes pack verify the streamed bytes.
	addResource := srcResource.DeepCopy()
	addResource.Access = &v2.LocalBlob{
		Type:      runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
		MediaType: mediaType,
	}

	// A lazy source does not know its size, a materialized one does.
	mode := "downloaded"
	if blobSize(srcBlob) == blob.SizeUnknown {
		mode = "streamed"
	}
	slog.InfoContext(ctx, "adding local resource to target",
		"resource", srcResource.ToIdentity(),
		"component", component,
		"version", version,
		"source", mode)

	updatedResource, err := repo.AddLocalResource(ctx, component, version, addResource, srcBlob)
	if err != nil {
		return nil, fmt.Errorf("failed adding local resource %q to component %s:%s: %w",
			srcResource.Name, component, version, err)
	}

	v2UpdatedResource, err := descriptor.ConvertToV2Resource(oci.DefaultRepositoryScheme, updatedResource)
	if err != nil {
		return nil, fmt.Errorf("failed converting updated resource to v2 format: %w", err)
	}

	// Like AddLocalResource: a resource whose source carried no digest must stay without
	// one. Pack computes the digest of the streamed bytes, but transfer never injects a
	// digest the source lacked, because that would change the signed descriptor.
	if srcResource.Digest == nil {
		v2UpdatedResource.Digest = nil
	}

	transformation.Output = &v1alpha1.OCIStreamLocalResourceOutput{Resource: v2UpdatedResource}
	return &transformation, nil
}

// resolveTargetCredentials best-effort resolves credentials for the target
// repository. A missing provider, an unresolvable consumer identity, or
// ErrNotFound yields nil credentials; only a real resolve error is returned.
func (t *StreamLocalResource) resolveTargetCredentials(ctx context.Context, repoSpec runtime.Typed) (runtime.Typed, error) {
	if t.CredentialProvider == nil {
		return nil, nil
	}
	consumerID, err := t.RepoProvider.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, repoSpec)
	if err != nil {
		return nil, fmt.Errorf("failed deriving target consumer identity: %w", err)
	}
	if consumerID == nil {
		return nil, nil
	}
	creds, err := t.CredentialProvider.Resolve(ctx, consumerID)
	if err != nil {
		if errors.Is(err, credentials.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed resolving target credentials: %w", err)
	}
	return creds, nil
}

// applyGlobalAccessPolicy mirrors AddLocalResource: it sets the OCI repository's
// global access policy from the spec so cached repository instances do not leak
// prior state.
func applyGlobalAccessPolicy(repo repository.ComponentVersionRepository, p ocirepospecv1.GlobalAccessPolicy) error {
	ociRepo, ok := repo.(*oci.Repository)
	if !ok {
		if p != ocirepospecv1.GlobalAccessPolicyNever {
			return fmt.Errorf("globalAccessPolicy is only supported for OCI repositories, got %T", repo)
		}
		return nil
	}
	switch p {
	case ocirepospecv1.GlobalAccessPolicyNever:
		ociRepo.SetGlobalAccessPolicy(oci.GlobalAccessPolicyNever)
	case ocirepospecv1.GlobalAccessPolicyAuto:
		ociRepo.SetGlobalAccessPolicy(oci.GlobalAccessPolicyAuto)
	default:
		return fmt.Errorf("unsupported globalAccessPolicy %q", p)
	}
	return nil
}

// octetStream is the media type a by-value source of unknown media type packs
// with, matching the application/octet-stream default of the old split path.
const octetStream = "application/octet-stream"

// blobSize returns the size b reports, blob.SizeUnknown when unknown.
func blobSize(b blob.ReadOnlyBlob) int64 {
	if s, ok := b.(blob.SizeAware); ok {
		return s.Size()
	}
	return blob.SizeUnknown
}

// blobMediaType returns the media type b reports, "" when unknown.
func blobMediaType(b blob.ReadOnlyBlob) string {
	if mt, ok := b.(blob.MediaTypeAware); ok {
		if m, known := mt.MediaType(); known {
			return m
		}
	}
	return ""
}

// resolveSourceCredentials resolves credentials for the source resource by its
// consumer identity. A missing provider or ErrNotFound yields nil credentials.
func (t *StreamLocalResource) resolveSourceCredentials(ctx context.Context, resource *descriptor.Resource) (runtime.Typed, error) {
	if t.CredentialProvider == nil {
		return nil, nil
	}
	consumerID, err := t.ResourceRepository.GetResourceCredentialConsumerIdentity(ctx, resource)
	if err != nil {
		return nil, fmt.Errorf("failed deriving source consumer identity: %w", err)
	}
	if consumerID == nil {
		return nil, nil
	}
	creds, err := t.CredentialProvider.Resolve(ctx, consumerID)
	if err != nil {
		if errors.Is(err, credentials.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed resolving source credentials: %w", err)
	}
	return creds, nil
}
