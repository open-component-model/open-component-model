package resource

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	helmaccess "ocm.software/open-component-model/bindings/go/helm/access"
	v1 "ocm.software/open-component-model/bindings/go/helm/access/spec/v1"
	helmblob "ocm.software/open-component-model/bindings/go/helm/blob"
	helmdownload "ocm.software/open-component-model/bindings/go/helm/internal/download"
	ocicredentialsspecv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/identity/v1"
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

	if helm.HelmRepository == "" {
		slog.DebugContext(ctx, "local helm inputs do not require credentials")
		return nil, nil
	}

	identity, err := runtime.ParseURLToIdentity(helm.HelmRepository)
	if err != nil {
		return nil, fmt.Errorf("error parsing helm repository URL to identity: %w", err)
	}

	if scheme, ok := identity[runtime.IdentityAttributeScheme]; ok && scheme == "oci" {
		identity.SetType(ocicredentialsspecv1.Type)
	} else {
		identity.SetType(runtime.NewUnversionedType(helmaccess.LegacyHelmChartConsumerType))
	}

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

// tarDirectoryRecursive creates a tar archive from all files in the given directory
// and its subdirectories, returning it as an in-memory blob. File paths in the
// archive are relative to the root directory so consumers can extract entries
// by name (e.g. the chart tgz and its .prov sidecar).
func tarDirectoryRecursive(dir string) (blob.ReadOnlyBlob, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	defer func(tw *tar.Writer) {
		if err := tw.Close(); err != nil {
			slog.Warn("error closing tar writer", "error", err)
		}
	}(tw)

	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("error opening root directory %s: %w", dir, err)
	}
	defer func() { _ = root.Close() }()

	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return fmt.Errorf("error computing relative path for %s: %w", path, err)
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("error creating tar header for %s: %w", relPath, err)
		}
		header.Name = relPath

		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("error writing tar header for %s: %w", relPath, err)
		}

		f, err := root.Open(relPath)
		if err != nil {
			return fmt.Errorf("error opening file %s: %w", relPath, err)
		}
		defer func() { _ = f.Close() }()

		if _, err := io.Copy(tw, f); err != nil {
			return fmt.Errorf("error writing file %s to tar: %w", relPath, err)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("error walking download directory: %w", err)
	}

	return inmemory.New(&buf, inmemory.WithMediaType("application/x-tar")), nil
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
