package componentversionrepository

import (
	"context"
	"errors"
	"fmt"
	"os"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ocmrepositoryv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/ocmrepository/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/blobs"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type componentVersionRepositoryProviderConverter struct {
	externalPlugin ocmrepositoryv1.ReadWriteOCMRepositoryPluginContract[runtime.Typed]
	scheme         *runtime.Scheme
}

var _ ComponentVersionRepositoryProvider = (*componentVersionRepositoryProviderConverter)(nil)

func (c *componentVersionRepositoryProviderConverter) GetComponentVersionRepositoryCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (runtime.Identity, error) {
	request := &ocmrepositoryv1.GetIdentityRequest[runtime.Typed]{
		Typ: repositorySpecification,
	}

	result, err := c.externalPlugin.GetIdentity(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to get identity: %w", err)
	}

	return result.Identity, nil
}

func (c *componentVersionRepositoryProviderConverter) GetComponentVersionRepository(ctx context.Context, repositorySpecification runtime.Typed, credentials map[string]string) (ComponentVersionRepository, error) {
	return &componentVersionRepositoryWrapper{
		externalPlugin:          c.externalPlugin,
		repositorySpecification: repositorySpecification,
		credentials:             credentials,
		scheme:                  c.scheme,
	}, nil
}

type componentVersionRepositoryWrapper struct {
	externalPlugin          ocmrepositoryv1.ReadWriteOCMRepositoryPluginContract[runtime.Typed]
	repositorySpecification runtime.Typed
	credentials             map[string]string
	scheme                  *runtime.Scheme
}

var _ ComponentVersionRepository = (*componentVersionRepositoryWrapper)(nil)

func (c *componentVersionRepositoryWrapper) AddComponentVersion(ctx context.Context, desc *descriptor.Descriptor) error {
	convertedDesc, err := descriptor.ConvertToV2(c.scheme, desc)
	if err != nil {
		return fmt.Errorf("failed to convert descriptor to v2: %w", err)
	}

	request := ocmrepositoryv1.PostComponentVersionRequest[runtime.Typed]{
		Repository: c.repositorySpecification,
		Descriptor: convertedDesc,
	}

	return c.externalPlugin.AddComponentVersion(ctx, request, c.credentials)
}

func (c *componentVersionRepositoryWrapper) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	request := ocmrepositoryv1.GetComponentVersionRequest[runtime.Typed]{
		Repository: c.repositorySpecification,
		Name:       component,
		Version:    version,
	}

	return c.externalPlugin.GetComponentVersion(ctx, request, c.credentials)
}

func (c *componentVersionRepositoryWrapper) ListComponentVersions(ctx context.Context, component string) ([]string, error) {
	request := ocmrepositoryv1.ListComponentVersionsRequest[runtime.Typed]{
		Repository: c.repositorySpecification,
		Name:       component,
	}

	return c.externalPlugin.ListComponentVersions(ctx, request, c.credentials)
}

func (c *componentVersionRepositoryWrapper) HealthCheck(ctx context.Context) error {
	// External plugins don't currently support HealthCheck in their contract.
	// For now, we assume the plugin is healthy if it was successfully instantiated.
	// In the future, this could be enhanced to call a plugin-specific health check method.
	return nil
}

func (c *componentVersionRepositoryWrapper) AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (_ *descriptor.Resource, err error) {
	resources, err := descriptor.ConvertToV2Resources(c.scheme, []descriptor.Resource{*res})
	if err != nil {
		return nil, fmt.Errorf("failed to convert resource: %w", err)
	}

	tmp, err := os.CreateTemp("", "resource")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() {
		err = errors.Join(err, tmp.Close())
	}()

	if err := filesystem.CopyBlobToOSPath(content, tmp.Name()); err != nil {
		return nil, fmt.Errorf("failed to copy blob to OS path: %w", err)
	}

	request := ocmrepositoryv1.PostLocalResourceRequest[runtime.Typed]{
		Repository: c.repositorySpecification,
		Name:       component,
		Version:    version,
		Resource:   &resources[0],
		ResourceLocation: types.Location{
			LocationType: types.LocationTypeLocalFile,
			Value:        tmp.Name(),
		},
	}

	return c.externalPlugin.AddLocalResource(ctx, request, c.credentials)
}

func (c *componentVersionRepositoryWrapper) GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	request := ocmrepositoryv1.GetLocalResourceRequest[runtime.Typed]{
		Repository: c.repositorySpecification,
		Name:       component,
		Version:    version,
		Identity:   identity,
	}

	response, err := c.externalPlugin.GetLocalResource(ctx, request, c.credentials)
	if err != nil {
		return nil, nil, err
	}

	rBlob, err := blobs.CreateBlobData(response.Location)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create blob data: %w", err)
	}

	convert := descriptor.ConvertFromV2Resources([]descriptorv2.Resource{*response.Resource})

	return rBlob, &convert[0], nil
}

func (c *componentVersionRepositoryWrapper) AddLocalSource(ctx context.Context, component, version string, res *descriptor.Source, content blob.ReadOnlyBlob) (_ *descriptor.Source, err error) {
	sources, err := descriptor.ConvertToV2Sources(c.scheme, []descriptor.Source{*res})
	if err != nil {
		return nil, fmt.Errorf("failed to convert source: %w", err)
	}

	tmp, err := os.CreateTemp("", "source")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}

	defer func() {
		err = errors.Join(err, tmp.Close())
	}()

	if err := filesystem.CopyBlobToOSPath(content, tmp.Name()); err != nil {
		return nil, fmt.Errorf("failed to copy blob to OS path: %w", err)
	}

	request := ocmrepositoryv1.PostLocalSourceRequest[runtime.Typed]{
		Repository: c.repositorySpecification,
		Name:       component,
		Version:    version,
		Source:     &sources[0],
		SourceLocation: types.Location{
			LocationType: types.LocationTypeLocalFile,
			Value:        tmp.Name(),
		},
	}

	return c.externalPlugin.AddLocalSource(ctx, request, c.credentials)
}

func (c *componentVersionRepositoryWrapper) GetLocalSource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Source, error) {
	request := ocmrepositoryv1.GetLocalSourceRequest[runtime.Typed]{
		Repository: c.repositorySpecification,
		Name:       component,
		Version:    version,
		Identity:   identity,
	}

	response, err := c.externalPlugin.GetLocalSource(ctx, request, c.credentials)
	if err != nil {
		return nil, nil, err
	}

	rBlob, err := blobs.CreateBlobData(response.Location)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create blob data: %w", err)
	}

	convert := descriptor.ConvertFromV2Sources([]descriptorv2.Source{*response.Source})

	return rBlob, &convert[0], nil
}

func (r *RepositoryRegistry) externalToComponentVersionRepositoryProviderConverter(plugin ocmrepositoryv1.ReadWriteOCMRepositoryPluginContract[runtime.Typed], scheme *runtime.Scheme) *componentVersionRepositoryProviderConverter {
	return &componentVersionRepositoryProviderConverter{
		externalPlugin: plugin,
		scheme:         scheme,
	}
}
