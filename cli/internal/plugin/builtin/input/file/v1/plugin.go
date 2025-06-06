package v1

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	inputv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/input/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/input"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type InputRepositoryPlugin struct {
	contracts.EmptyBasePlugin
	resourceInputMethod constructor.ResourceInputMethod
	sourceInputMethod   constructor.SourceInputMethod
	scheme              *runtime.Scheme
}

func (i *InputRepositoryPlugin) GetIdentity(ctx context.Context, typ *inputv1.GetIdentityRequest[runtime.Typed]) (*inputv1.GetIdentityResponse, error) {
	return nil, fmt.Errorf("input repository plugin does not support yet support identity forwarding")
}

func (i *InputRepositoryPlugin) ProcessResource(ctx context.Context, request *inputv1.ProcessResourceInputRequest, credentials map[string]string) (*inputv1.ProcessResourceResponse, error) {
	runtimeResource := constructorruntime.ConvertFromV1Resource(request.Resource)
	response, err := i.resourceInputMethod.ProcessResource(ctx, &runtimeResource, credentials)
	if err != nil {
		return nil, fmt.Errorf("failed to process resource: %w", err)
	}

	if response.ProcessedResource != nil {
		res, err := descriptor.ConvertToV2Resources(i.scheme, []descriptor.Resource{*response.ProcessedResource})
		if err != nil {
			return nil, fmt.Errorf("error converting processed resource: %w", err)
		}
		return &inputv1.ProcessResourceResponse{
			Resource: &res[0],
		}, nil
	}

	path := filepath.Join(os.TempDir(), fmt.Sprintf("ocm-processed-file-input-resource-%d", runtimeResource.ToIdentity().CanonicalHashV1()))
	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("temporary file path %q already exists", path)
	}

	if err := filesystem.CopyBlobToOSPath(response.ProcessedBlobData, path); err != nil {
		return nil, fmt.Errorf("error copying blob to OS path: %w", err)
	}
	return &inputv1.ProcessResourceResponse{
		Location: &types.Location{
			LocationType: types.LocationTypeLocalFile,
			Value:        path,
		},
	}, nil

}

func (i *InputRepositoryPlugin) ProcessSource(ctx context.Context, request *inputv1.ProcessSourceInputRequest, credentials map[string]string) (*inputv1.ProcessSourceResponse, error) {
	runtimeSource := constructorruntime.ConvertFromV1Source(request.Source)
	response, err := i.sourceInputMethod.ProcessSource(ctx, &runtimeSource, credentials)
	if err != nil {
		return nil, fmt.Errorf("failed to process source: %w", err)
	}

	if response.ProcessedSource != nil {
		src, err := descriptor.ConvertToV2Sources(i.scheme, []descriptor.Source{*response.ProcessedSource})
		if err != nil {
			return nil, fmt.Errorf("error converting processed source: %w", err)
		}
		return &inputv1.ProcessSourceResponse{
			Source: &src[0],
		}, nil
	}

	path := filepath.Join(os.TempDir(), fmt.Sprintf("ocm-processed-file-input-source-%d", runtimeSource.ToIdentity().CanonicalHashV1()))
	if err := filesystem.CopyBlobToOSPath(response.ProcessedBlobData, path); err != nil {
		return nil, fmt.Errorf("error copying blob to OS path: %w", err)
	}
	return &inputv1.ProcessSourceResponse{
		Location: &types.Location{
			LocationType: types.LocationTypeLocalFile,
			Value:        path,
		},
	}, nil
}

func Register(
	inputRegistry *input.RepositoryRegistry,
) error {
	plugin := InputRepositoryPlugin{resourceInputMethod: &ResourceInputMethod{}, sourceInputMethod: &SourceInputMethod{}, scheme: scheme}
	return errors.Join(
		input.RegisterInternalInputPlugin(
			scheme,
			inputRegistry,
			&plugin,
			&File{},
		),
	)
}
