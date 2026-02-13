package internal

import (
	"fmt"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
	"ocm.software/open-component-model/cli/internal/reference/compref"
)

func processLocalBlob(resource descriptorv2.Resource, id string, ref *compref.Ref, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int) {
	// Generate transformation IDs
	resourceIdentity := resource.ToIdentity()
	resourceID := identityToTransformationID(resourceIdentity)
	getResourceID := fmt.Sprintf("%sGet%s", id, resourceID)
	addResourceID := fmt.Sprintf("%sAdd%s", id, resourceID)

	// Convert resourceIdentity to map[string]any for deep copy compatibility
	resourceIdentityMap := make(map[string]any)
	for k, v := range resourceIdentity {
		resourceIdentityMap[k] = v
	}

	// Create GetLocalResource transformation
	getResourceTransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: ChooseGetLocalResourceType(ref.Repository),
			ID:   getResourceID,
		},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"repository":       AsUnstructured(ref.Repository).Data,
			"component":        ref.Component,
			"version":          ref.Version,
			"resourceIdentity": resourceIdentityMap,
		}},
	}
	tgd.Transformations = append(tgd.Transformations, getResourceTransform)

	// Create AddLocalResource transformation
	addResourceTransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: ChooseAddLocalResourceType(toSpec),
			ID:   addResourceID,
		},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"repository": AsUnstructured(toSpec).Data,
			"component":  ref.Component,
			"version":    ref.Version,
			"resource":   fmt.Sprintf("${%s.output.resource}", getResourceID),
			"file":       fmt.Sprintf("${%s.output.file}", getResourceID),
		}},
	}
	tgd.Transformations = append(tgd.Transformations, addResourceTransform)

	// Track this resource's transformation
	resourceTransformIDs[i] = addResourceID
}
