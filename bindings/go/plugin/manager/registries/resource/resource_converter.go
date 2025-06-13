package resource

import (
	"context"
	"fmt"
	"io"
	"os"

	"ocm.software/open-component-model/bindings/go/blob"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
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

func (r *resourcePluginConverter) UploadResource(ctx context.Context, targetAccess runtime.Typed, resource *descriptor.Resource, content blob.ReadOnlyBlob, credentials map[string]string) (*descriptor.Resource, error) {
	file, err := os.CreateTemp("", "resource")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}()

	reader, err := content.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("failed to read blob: %w", err)
	}
	defer func() {
		_ = reader.Close()
	}()
	if _, err := io.Copy(file, reader); err != nil {
		return nil, fmt.Errorf("failed to copy blob: %w", err)
	}

	convert, err := descriptor.ConvertToV2Resources(r.scheme, []descriptor.Resource{*resource})
	if err != nil {
		return nil, fmt.Errorf("failed to convert resource: %w", err)
	}

	request := &v1.AddGlobalResourceRequest{
		ResourceLocation: types.Location{
			LocationType: types.LocationTypeLocalFile,
			Value:        file.Name(),
		},
		Resource: &convert[0],
	}

	result, err := r.externalPlugin.AddGlobalResource(ctx, request, credentials)
	if err != nil {
		return nil, fmt.Errorf("failed to add global resource: %w", err)
	}

	convertedResult := descriptor.ConvertFromV2Resources([]descriptorv2.Resource{*result.Resource})
	return &convertedResult[0], nil
}

func (r *resourcePluginConverter) createBlobData(location types.Location) (blob.ReadOnlyBlob, error) {
	var rBlob blob.ReadOnlyBlob

	if location.LocationType == types.LocationTypeLocalFile {
		file, err := os.Open(location.Value)
		if err != nil {
			return nil, err
		}

		rBlob = blob.NewDirectReadOnlyBlob(file)
	}

	return rBlob, nil
}

var _ Repository = (*resourcePluginConverter)(nil)

func (r *ResourceRegistry) externalToResourcePluginConverter(plugin v1.ReadWriteResourcePluginContract, scheme *runtime.Scheme) *resourcePluginConverter {
	return &resourcePluginConverter{
		externalPlugin: plugin,
		scheme:         scheme,
	}
}
