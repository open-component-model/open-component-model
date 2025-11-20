package digestprocessor

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/constructor"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts/digestprocessor/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type resourceDigestProcessorPluginConverter struct {
	externalPlugin v1.ResourceDigestProcessorContract
	scheme         *runtime.Scheme
}

func (r *resourceDigestProcessorPluginConverter) GetResourceDigestProcessorCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (identity runtime.Identity, err error) {
	request := &v1.GetIdentityRequest[runtime.Typed]{
		Typ: resource.Access,
	}

	result, err := r.externalPlugin.GetIdentity(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to get identity: %w", err)
	}

	return result.Identity, nil
}

func (r *resourceDigestProcessorPluginConverter) ProcessResourceDigest(ctx context.Context, resource *descriptor.Resource, credentials map[string]string) (*descriptor.Resource, error) {
	resources, err := descriptor.ConvertToV2Resources(r.scheme, []descriptor.Resource{*resource})
	if err != nil {
		return nil, fmt.Errorf("failed to convert resource: %w", err)
	}

	request := &v1.ProcessResourceDigestRequest{
		Resource: &resources[0],
	}
	response, err := r.externalPlugin.ProcessResourceDigest(ctx, request, credentials)
	if err != nil {
		return nil, fmt.Errorf("failed to process resource digest: %w", err)
	}

	convert := descriptor.ConvertFromV2Resources([]descriptorv2.Resource{*response.Resource})
	return &convert[0], nil
}

var _ constructor.ResourceDigestProcessor = (*resourceDigestProcessorPluginConverter)(nil)

func (r *RepositoryRegistry) externalToResourceDigestProcessorPluginConverter(plugin v1.ResourceDigestProcessorContract, scheme *runtime.Scheme) *resourceDigestProcessorPluginConverter {
	return &resourceDigestProcessorPluginConverter{
		externalPlugin: plugin,
		scheme:         scheme,
	}
}
