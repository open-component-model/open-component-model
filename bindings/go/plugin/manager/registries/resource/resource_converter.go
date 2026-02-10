package resource

import (
	"context"
	"errors"
	"fmt"
	"os"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts/resource/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/blobs"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type resourcePluginConverter struct {
	externalPlugin v1.ReadWriteResourcePluginContract
	scheme         *runtime.Scheme
}

func (r *resourcePluginConverter) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (identity runtime.Identity, err error) {
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

	rBlob, err := blobs.CreateBlobData(result.Location)
	if err != nil {
		return nil, fmt.Errorf("failed to create blob data: %w", err)
	}

	return rBlob, nil
}

func (r *resourcePluginConverter) UploadResource(ctx context.Context, resource *descriptor.Resource, content blob.ReadOnlyBlob, credentials map[string]string) (result *descriptor.Resource, err error) {
	resources, err := descriptor.ConvertToV2Resources(r.scheme, []descriptor.Resource{*resource})
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

	request := &v1.AddGlobalResourceRequest{
		Resource: &resources[0],
		ResourceLocation: types.Location{
			LocationType: types.LocationTypeLocalFile,
			Value:        tmp.Name(),
		},
	}
	res, err := r.externalPlugin.AddGlobalResource(ctx, request, credentials)
	if err != nil {
		return nil, err
	}

	return descriptor.ConvertFromV2Resource(res.Resource), nil
}

var _ Repository = (*resourcePluginConverter)(nil)

func (r *ResourceRegistry) externalToResourcePluginConverter(plugin v1.ReadWriteResourcePluginContract, scheme *runtime.Scheme) *resourcePluginConverter {
	return &resourcePluginConverter{
		externalPlugin: plugin,
		scheme:         scheme,
	}
}
