package resource

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory/cache"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	helminternal "ocm.software/open-component-model/bindings/go/helm/internal"
	helmdownload "ocm.software/open-component-model/bindings/go/helm/internal/download"
	helmaccess "ocm.software/open-component-model/bindings/go/helm/spec/access"
	"ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/repository/integrity"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ResourceRepository implements a resource repository for Helm charts.
// It supports downloading charts from HTTP/HTTPS and OCI-based Helm repositories
// and resolving credential consumer identities for authentication.
type ResourceRepository struct {
	filesystemConfig *filesystemv1alpha1.Config
	httpConfig       *httpv1alpha1.Config
}

// Option configures a ResourceRepository.
type Option func(*ResourceRepository)

// WithHTTPConfig sets the HTTP client configuration used for chart downloads and OCI registry access.
// The repository builds its internal client from cfg on each download. When nil, the default Helm
// client is used. Accepts the serialisable config type so that external plugins can round-trip it
// over the wire and reconstruct an equivalent client.
func WithHTTPConfig(cfg *httpv1alpha1.Config) Option {
	return func(r *ResourceRepository) {
		r.httpConfig = cfg
	}
}

var _ repository.ResourceRepository = (*ResourceRepository)(nil)

// NewResourceRepository creates a ResourceRepository. If filesystemConfig is non-nil,
// its TempFolder is used for intermediate download directories; otherwise os.TempDir
// is used.
func NewResourceRepository(filesystemConfig *filesystemv1alpha1.Config, opts ...Option) *ResourceRepository {
	if filesystemConfig == nil {
		filesystemConfig = &filesystemv1alpha1.Config{}
	}
	r := &ResourceRepository{
		filesystemConfig: filesystemConfig,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// GetResourceRepositoryScheme returns the Helm access scheme containing the
// helm/v1 type and its aliases.
func (r *ResourceRepository) GetResourceRepositoryScheme() *runtime.Scheme {
	return helmaccess.Scheme
}

// GetResourceCredentialConsumerIdentity resolves the credential consumer identity
// for the given helm resource. For OCI-based helm repositories the identity type
// is OCIRegistry; for HTTP/HTTPS repositories it is HelmChartRepository.
// Returns nil if the resource has no remote repository (local chart).
func (r *ResourceRepository) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (runtime.Identity, error) {
	helm, err := r.convertAccess(resource)
	if err != nil {
		return nil, fmt.Errorf("error converting resource access to helm spec: %w", err)
	}

	if helm.HelmRepository == "" {
		slog.DebugContext(ctx, "local helm inputs do not require credentials")
		return nil, nil
	}

	return helminternal.CredentialConsumerIdentity(helm.HelmRepository)
}

// DownloadResource fetches a helm chart (and optional provenance file) from the
// remote repository specified in the resource's helm access. The returned blob
// is a [helmblob.ChartBlob] wrapping a tar archive of the downloaded files.
func (r *ResourceRepository) DownloadResource(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	helm, err := r.convertAccess(resource)
	if err != nil {
		return nil, err
	}

	if helm.HelmRepository == "" {
		return nil, fmt.Errorf("helm repository URL is required for downloading a chart")
	}

	helmURL, err := helm.ChartReference()
	if err != nil {
		return nil, fmt.Errorf("error constructing chart reference: %w", err)
	}

	slog.DebugContext(ctx, "Resolved helm chart reference for download", "chartReference", helmURL)

	tempDir := ""
	if r.filesystemConfig != nil && r.filesystemConfig.TempFolder != nil {
		tempDir = *r.filesystemConfig.TempFolder
	}

	downloadDir, err := os.MkdirTemp(tempDir, "helm-resource-download-*")
	if err != nil {
		return nil, fmt.Errorf("error creating temporary directory for helm download: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(downloadDir)
	}()

	slog.DebugContext(ctx, "Created temporary download directory", "dir", downloadDir)

	isOCI := strings.HasPrefix(helmURL, "oci://")
	opts := []helmdownload.Option{helmdownload.WithAlwaysDownloadProv(true)}
	var ociCreds *ocicredsv1.OCICredentials
	if credentials != nil {
		if isOCI {
			if ociCreds, err = helmcredsv1.ConvertToOCICredentials(credentials); err != nil {
				return nil, fmt.Errorf("error converting credentials: %w", err)
			}
			opts = append(opts, helmdownload.WithOCICredentials(ociCreds))
		} else {
			helmCreds, err := helmcredsv1.ConvertToHelmHTTPCredentials(credentials)
			if err != nil {
				return nil, fmt.Errorf("error converting credentials: %w", err)
			}
			opts = append(opts, helmdownload.WithCredentials(helmCreds))
		}
	}
	if r.httpConfig != nil {
		opts = append(opts, helmdownload.WithHTTPConfig(r.httpConfig))
	}

	var pin digest.Digest
	if isOCI {
		if pin, err = r.manifestPin(ctx, resource, helmURL, ociCreds); err != nil {
			return nil, err
		}
		if pin != "" {
			opts = append(opts, helmdownload.WithOCIManifestDigest(pin))
		}
	}

	result, err := helmdownload.NewReadOnlyChartFromRemote(ctx, helmURL, downloadDir, opts...)
	if err != nil {
		return nil, fmt.Errorf("error downloading helm chart %q: %w", helmURL, err)
	}

	// Verification covers the chart .tgz as downloaded. A pinned chart is bound through its
	// manifest: the registry client pulls it by that digest and checks the chart layer against
	// the digest the manifest lists, so the .tgz on disk is that layer. Otherwise the resource
	// digest must describe the .tgz itself. The tar returned below is no transform of the chart:
	// it only packs the verified .tgz (and its .prov) byte for byte, read back from downloadDir,
	// which this function created and nothing else writes to.
	if pin == "" {
		if err := integrity.VerifyContent(ctx, resource.Digest, result.ChartBlob,
			integrity.WithLogAttrs(slog.Any("resource", resource.ToIdentity()))); err != nil {
			return nil, fmt.Errorf("helm chart %q does not match the digest of resource %q: %w", helmURL, resource.Name, err)
		}
	}

	slog.DebugContext(ctx, "Helm chart downloaded successfully, creating tar archive", "chartReference", helmURL)

	streamingBlob, err := filesystem.GetBlobFromPath(ctx, result.ChartDir, filesystem.DirOptions{
		// Reproducible ensures that the tar header is not affected by times, uid or other meta infos and the digests stay stable
		Reproducible: true,
	})
	if err != nil {
		return nil, fmt.Errorf("error creating tar archive from helm download: %w", err)
	}

	// GetBlobFromPath returns a lazy streaming blob that reads files on demand.
	// Since the download directory is removed when this function returns, the blob
	// must be fully materialized into memory before cleanup.
	tarBlob, err := cache.Cache(streamingBlob)
	if err != nil {
		return nil, fmt.Errorf("error buffering tar archive from helm download: %w", err)
	}

	slog.DebugContext(ctx, "Created tar archive from downloaded helm chart files")

	return tarBlob, nil
}

// manifestPin returns the manifest digest an oci:// chart must be pulled by, or "" if the resource
// digest does not name its manifest.
func (r *ResourceRepository) manifestPin(ctx context.Context, resource *descriptor.Resource, helmURL string, creds *ocicredsv1.OCICredentials) (digest.Digest, error) {
	want, err := integrity.ParseDigest(resource.Digest)
	if err != nil {
		return "", fmt.Errorf("resource %q: %w", resource.Name, err)
	}
	if want == "" {
		return "", nil
	}
	switch resource.Digest.NormalisationAlgorithm {
	case descriptor.OCIArtifactDigestV1:
		return want, nil
	case descriptor.GenericBlobDigestV1, "":
		// OCM v1 records the chart .tgz digest here, OCM v2 historically the manifest digest.
		// Resolve reports the registry's SHA-256 manifest digest, which another algorithm never equals.
		if want.Algorithm() != digest.SHA256 {
			return "", nil
		}
		m, err := helmdownload.ResolveOCIManifestDigest(ctx, helmURL, creds, r.httpConfig)
		if err != nil {
			// Not knowing the manifest only means the .tgz content check decides instead.
			slog.WarnContext(ctx, "could not resolve the manifest digest of the helm chart, verifying the chart archive instead",
				"chartReference", helmURL, "resource", resource.Name, "error", err)
			return "", nil
		}
		if m == want {
			return want, nil
		}
	}
	return "", nil
}

// UploadResource is not supported for Helm repositories and always returns an error.
// Traditional Helm chart repositories are read-only HTTP servers that serve a static
// index.yaml and packaged chart archives; there is no standardized upload API.
// Charts stored in OCI registries should use the OCI resource repository instead.
func (r *ResourceRepository) UploadResource(_ context.Context, _ *descriptor.Resource, _ blob.ReadOnlyBlob, _ runtime.Typed) (*descriptor.Resource, error) {
	return nil, fmt.Errorf("helm chart repositories do not support upload operations")
}

func (r *ResourceRepository) convertAccess(resource *descriptor.Resource) (*v1.Helm, error) {
	if resource == nil || resource.Access == nil {
		return nil, fmt.Errorf("resource access is required")
	}
	var helm v1.Helm
	if err := helmaccess.Scheme.Convert(resource.Access, &helm); err != nil {
		return nil, fmt.Errorf("error converting access to helm spec: %w", err)
	}
	return &helm, nil
}
