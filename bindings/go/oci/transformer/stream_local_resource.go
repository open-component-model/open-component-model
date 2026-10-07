package transformer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	godigest "github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	internaldigest "ocm.software/open-component-model/bindings/go/oci/internal/digest"
	ocirepospecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// StreamLocalResource is a fused transformer that streams a by-value source
// resource (for example a wget or s3 access) straight into an OCI registry
// target as a local blob, without buffering the content to a temporary file.
//
// It serves OCI registry targets only: a CTF target is a local filesystem
// archive where the blob must land on local disk regardless, so there is no
// streaming benefit and CTF targets keep using the split Download* ->
// AddLocalResource path.
//
// It fuses the download and the add so the stream never has to cross a graph
// node boundary: the source is obtained as a LAZY, size-unknown blob through a
// [repository.StreamingResourceRepository] and handed to
// [oci.Repository.AddLocalResource], where pack's streamResourceLayer routes it
// to the OCI chunked streaming push (PushStreaming). It mirrors the fused
// [TransferOCIArtifact] and HTTPStreaming pattern.
//
// When the injected ResourceRepository is not streaming-capable, or the source
// request is not idempotent (see [repository.IdempotentSource]), the transformer
// falls back to the materialized DownloadResource, which yields a file-backed
// blob with a known size, digest and media type and therefore takes the regular
// buffered push. The result is identical; only the temporary file is avoided on
// the streaming path.
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

	// Obtain the source as a lazy, size-unknown stream when the resource
	// repository is streaming-capable and the source request is idempotent;
	// otherwise fall back to a materialized download (buffered push, same result).
	srcBlob, streamed, err := t.openSource(ctx, srcResource, srcCreds)
	if err != nil {
		return nil, fmt.Errorf("failed opening source resource %v: %w", srcResource.ToIdentity(), err)
	}

	// Resolve the media type the way the split DownloadResource -> AddLocalResource
	// path effectively did, so an untyped wget/s3 source still packs: prefer the
	// media type the blob already knows (the access-pinned media type), otherwise
	// prime it from the stream's response headers (idempotent sources only, see
	// primeMediaType), and finally default to application/octet-stream. Without
	// this a source that does not pin a media type would report an empty media
	// type and pack would reject it with "blob media type is unknown".
	primeMediaType(srcBlob)
	mediaType := blobMediaType(srcBlob)
	if mediaType == "" {
		mediaType = octetStream
	}

	// Build the resource to add: a copy of the source resource with a LocalBlob
	// access so pack can convert it. The reference name mirrors the split
	// AddLocalResource path.
	//
	// The access MUST be a descriptor/v2.LocalBlob (not the typed
	// descriptor/runtime.LocalBlob): pack converts the add resource's access with
	// the OCI repository scheme via opts.AccessScheme.Convert(access, &v2.LocalBlob{}),
	// and oci/blob/update.go UpdateArtifactWithInformationFromBlob type-switches on
	// *v2.LocalBlob. A typed runtime.LocalBlob cannot be converted to v2.LocalBlob
	// and would fail pack with "cannot assign value of type *runtime.LocalBlob to
	// target of type *v2.LocalBlob". A *v2.LocalBlob satisfies runtime.Typed, so it
	// assigns directly to the resource's Access field.
	//
	// LocalReference stays empty: there is no pre-materialized local file; the
	// digest comes from the blob (knownDigestBlob) or is computed by PushStreaming.
	addResource := srcResource.DeepCopy()
	addResource.Access = &v2.LocalBlob{
		Type:          runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
		MediaType:     mediaType,
		ReferenceName: srcResource.Name,
	}

	// If the source resource advertises a SHA-256/genericBlobDigest digest, expose
	// it on the blob so the chunked push verifies the streamed bytes against it;
	// otherwise let PushStreaming compute the digest from the stream.
	//
	// Only wrap the LAZY streaming blob (streamed): it has no known size, so this
	// is additive. A materialized fallback blob is file-backed and SizeAware;
	// wrapping it would hide SizeAware and make pack force PushStreaming on a
	// size-known blob (buffering on fallback, or erroring after the first PATCH).
	// DownloadResource already verified the materialized blob's integrity.
	if streamed {
		if dig, okDigest := knownSHA256Digest(srcResource.Digest); okDigest {
			srcBlob = &knownDigestBlob{base: srcBlob, digest: dig}
		}
	}

	// Report the push mode honestly so an e2e can trust the signal: "streamed"
	// only when the lazy streaming path was actually taken, "materialized" when
	// the source was buffered via DownloadResource (non-streaming repository or a
	// non-idempotent source).
	mode := "materialized"
	if streamed {
		mode = "streamed"
	}
	slog.InfoContext(ctx, "adding local resource to target",
		"resource", srcResource.ToIdentity(),
		"component", component,
		"version", version,
		"mode", mode)

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

// openSource returns the source blob and whether it was obtained as a lazy
// stream (true) or materialized via DownloadResource (false).
//
// A streaming source whose request is not idempotent (see
// [repository.IdempotentSource]) is materialized with DownloadResource instead
// of streamed, mirroring the uploader (transfer/internal/repositoryupload).
// The streaming path opens the source more than once (prime the media type from
// the response headers, then stream the body, plus a possible pack
// chunking-unavailable fallback), which must never re-issue a non-idempotent
// request. Materializing issues the single request the old split path always
// did and carries a media type. No request is sent while deciding, because
// DownloadResourceStream defers all I/O until the blob's ReadCloser is called.
func (t *StreamLocalResource) openSource(ctx context.Context, resource *descriptor.Resource, creds runtime.Typed) (blob.ReadOnlyBlob, bool, error) {
	streamingRepo, ok := t.ResourceRepository.(repository.StreamingResourceRepository)
	if !ok {
		b, err := t.ResourceRepository.DownloadResource(ctx, resource, creds)
		if err != nil {
			return nil, false, err
		}
		return b, false, nil
	}
	b, err := streamingRepo.DownloadResourceStream(ctx, resource, creds)
	if err != nil {
		return nil, false, err
	}
	if idem, ok := b.(repository.IdempotentSource); ok && !idem.Idempotent() {
		mb, err := t.ResourceRepository.DownloadResource(ctx, resource, creds)
		if err != nil {
			return nil, false, err
		}
		return mb, false, nil
	}
	return b, true, nil
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

// primeMediaType best-effort populates the media type of a lazy streaming source
// blob by opening its stream, which reads the response headers (and thus the
// Content-Type) without downloading the whole body. It is a no-op for a blob
// that already knows its media type (such as a materialized file-backed
// download) and is best-effort: any error is ignored, leaving the caller to fall
// back to application/octet-stream, exactly as the old split path did.
//
// The priming open is a separate header-only source request, so it is only
// issued when the source is idempotent (see [repository.IdempotentSource]).
// openSource already materializes a non-idempotent source, so this guard is a
// defence in depth that keeps a non-idempotent request from ever being replayed.
func primeMediaType(b blob.ReadOnlyBlob) {
	aware, ok := b.(blob.MediaTypeAware)
	if !ok {
		return
	}
	if _, known := aware.MediaType(); known {
		return
	}
	if idem, ok := b.(repository.IdempotentSource); !ok || !idem.Idempotent() {
		return
	}
	rc, err := b.ReadCloser()
	if err != nil {
		return
	}
	_ = rc.Close()
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

// knownSHA256Digest returns the OCI digest string ("sha256:<hex>") for a
// SHA-256/genericBlobDigest resource digest, and whether it applies.
func knownSHA256Digest(d *descriptor.Digest) (string, bool) {
	if d == nil || d.Value == "" {
		return "", false
	}
	algo, ok := internaldigest.SHAMapping[d.HashAlgorithm]
	if !ok || algo != godigest.SHA256 {
		return "", false
	}
	dig := godigest.NewDigestFromEncoded(godigest.SHA256, d.Value)
	if err := dig.Validate(); err != nil {
		return "", false
	}
	return dig.String(), true
}

// knownDigestBlob re-exposes a lazy source blob with a digest known in advance
// (from the source resource descriptor) so the chunked streaming push can verify
// the streamed bytes against it. It deliberately does NOT implement
// [blob.SizeAware]: the size stays unknown so pack routes the blob to the
// streaming push instead of the buffered one. It forwards the media type, the
// Close (to release any resources the underlying blob owns), and the idempotency
// report so wrapping hides none of the base blob's capabilities.
type knownDigestBlob struct {
	base   blob.ReadOnlyBlob
	digest string
}

var (
	_ blob.ReadOnlyBlob           = (*knownDigestBlob)(nil)
	_ blob.DigestAware            = (*knownDigestBlob)(nil)
	_ blob.MediaTypeAware         = (*knownDigestBlob)(nil)
	_ repository.IdempotentSource = (*knownDigestBlob)(nil)
	_ io.Closer                   = (*knownDigestBlob)(nil)
)

func (b *knownDigestBlob) ReadCloser() (io.ReadCloser, error) { return b.base.ReadCloser() }

func (b *knownDigestBlob) Digest() (string, bool) { return b.digest, true }

func (b *knownDigestBlob) MediaType() (string, bool) {
	if mt, ok := b.base.(blob.MediaTypeAware); ok {
		return mt.MediaType()
	}
	return "", false
}

func (b *knownDigestBlob) Idempotent() bool {
	if idem, ok := b.base.(repository.IdempotentSource); ok {
		return idem.Idempotent()
	}
	return false
}

func (b *knownDigestBlob) Close() error {
	if c, ok := b.base.(io.Closer); ok {
		return c.Close()
	}
	return nil
}
