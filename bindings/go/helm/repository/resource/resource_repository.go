package resource

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
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
func (r *ResourceRepository) GetResourceCredentialConsumerIdentity(_ context.Context, resource *descriptor.Resource) (runtime.Identity, error) {
	helm, err := r.convertAccess(resource)
	if err != nil {
		return nil, err
	}

	if helm.HelmRepository == "" {
		return nil, nil
	}

	return helminternal.CredentialConsumerIdentity(helm.HelmRepository)
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

	opts := []helmdownload.Option{
		helmdownload.WithCredentials(credentials),
		helmdownload.WithAlwaysDownloadProv(true),
	}

	if _, err := helmdownload.NewReadOnlyChartFromRemote(ctx, helmURL, downloadDir, opts...); err != nil {
		return nil, fmt.Errorf("error downloading helm chart %q: %w", helmURL, err)
	}

	tarBlob, err := tarDirectoryRecursive(downloadDir)
	if err != nil {
		return nil, fmt.Errorf("error creating tar archive from helm download: %w", err)
	}

	return helmblob.NewChartBlob(tarBlob), nil
}

// tarDirectoryRecursive creates an in-memory tar archive from all files in the given
// directory tree. The blob must be fully buffered because the download directory
// is cleaned up immediately after this function returns.
func tarDirectoryRecursive(dir string) (blob.ReadOnlyBlob, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	root := os.DirFS(dir)
	err := fs.WalkDir(root, ".", func(path string, d fs.DirEntry, err error) error {
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

		f, err := root.Open(path)
		if err != nil {
			return fmt.Errorf("error opening file %s: %w", path, err)
		}
		defer func() { _ = f.Close() }()

		if _, err := io.Copy(tw, f); err != nil {
			return fmt.Errorf("error writing file %s to tar: %w", path, err)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("error walking download directory: %w", err)
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("error closing tar writer: %w", err)
	}

	return inmemory.New(&buf, inmemory.WithMediaType(filesystem.DefaultTarMediaType)), nil
}

// UploadResource is not supported for Helm repositories and always returns an error.
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
	t := resource.Access.GetType()
	obj, err := helmaccess.Scheme.NewObject(t)
	if err != nil {
		return nil, fmt.Errorf("error creating new object for type %s: %w", t, err)
	}
	if err := helmaccess.Scheme.Convert(resource.Access, obj); err != nil {
		return nil, fmt.Errorf("error converting access to object of type %s: %w", t, err)
	}
	helm, ok := obj.(*v1.Helm)
	if !ok {
		return nil, fmt.Errorf("unexpected type %T for helm access", obj)
	}
	return helm, nil
}
