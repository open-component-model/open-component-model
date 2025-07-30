package main

import (
	"context"
	"fmt"
	"os"

	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	"ocm.software/open-component-model/bindings/go/input/helm"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/input/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

// processHelmResource wraps the helm.InputMethod to process resources
func processHelmResource(ctx context.Context, request *v1.ProcessResourceInputRequest, credentials map[string]string) (*v1.ProcessResourceInputResponse, error) {
	resource := &constructorruntime.Resource{
		AccessOrInput: constructorruntime.AccessOrInput{
			Input: request.Resource.Input,
		},
	}

	helmMethod := &helm.InputMethod{}

	result, err := helmMethod.ProcessResource(ctx, resource, credentials)
	if err != nil {
		return nil, fmt.Errorf("helm input method failed: %w", err)
	}

	// TODO: Convert the blob result to a file location
	// For now, create a stub temp file
	tmp, err := os.CreateTemp("", "helm-resource-*.tar.gz")
	if err != nil {
		return nil, fmt.Errorf("error creating temp file: %w", err)
	}
	_ = tmp.Close()

	// TODO: Write result.ProcessedBlobData to temp file
	// TODO: Extract metadata from Chart.yaml in the helm spec
	_ = result

	return &v1.ProcessResourceInputResponse{
		Resource: &constructorv1.Resource{
			ElementMeta: constructorv1.ElementMeta{
				ObjectMeta: constructorv1.ObjectMeta{
					Name:    "helm-chart", // TODO: Extract from Chart.yaml
					Version: "v1.0.0",     // TODO: Extract from Chart.yaml
				},
			},
			Type:     "helmChart",
			Relation: "local",
		},
		Location: &types.Location{
			LocationType: types.LocationTypeLocalFile,
			Value:        tmp.Name(),
		},
	}, nil
}

// processHelmSource wraps the helm.InputMethod to process sources
func processHelmSource(ctx context.Context, request *v1.ProcessSourceInputRequest, credentials map[string]string) (*v1.ProcessSourceInputResponse, error) {
	// Convert plugin request to internal format
	// Note: helm.InputMethod doesn't currently have ProcessSource,
	// but we can use ProcessResource and adapt the result
	resource := &constructorruntime.Resource{
		AccessOrInput: constructorruntime.AccessOrInput{
			Input: request.Source.Input,
		},
	}

	helmMethod := &helm.InputMethod{}
	result, err := helmMethod.ProcessResource(ctx, resource, nil)
	if err != nil {
		return nil, fmt.Errorf("helm input method failed: %w", err)
	}

	// TODO: Convert the blob result to a file location
	tmp, err := os.CreateTemp("", "helm-source-*.tar.gz")
	if err != nil {
		return nil, fmt.Errorf("error creating temp file: %w", err)
	}
	_ = tmp.Close()

	// TODO: Write result.ProcessedBlobData to temp file
	// TODO: Extract metadata from Chart.yaml in the helm spec
	_ = result

	return &v1.ProcessSourceInputResponse{
		Source: &constructorv1.Source{
			ElementMeta: constructorv1.ElementMeta{
				ObjectMeta: constructorv1.ObjectMeta{
					Name:    "helm-source", // TODO: Extract from Chart.yaml
					Version: "v1.0.0",      // TODO: Extract from Chart.yaml
				},
			},
			Type: "helmChart",
		},
		Location: &types.Location{
			LocationType: types.LocationTypeLocalFile,
			Value:        tmp.Name(),
		},
	}, nil
}
