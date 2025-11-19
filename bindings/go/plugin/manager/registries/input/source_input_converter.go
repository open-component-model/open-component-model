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

type sourceInputPluginConverter struct {
	externalPlugin v1.SourceInputPluginContract
	scheme         *runtime.Scheme
}

func (r *sourceInputPluginConverter) GetInputMethodScheme() *runtime.Scheme {
	return r.scheme
}

func (r *sourceInputPluginConverter) GetSourceCredentialConsumerIdentity(ctx context.Context, resource *constructorruntime.Source) (runtime.Identity, error) {
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

func (r *sourceInputPluginConverter) ProcessSource(ctx context.Context, source *constructorruntime.Source, credentials map[string]string) (*constructor.SourceInputMethodResult, error) {
	convert, err := constructorruntime.ConvertToV1Source(source)
	if err != nil {
		return nil, fmt.Errorf("failed to convert source: %w", err)
	}
	request := &v1.ProcessSourceInputRequest{
		Source: convert,
	}
	result, err := r.externalPlugin.ProcessSource(ctx, request, credentials)
	if err != nil {
		return nil, err
	}

	rBlob, err := blobs.CreateBlobData(*result.Location)
	if err != nil {
		return nil, fmt.Errorf("failed to create blob data: %w", err)
	}

	// Convert v2 source back to descriptor source
	descriptorSources := descriptorruntime.ConvertFromV2Sources([]v2.Source{*result.Source})
	if len(descriptorSources) == 0 {
		return nil, fmt.Errorf("conversion resulted in empty source list")
	}
	// Convert descriptor source to constructor runtime source
	converted := constructorruntime.ConvertFromDescriptorSource(&descriptorSources[0])
	descSource := constructorruntime.ConvertToDescriptorSource(converted)
	resourceInputMethodResult := &constructor.SourceInputMethodResult{
		ProcessedSource:   descSource,
		ProcessedBlobData: rBlob,
	}

	return resourceInputMethodResult, nil
}

func (r *RepositoryRegistry) externalToSourceInputPluginConverter(plugin v1.SourceInputPluginContract, scheme *runtime.Scheme) *sourceInputPluginConverter {
	return &sourceInputPluginConverter{
		externalPlugin: plugin,
		scheme:         scheme,
	}
}
