package resource

import (
	"context"
	"fmt"
	"os"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts/resource/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type resourcePluginConverter struct {
	externalPlugin v1.ReadWriteResourcePluginContract
	scheme         *runtime.Scheme
}

func (r *resourcePluginConverter) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *constructorruntime.Resource) (identity runtime.Identity, err error) {
	request := &v1.GetIdentityRequest[runtime.Typed]{
		Typ: resource.Access,
	}

	result, err := r.externalPlugin.GetIdentity(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to get identity: %w", err)
	}

	return result.Identity, nil
}

func (r *resourcePluginConverter) DownloadResource(ctx context.Context, resource *descriptor.Resource, credentials map[string]string) (content blob.ReadOnlyBlob, err error) {
	resources, err := descriptor.ConvertToV2Resources(r.scheme, []descriptor.Resource{*resource})
	if err != nil {
		return nil, fmt.Errorf("failed to convert resource: %w", err)
	}

	request := &v1.GetGlobalResourceRequest{
		Resource: &resources[0],
	}
	result, err := r.externalPlugin.GetGlobalResource(ctx, request, credentials)
	if err != nil {
		return nil, err
	}

	rBlob, err := r.createBlobData(result.Location)
	if err != nil {
		return nil, fmt.Errorf("failed to create blob data: %w", err)
	}

	return rBlob, nil
}

func (r *resourcePluginConverter) createBlobData(location types.Location) (blob.ReadOnlyBlob, error) {
	if location.LocationType == types.LocationTypeLocalFile {
		file, err := os.Open(location.Value)
		if err != nil {
			return nil, err
		}

		fileBlob, err := filesystem.GetBlobFromOSPath(file.Name())
		if err != nil {
			return nil, err
		}

		return fileBlob, nil
	}

	return nil, fmt.Errorf("unsupported location type: %s", location.LocationType)
}

var _ Repository = (*resourcePluginConverter)(nil)

func (r *ResourceRegistry) externalToResourcePluginConverter(plugin v1.ReadWriteResourcePluginContract, scheme *runtime.Scheme) *resourcePluginConverter {
	return &resourcePluginConverter{
		externalPlugin: plugin,
		scheme:         scheme,
	}
}
