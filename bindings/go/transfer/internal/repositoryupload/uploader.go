// Package repositoryupload runs the uploads of resources into vendor repositories, see
// [Uploader.Upload]. The vendor-specific parts are [Backend] and [Store] implementations.
package repositoryupload

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	helmidentityv1 "ocm.software/open-component-model/bindings/go/helm/spec/identity/v1"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/chart"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/client"
	uploadv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/transformation/spec/v1alpha1"
	wgetcredsv1 "ocm.software/open-component-model/bindings/go/wget/spec/credentials/v1"
	wgetidentityv1 "ocm.software/open-component-model/bindings/go/wget/spec/identity/v1"
)

// octetStream is the content type of uploaded content of unknown media type.
const octetStream = "application/octet-stream"

// Uploader holds what every repository upload needs: it fetches the source resource
// and talks to the target server.
type Uploader struct {
	Scheme *runtime.Scheme
	// ResourceRepository downloads remote source resources and derives their credential identities.
	ResourceRepository repository.ResourceRepository
	// RepoProvider resolves the source repositories of local blob resources.
	RepoProvider       repository.ComponentVersionRepositoryProvider
	CredentialProvider credentials.Resolver
	HTTPConfig         *httpv1alpha1.Config

	// PollInterval is the wait between two metadata or search polls; zero uses DefaultPollInterval.
	PollInterval time.Duration
}

// validateSpec rejects a spec missing a field every upload needs.
func validateSpec(spec *uploadv1alpha1.RepositoryUploadSpec) error {
	switch {
	case spec == nil:
		return fmt.Errorf("spec is required")
	case spec.Resource == nil:
		return fmt.Errorf("source resource is required")
	case spec.ComponentVersion == nil || spec.ComponentVersion.Component == "" || spec.ComponentVersion.Version == "":
		return fmt.Errorf("component and version are required")
	case spec.URL == "":
		return fmt.Errorf("url is required")
	case spec.Repository == "":
		return fmt.Errorf("repository is required")
	}
	return nil
}

// target returns a client sending requests with the upload credentials, see
// resolveTargetCredentials.
func (u *Uploader) target(ctx context.Context, helmRepo, repoURL string) (*client.Client, error) {
	creds, err := u.resolveTargetCredentials(ctx, helmRepo, repoURL)
	if err != nil {
		return nil, err
	}
	return client.New(u.HTTPConfig, creds), nil
}

// source returns the unmodified content of the source resource and its media type ("" when unknown):
// local blobs from the source component version, remote resources downloaded with the resolved
// source credentials.
func (u *Uploader) source(ctx context.Context, spec *uploadv1alpha1.RepositoryUploadSpec, src *descriptor.Resource) (blob.ReadOnlyBlob, string, error) {
	if spec.ComponentVersion.Repository != nil {
		repo, err := u.localSource(ctx, spec.ComponentVersion)
		if err != nil {
			return nil, "", err
		}
		b, _, err := repo.GetLocalResource(ctx, spec.ComponentVersion.Component, spec.ComponentVersion.Version, src.ToIdentity())
		if err != nil {
			return nil, "", fmt.Errorf("failed getting local resource %v: %w", src.ToIdentity(), err)
		}
		if mt := blobMediaType(b); mt != "" && mt != octetStream {
			return b, mt, nil
		}
		return b, localBlobMediaType(src.Access), nil
	}
	creds, err := u.resolveSourceCredentials(ctx, src)
	if err != nil {
		return nil, "", err
	}
	// Prefer the lazy streaming source so a remote-to-remote upload (e.g. wget/s3 ->
	// Artifactory/Nexus) streams the source straight to the target instead of
	// buffering it to a temporary file first. Integrity is preserved inline by the
	// streaming blob, and the uploaded bytes are checked against the source digest
	// after the upload (see knownDigest and the digest comparison in Upload). A
	// repository without the capability falls back to a materialized download.
	b, err := u.downloadRemoteSource(ctx, src, creds)
	if err != nil {
		return nil, "", fmt.Errorf("failed downloading source resource %v: %w", src.ToIdentity(), err)
	}
	// A lazy streaming source only learns its media type (the response Content-Type)
	// once a request is issued, whereas the old file-backed download captured it
	// eagerly. Prime it here so a source whose access spec does not pin a media type
	// still uploads with its real content type instead of application/octet-stream.
	// Priming is safe only for an idempotent source; downloadRemoteSource guarantees
	// that by materializing a non-idempotent source instead of streaming it.
	primeMediaType(b)
	return b, blobMediaType(b), nil
}

// primeMediaType best-effort populates the media type of a lazy streaming source
// blob by opening its stream, which reads the response headers (and thus the
// Content-Type) without downloading the whole body. It is a no-op for a blob that
// already knows its media type (such as a materialized file-backed download) and
// is best-effort: any error is ignored, leaving the caller to fall back to the
// access media type or application/octet-stream, exactly as before.
//
// Under the re-issue-per-ReadCloser replay model this priming open is a separate
// header-only source request (prime headers here, then stream the body on the
// upload's own ReadCloser). That extra request is only issued when the source is
// idempotent (see [repository.IdempotentSource]); priming is skipped otherwise so
// a non-idempotent source request is never sent more than once.
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

// downloadRemoteSource obtains a remote source blob, preferring the lazy streaming
// source when the resource repository implements
// [repository.StreamingResourceRepository]. A streaming source whose request is
// not idempotent (see [repository.IdempotentSource]) is materialized with
// DownloadResource instead: the streaming upload path opens the source more than
// once (prime the media type, then stream the body), which must not re-issue a
// non-idempotent request. Materializing issues the single request the old path
// always did. No request is sent while deciding, because DownloadResourceStream
// defers all I/O until the blob's ReadCloser is called.
func (u *Uploader) downloadRemoteSource(ctx context.Context, src *descriptor.Resource, creds runtime.Typed) (blob.ReadOnlyBlob, error) {
	streamingRepo, ok := u.ResourceRepository.(repository.StreamingResourceRepository)
	if !ok {
		return u.ResourceRepository.DownloadResource(ctx, src, creds)
	}
	b, err := streamingRepo.DownloadResourceStream(ctx, src, creds)
	if err != nil {
		return nil, err
	}
	if idem, ok := b.(repository.IdempotentSource); ok && !idem.Idempotent() {
		return u.ResourceRepository.DownloadResource(ctx, src, creds)
	}
	return b, nil
}

// ociSource reports that the source content of src with mediaType was downloaded from an OCI
// artifact, so the source digest does not describe it.
func ociSource(src *descriptor.Resource, mediaType string) bool {
	return chart.IsOCILayout(mediaType) || chart.FromOCIRegistry(src)
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

func localBlobMediaType(access runtime.Typed) string {
	var lb descriptorv2.LocalBlob
	if err := descriptorv2.Scheme.Convert(access, &lb); err != nil {
		return ""
	}
	return lb.MediaType
}

// contentType is the media type content is uploaded with: that of the content, else that of the
// resource access, else application/octet-stream.
func contentType(mediaType string, res *descriptorv2.Resource) string {
	if mediaType != "" {
		return mediaType
	}
	if mt := MediaTypeFromAccess(*res); mt != "" {
		return mt
	}
	return octetStream
}

// MediaTypeFromAccess extracts the source access media type (if any) from the resource
// access, used as the default target media type. Returns "" when absent.
func MediaTypeFromAccess(resource descriptorv2.Resource) string {
	if resource.Access == nil || len(resource.Access.Data) == 0 {
		return ""
	}
	var access struct {
		MediaType string `json:"mediaType"`
	}
	if err := json.Unmarshal(resource.Access.Data, &access); err != nil {
		return ""
	}
	return access.MediaType
}

// interval is the wait between two metadata or search polls.
func (u *Uploader) interval() time.Duration {
	if u.PollInterval == 0 {
		return DefaultPollInterval
	}
	return u.PollInterval
}

// output converts the uploaded resource to its v2 form.
func (u *Uploader) output(out *descriptor.Resource) (*uploadv1alpha1.RepositoryUploadOutput, error) {
	res, err := descriptor.ConvertToV2Resource(u.Scheme, out)
	if err != nil {
		return nil, fmt.Errorf("failed converting uploaded resource to v2 format: %w", err)
	}
	return &uploadv1alpha1.RepositoryUploadOutput{Resource: res}, nil
}

// localSource resolves the source component version repository of a local blob resource.
func (u *Uploader) localSource(ctx context.Context, cv *uploadv1alpha1.RepositoryUploadComponentVersion) (repository.ComponentVersionRepository, error) {
	if u.RepoProvider == nil {
		return nil, fmt.Errorf("no component version repository provider configured for local resources")
	}
	var creds runtime.Typed
	if u.CredentialProvider != nil {
		if consumerID, err := u.RepoProvider.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, cv.Repository); err == nil {
			if creds, err = u.CredentialProvider.Resolve(ctx, consumerID); err != nil && !errors.Is(err, credentials.ErrNotFound) {
				return nil, fmt.Errorf("failed resolving source repository credentials: %w", err)
			}
		}
	}
	repo, err := u.RepoProvider.GetComponentVersionRepository(ctx, cv.Repository, creds)
	if err != nil {
		return nil, fmt.Errorf("failed getting source component version repository: %w", err)
	}
	return repo, nil
}

// resolveSourceCredentials resolves credentials for a remote source resource by its consumer
// identity. A missing provider or ErrNotFound yields nil credentials.
func (u *Uploader) resolveSourceCredentials(ctx context.Context, resource *descriptor.Resource) (runtime.Typed, error) {
	if u.CredentialProvider == nil {
		return nil, nil
	}
	consumerID, err := u.ResourceRepository.GetResourceCredentialConsumerIdentity(ctx, resource)
	if err != nil {
		return nil, fmt.Errorf("failed deriving source consumer identity: %w", err)
	}
	if consumerID == nil {
		return nil, nil
	}
	creds, err := u.CredentialProvider.Resolve(ctx, consumerID)
	if err != nil {
		if errors.Is(err, credentials.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed resolving source credentials: %w", err)
	}
	return creds, nil
}

// resolveTargetCredentials resolves the upload credentials: those of the HelmChartRepository
// identity of the Helm repository URL of the target repository, falling back to the Wget
// identity of its repository URL. Without either, the upload is anonymous. HelmHTTPCredentials
// are mapped to their username and password, which is all an HTTP upload uses.
func (u *Uploader) resolveTargetCredentials(ctx context.Context, helmRepo, repoURL string) (runtime.Typed, error) {
	if u.CredentialProvider == nil {
		return nil, nil
	}
	helmID, err := runtime.ParseURLToIdentity(helmRepo)
	if err != nil {
		return nil, fmt.Errorf("failed deriving target consumer identity: %w", err)
	}
	helmID.SetType(helmidentityv1.Type)
	wgetID, err := wgetidentityv1.IdentityFromURL(repoURL)
	if err != nil {
		return nil, fmt.Errorf("failed deriving target consumer identity: %w", err)
	}

	var creds runtime.Typed
	for _, id := range []runtime.Identity{helmID, wgetID} {
		creds, err = u.CredentialProvider.Resolve(ctx, id)
		if err == nil {
			break
		}
		if !errors.Is(err, credentials.ErrNotFound) {
			return nil, fmt.Errorf("failed resolving target credentials: %w", err)
		}
		creds = nil
	}
	if creds == nil || creds.GetType().Name != helmcredsv1.HelmHTTPCredentialsType {
		return creds, nil
	}
	helmCreds, err := helmcredsv1.ConvertToHelmHTTPCredentials(creds)
	if err != nil {
		return nil, fmt.Errorf("failed converting target credentials: %w", err)
	}
	if helmCreds.CertFile != "" || helmCreds.KeyFile != "" {
		return nil, fmt.Errorf("HelmHTTPCredentials certFile/keyFile are not supported for repository uploads; use WgetCredentials/v1 certificate and privateKey")
	}
	return &wgetcredsv1.WgetCredentials{
		Type:     wgetcredsv1.WgetCredentialsVersionedType,
		Username: helmCreds.Username,
		Password: helmCreds.Password,
	}, nil
}
