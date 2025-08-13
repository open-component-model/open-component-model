package input

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	descriptorruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/input/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/blobs"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var _ constructor.ResourceInputMethod = (*resourceInputPluginConverter)(nil)

type resourceInputPluginConverter struct {
	externalPlugin v1.ResourceInputPluginContract
	scheme         *runtime.Scheme
}

func (r *resourceInputPluginConverter) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *constructorruntime.Resource) (runtime.Identity, error) {
	request := &v1.GetIdentityRequest[runtime.Typed]{}
	if resource.HasAccess() {
		request.Typ = resource.Access
	} else if resource.HasInput() {
		request.Typ = resource.Input
	}

	result, err := r.externalPlugin.GetIdentity(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to get identity: %w", err)
	}

	return result.Identity, nil
}

func (r *resourceInputPluginConverter) ProcessResource(ctx context.Context, resource *constructorruntime.Resource, credentials map[string]string) (*constructor.ResourceInputMethodResult, error) {
	// Convert constructor runtime resource to descriptor resource
	descriptorResource := constructorruntime.ConvertToDescriptorResource(resource)
	// Convert descriptor resource to v2 resource
	convert, err := descriptorruntime.ConvertToV2Resource(r.scheme, descriptorResource)
	if err != nil {
		return nil, fmt.Errorf("failed to convert resource: %w", err)
	}
	request := &v1.ProcessResourceInputRequest{
		Resource: convert,
	}
	result, err := r.externalPlugin.ProcessResource(ctx, request, credentials)
	if err != nil {
		return nil, err
	}

	rBlob, err := blobs.CreateBlobData(*result.Location)
	if err != nil {
		return nil, fmt.Errorf("failed to create blob data: %w", err)
	}

	// Convert v2 resource back to descriptor resource
	descriptorResources := descriptorruntime.ConvertFromV2Resources([]v2.Resource{*result.Resource})
	if len(descriptorResources) == 0 {
		return nil, fmt.Errorf("conversion resulted in empty resource list")
	}
	// Convert descriptor resource to constructor runtime resource
	converted := constructorruntime.ConvertFromDescriptorResource(&descriptorResources[0])
	descResource := constructorruntime.ConvertToDescriptorResource(converted)
	resourceInputMethodResult := &constructor.ResourceInputMethodResult{
		ProcessedResource: descResource,
		ProcessedBlobData: rBlob,
	}

	return resourceInputMethodResult, nil
}

func (r *RepositoryRegistry) externalToResourceInputPluginConverter(plugin v1.ResourceInputPluginContract, scheme *runtime.Scheme) *resourceInputPluginConverter {
	return &resourceInputPluginConverter{
		externalPlugin: plugin,
		scheme:         scheme,
	}
}
