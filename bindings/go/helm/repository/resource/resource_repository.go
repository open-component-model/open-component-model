package resource

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	helmaccess "ocm.software/open-component-model/bindings/go/helm/access"
	v1 "ocm.software/open-component-model/bindings/go/helm/access/spec/v1"
	helmblob "ocm.software/open-component-model/bindings/go/helm/blob"
	helminternal "ocm.software/open-component-model/bindings/go/helm/internal"
	helmdownload "ocm.software/open-component-model/bindings/go/helm/internal/download"
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
func (r *ResourceRepository) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (runtime.Identity, error) {
	helm, err := r.convertAccess(resource)
	if err != nil {
		return nil, err
	}

	identity, err := helminternal.CredentialConsumerIdentity(helm.HelmRepository)
	if err != nil {
		return nil, err
	}

	slog.DebugContext(ctx, "Resolved credential consumer identity for helm resource", "identity", identity)

	return identity, nil
}

// DownloadResource fetches a helm chart (and optional provenance file) from the
// remote repository specified in the resource's helm access. The returned blob
// is a [helmblob.ChartBlob] wrapping a tar archive of the downloaded files.
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
		helmdownload.WithCredentials(credentials),
		helmdownload.WithAlwaysDownloadProv(true),
	}

	if _, err := helmdownload.NewReadOnlyChartFromRemote(ctx, helmURL, downloadDir, opts...); err != nil {
		return nil, fmt.Errorf("error downloading helm chart %q: %w", helmURL, err)
	}

	slog.DebugContext(ctx, "Helm chart downloaded successfully, creating tar archive", "chartReference", helmURL)

	tarBlob, err := tarDirectoryRecursive(downloadDir)
	if err != nil {
		return nil, fmt.Errorf("error creating tar archive from helm download: %w", err)
	}

	slog.DebugContext(ctx, "Created tar archive from downloaded helm chart files")

	return helmblob.NewChartBlob(tarBlob), nil
}

// tarDirectoryRecursive creates an in-memory tar archive from all files in the given
// directory tree. The blob must be fully buffered in memory because the download
// directory is cleaned up immediately after this function returns -- the caller
// removes the temp directory once it has the resulting blob, so any lazy reading
// from the filesystem would fail.
func tarDirectoryRecursive(dir string) (blob.ReadOnlyBlob, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// os.OpenRoot restricts all subsequent file operations to the given directory tree,
	// preventing path traversal (e.g. via symlinks) outside of it.
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("error opening root directory %s: %w", dir, err)
	}
	defer func() { _ = root.Close() }()

	fsRoot := os.DirFS(dir)
	err = fs.WalkDir(fsRoot, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("error getting file info for %s: %w", path, err)
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("error creating tar header for %s: %w", path, err)
		}
		header.Name = path

		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("error writing tar header for %s: %w", path, err)
		}

		// Open through the rooted directory to prevent path escape via symlinks.
		f, err := root.Open(path)
		if err != nil {
			return fmt.Errorf("error opening file %s: %w", path, err)
		}

		_, copyErr := io.Copy(tw, f)
		closeErr := f.Close()
		if copyErr != nil {
			return fmt.Errorf("error writing file %s to tar: %w", path, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("error closing file %s: %w", path, closeErr)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("error walking download directory: %w", err)
	}

	// Explicitly close the tar writer before reading the buffer to ensure the
	// tar footer (two 512-byte zero blocks) is flushed.
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("error closing tar writer: %w", err)
	}

	return inmemory.New(&buf, inmemory.WithMediaType(filesystem.DefaultTarMediaType)), nil
}

// UploadResource is not supported for Helm repositories and always returns an error.
// Traditional Helm chart repositories are read-only HTTP servers that serve a static
// index.yaml and packaged chart archives; there is no standardized upload API.
// Charts stored in OCI registries should use the OCI resource repository instead.
func (r *ResourceRepository) UploadResource(_ context.Context, _ *descriptor.Resource, _ blob.ReadOnlyBlob, _ map[string]string) (*descriptor.Resource, error) {
	return nil, fmt.Errorf("helm chart repositories do not support upload operations")
}

func (r *ResourceRepository) convertAccess(resource *descriptor.Resource) (*v1.Helm, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource is required")
	}
	if resource.Access == nil {
		return nil, fmt.Errorf("resource access is required")
	}
	var helm v1.Helm
	if err := helmaccess.Scheme.Convert(resource.Access, &helm); err != nil {
		return nil, fmt.Errorf("error converting access to helm spec: %w", err)
	}
	return &helm, nil
}
