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
// Download and add run in one node, so the lazy source stream does not cross a
// graph node boundary. The source is opened with
// [repository.StreamOrDownloadResource] and handed to AddLocalResource as a
// size-unknown blob, which pack uploads with the chunked streaming push
// (PushStreaming) and verifies against the resource digest on the fly.
//
// A source that cannot be streamed (a repository without the streaming
// capability, or a non-idempotent request) is downloaded with DownloadResource
// instead and takes the regular push. CTF targets do not use this transformer:
// the blob must be stored on local disk there anyway.
type StreamLocalResource struct {
	Scheme             *runtime.Scheme
	RepoProvider       repository.ComponentVersionRepositoryProvider
	ResourceRepository repository.ResourceRepository
	CredentialProvider credentials.Resolver
}

func (t *StreamLocalResource) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	transformation, spec, err := t.decodeStreamSpec(step)
	if err != nil {
		return nil, err
	}
	repoSpec := spec.repoSpec
	component := spec.component
	version := spec.version
	sourceResource := spec.sourceResource
	output := spec.output
	globalAccessPolicy := spec.globalAccessPolicy

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

	if err := applyGlobalAccessPolicy(repo, globalAccessPolicy); err != nil {
		return nil, err
	}

	localResourceRepo, ok := repo.(localResourceAdder)
	if !ok {
		return nil, fmt.Errorf("target repository %T cannot add local resources", repo)
	}

	// Resolve source credentials by consumer identity (same approach as HTTPStreaming).
	srcCreds, err := t.resolveSourceCredentials(ctx, srcResource)
	if err != nil {
		return nil, err
	}

	srcBlob, streamed, err := repository.StreamOrDownloadResource(ctx, t.ResourceRepository, srcResource, srcCreds)
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

	mode := "downloaded"
	if streamed {
		mode = "streamed"
	}
	slog.InfoContext(ctx, "adding local resource to target",
		"resource", srcResource.ToIdentity(),
		"component", component,
		"version", version,
		"source", mode)

	updatedResource, err := localResourceRepo.AddLocalResource(ctx, component, version, addResource, srcBlob)
	if err != nil {
		return nil, fmt.Errorf("failed adding local resource %q to component %s:%s: %w",
			srcResource.Name, component, version, err)
	}

	v2UpdatedResource, err := descriptor.ConvertToV2Resource(oci.DefaultRepositoryScheme, updatedResource)
	if err != nil {
		return nil, fmt.Errorf("failed converting updated resource to v2 format: %w", err)
	}

	if err := setStreamOutput(output, v2UpdatedResource); err != nil {
		return nil, err
	}

	return transformation, nil
}

// streamSpecView is the decoded, validated view of a stream-local-resource step.
type streamSpecView struct {
	repoSpec           runtime.Typed
	component          string
	version            string
	sourceResource     *v2.Resource
	output             any
	globalAccessPolicy ocirepospecv1.GlobalAccessPolicy
}

// decodeStreamSpec converts the generic step into the concrete OCI stream
// transformation, validates it, and returns the concrete transformation (needed
// for the output) together with a flat view of its fields.
func (t *StreamLocalResource) decodeStreamSpec(step runtime.Typed) (runtime.Typed, *streamSpecView, error) {
	transformation, err := t.Scheme.NewObject(step.GetType())
	if err != nil {
		return nil, nil, fmt.Errorf("failed creating stream local resource transformation object: %w", err)
	}
	if err := t.Scheme.Convert(step, transformation); err != nil {
		return nil, nil, fmt.Errorf("failed converting generic transformation to stream local resource transformation: %w", err)
	}

	v := &streamSpecView{}
	switch tr := transformation.(type) {
	case *v1alpha1.OCIStreamLocalResource:
		if tr.Spec == nil {
			return nil, nil, fmt.Errorf("spec is required for OCIStreamLocalResource transformation")
		}
		if tr.Output == nil {
			tr.Output = &v1alpha1.OCIStreamLocalResourceOutput{}
		}
		v.repoSpec = &tr.Spec.Repository
		v.component = tr.Spec.Component
		v.version = tr.Spec.Version
		v.sourceResource = tr.Spec.Resource
		v.globalAccessPolicy = tr.Spec.GlobalAccessPolicy
		v.output = tr.Output
	default:
		return nil, nil, fmt.Errorf("unexpected transformation type: %T", transformation)
	}

	if v.component == "" {
		return nil, nil, fmt.Errorf("component name is required")
	}
	if v.version == "" {
		return nil, nil, fmt.Errorf("component version is required")
	}
	if v.sourceResource == nil {
		return nil, nil, fmt.Errorf("source resource is required")
	}
	return transformation, v, nil
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

// setStreamOutput writes the updated resource onto the OCI output.
func setStreamOutput(output any, resource *v2.Resource) error {
	switch out := output.(type) {
	case *v1alpha1.OCIStreamLocalResourceOutput:
		out.Resource = resource
	default:
		return fmt.Errorf("unexpected output type: %T", output)
	}
	return nil
}

// localResourceAdder is the subset of the component version repository used to
// embed a resource as a local blob.
type localResourceAdder interface {
	AddLocalResource(ctx context.Context, component, version string, resource *descriptor.Resource, content blob.ReadOnlyBlob) (*descriptor.Resource, error)
}

// octetStream is the media type a by-value source of unknown media type packs
// with, matching the application/octet-stream default of the old split path.
const octetStream = "application/octet-stream"

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
