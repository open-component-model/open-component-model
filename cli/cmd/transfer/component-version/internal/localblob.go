package internal

import (
	"fmt"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ocirepo "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
	"ocm.software/open-component-model/cli/internal/reference/compref"
)

func processLocalBlob(resource descriptorv2.Resource, access *descriptorv2.LocalBlob, id string, ref *compref.Ref, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int, uploadAsOCIArtifact bool) error {
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

	var addResourceTransform transformv1alpha1.GenericTransformation
	if !uploadAsOCIArtifact {
		// Create AddLocalResource transformation
		addResourceTransform = transformv1alpha1.GenericTransformation{
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
	} else {
		var ociSpec ocirepo.Repository
		if err := Scheme.Convert(toSpec, &ociSpec); err != nil {
			return err
		}
		targetRepoBaseURL := ociSpec.BaseUrl
		if ociSpec.SubPath != "" {
			targetRepoBaseURL = targetRepoBaseURL + "/" + ociSpec.SubPath
		}
		addResourceTransform = transformv1alpha1.GenericTransformation{
			TransformationMeta: meta.TransformationMeta{
				Type: runtime.NewVersionedType(ociv1alpha1.AddOCIArtifactType, ociv1alpha1.Version),
				ID:   addResourceID,
			},
			Spec: &runtime.Unstructured{Data: map[string]any{
				"resource": map[string]any{
					"name":     fmt.Sprintf("${%s.output.resource.name}", getResourceID),
					"version":  fmt.Sprintf("${%s.output.resource.version}", getResourceID),
					"type":     fmt.Sprintf("${%s.output.resource.type}", getResourceID),
					"relation": fmt.Sprintf("${%s.output.resource.relation}", getResourceID),
					"access": map[string]interface{}{
						"type":           runtime.NewVersionedType(ociv1.LegacyType, ociv1.LegacyTypeVersion).String(),
						"imageReference": fmt.Sprintf("%s/${%s.output.resource.access.referenceName}", targetRepoBaseURL, getResourceID),
					},
					"digest":        fmt.Sprintf("${%s.output.resource.digest}", getResourceID),
					"labels":        fmt.Sprintf("${has(%s.output.resource.labels) ? %s.output.resource.labels  : []}", getResourceID, getResourceID),
					"extraIdentity": fmt.Sprintf("${has(%s.output.resource.extraIdentity) ? %s.output.resource.extraIdentity  : {}}", getResourceID, getResourceID),
					"srcRefs":       fmt.Sprintf("${has(%s.output.resource.srcRefs) ? %s.output.resource.srcRefs  : []}", getResourceID, getResourceID),
				},
				"file": fmt.Sprintf("${%s.output.file}", getResourceID),
			}},
		}
	}
	tgd.Transformations = append(tgd.Transformations, addResourceTransform)
	// Track this resource's transformation
	resourceTransformIDs[i] = addResourceID
	return nil
}
