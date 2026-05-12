package resource

import (
	"context"
	"fmt"
	"log/slog"
	"os"

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
	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ResourceRepository implements [repository.ResourceRepository] for Helm charts.
// It supports downloading charts from HTTP/HTTPS and OCI-based Helm repositories
// and resolving credential consumer identities for authentication.
type ResourceRepository struct {
	filesystemConfig *filesystemv1alpha1.Config
}

var _ repository.ResourceRepository = (*ResourceRepository)(nil)

// NewResourceRepository creates a ResourceRepository. If filesystemConfig is non-nil,
// its TempFolder is used for intermediate download directories; otherwise os.TempDir
// is used.
func NewResourceRepository(filesystemConfig *filesystemv1alpha1.Config) *ResourceRepository {
	if filesystemConfig == nil {
		filesystemConfig = &filesystemv1alpha1.Config{}
	}
	return &ResourceRepository{
		filesystemConfig: filesystemConfig,
	}
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
//
// TODO(matthiasbruns): migrate return type to runtime.Typed once the ResourceRepository interface is updated.
// https://github.com/open-component-model/ocm-project/issues/988
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
//
// TODO(matthiasbruns): migrate credentials parameter to runtime.Typed once the ResourceRepository interface is updated.
// https://github.com/open-component-model/ocm-project/issues/988
func (r *ResourceRepository) DownloadResource(ctx context.Context, resource *descriptor.Resource, credentials map[string]string) (blob.ReadOnlyBlob, error) {
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
	if r.filesystemConfig != nil {
		tempDir = r.filesystemConfig.TempFolder
	}

	downloadDir, err := os.MkdirTemp(tempDir, "helm-resource-download-*")
	if err != nil {
		return nil, fmt.Errorf("error creating temporary directory for helm download: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(downloadDir)
	}()

	slog.DebugContext(ctx, "Created temporary download directory", "dir", downloadDir)

	opts := []helmdownload.Option{
		helmdownload.WithCredentials(helmcredsv1.FromDirectCredentials(credentials)),
		helmdownload.WithOCICredentials(ocicredsv1.FromDirectCredentials(credentials)),
		helmdownload.WithAlwaysDownloadProv(true),
	}

	result, err := helmdownload.NewReadOnlyChartFromRemote(ctx, helmURL, downloadDir, opts...)
	if err != nil {
		return nil, fmt.Errorf("error downloading helm chart %q: %w", helmURL, err)
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

// UploadResource is not supported for Helm repositories and always returns an error.
// Traditional Helm chart repositories are read-only HTTP servers that serve a static
// index.yaml and packaged chart archives; there is no standardized upload API.
// Charts stored in OCI registries should use the OCI resource repository instead.
//
// TODO(matthiasbruns): migrate credentials parameter to runtime.Typed once the ResourceRepository interface is updated.
// https://github.com/open-component-model/ocm-project/issues/988
func (r *ResourceRepository) UploadResource(_ context.Context, _ *descriptor.Resource, _ blob.ReadOnlyBlob, _ map[string]string) (*descriptor.Resource, error) {
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
