package internal

import (
	"context"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/cli/internal/reference/compref"
)

type helmChartProcessor struct{}

func (h helmChartProcessor) ShouldUploadAsOCIArtifact(ctx context.Context, resource v2.Resource, toSpec runtime.Typed, access runtime.Typed, uploadType UploadType) (bool, error) {
	// TODO implement me
	panic("implement me")
}

func (h helmChartProcessor) Process(ctx context.Context, resource v2.Resource, id string, ref *compref.Ref, tgd *v1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int, uploadAsOCIArtifact bool) error {
	// TODO implement me
	panic("implement me")
}
