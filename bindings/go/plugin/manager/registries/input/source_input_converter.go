package input

import (
	"context"
	"fmt"
	"os"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/input/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type sourceInputPluginConverter struct {
	externalPlugin v1.SourceInputPluginContract
	scheme         *runtime.Scheme
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

	rBlob, err := r.createBlobData(result.Location)
	if err != nil {
		return nil, fmt.Errorf("failed to create blob data: %w", err)
	}

	converted := constructorruntime.ConvertFromV1Source(result.Source)
	descSource := constructorruntime.ConvertToDescriptorSource(&converted)
	resourceInputMethodResult := &constructor.SourceInputMethodResult{
		ProcessedSource:   descSource,
		ProcessedBlobData: rBlob,
	}

	return resourceInputMethodResult, nil
}

func (r *sourceInputPluginConverter) createBlobData(location *types.Location) (blob.Blob, error) {
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

func (r *RepositoryRegistry) externalToSourceInputPluginConverter(plugin v1.SourceInputPluginContract, scheme *runtime.Scheme) *sourceInputPluginConverter {
	return &sourceInputPluginConverter{
		externalPlugin: plugin,
		scheme:         scheme,
	}
}
