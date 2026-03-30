package resource

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"ocm.software/open-component-model/bindings/go/blob"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	helmaccess "ocm.software/open-component-model/bindings/go/helm/access"
	v1 "ocm.software/open-component-model/bindings/go/helm/access/spec/v1"
	helmdownload "ocm.software/open-component-model/bindings/go/helm/internal/download"
	ocicredentialsspecv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/identity/v1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type ResourceRepository struct {
	filesystemConfig *filesystemv1alpha1.Config
}

var _ repository.ResourceRepository = (*ResourceRepository)(nil)

func NewResourceRepository(filesystemConfig *filesystemv1alpha1.Config) *ResourceRepository {
	return &ResourceRepository{
		filesystemConfig: filesystemConfig,
	}
}

func (r *ResourceRepository) GetResourceRepositoryScheme() *runtime.Scheme {
	return helmaccess.Scheme
}

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

func (r *ResourceRepository) DownloadResource(ctx context.Context, resource *descriptor.Resource, credentials map[string]string) (blob.ReadOnlyBlob, error) {
	helm, err := r.convertAccess(resource)
	if err != nil {
		return nil, err
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

	opts := []helmdownload.Option{
		helmdownload.WithCredentials(credentials),
	}

	chartData, err := helmdownload.NewReadOnlyChartFromRemote(ctx, helmURL, downloadDir, opts...)
	if err != nil {
		_ = os.RemoveAll(downloadDir)
		return nil, fmt.Errorf("error downloading helm chart %q: %w", helmURL, err)
	}

	return chartData.ChartBlob, nil
}

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
	obj, err := r.GetResourceRepositoryScheme().NewObject(t)
	if err != nil {
		return nil, fmt.Errorf("error creating new object for type %s: %w", t, err)
	}
	if err := r.GetResourceRepositoryScheme().Convert(resource.Access, obj); err != nil {
		return nil, fmt.Errorf("error converting access to object of type %s: %w", t, err)
	}
	helm, ok := obj.(*v1.Helm)
	if !ok {
		return nil, fmt.Errorf("unexpected type %T for helm access", obj)
	}
	return helm, nil
}
