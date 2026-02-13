package internal

import (
	"encoding/json"
	"fmt"
	"strings"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ocirepo "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
	"ocm.software/open-component-model/cli/internal/reference/compref"
)

func processOCIArtifact(resource descriptorv2.Resource, id string, ref *compref.Ref, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int, uploadAsOCIArtifact bool) error {
	resourceIdentity := resource.ToIdentity()
	resourceID := identityToTransformationID(resourceIdentity)
	getResourceID := fmt.Sprintf("%sGet%s", id, resourceID)
	addResourceID := fmt.Sprintf("%sAdd%s", id, resourceID)

	var ociAccess ociv1.OCIImage
	if err := json.Unmarshal(resource.Access.Data, &ociAccess); err != nil {
		return fmt.Errorf("cannot unmarshal OCI access: %w", err)
	}

	// e.g. ghcr.io/open-component-model/helmexample/charts/mariadb:12.2.7
	// strip the domain part and keep the rest
	referenceName, err := GetReferenceName(ociAccess)
	if err != nil {
		return fmt.Errorf("cannot get reference name: %w", err)
	}

	jRes, err := json.Marshal(resource)
	if err != nil {
		return fmt.Errorf("cannot marshal resource: %w", err)
	}
	var resourceMap map[string]any
	if err := json.Unmarshal(jRes, &resourceMap); err != nil {
		return fmt.Errorf("cannot unmarshal resource to map: %w", err)
	}

	// Create GetOCIArtifact transformation
	getArtifactTransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: ociv1alpha1.GetOCIArtifactV1alpha1,
			ID:   getResourceID,
		},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"resource": resourceMap,
		}},
	}
	tgd.Transformations = append(tgd.Transformations, getArtifactTransform)

	// Create AddLocalResource transformation
	var addResourceTransform transformv1alpha1.GenericTransformation
	if uploadAsOCIArtifact {
		addResourceTransform = ociUploadAsArtifact(toSpec, addResourceID, getResourceID, referenceName)
	} else {
		addResourceTransform = ociUploadAsLocalResource(toSpec, ref, addResourceID, getResourceID, referenceName)
	}
	tgd.Transformations = append(tgd.Transformations, addResourceTransform)

	// Track this resource's transformation
	resourceTransformIDs[i] = addResourceID

	return nil
}

// ociUploadAsLocalResource creates an AddLocalResource transformation that uploads the OCI artifact as a local resource to the target repository.
// It uses the output of the GetOCIArtifact transformation to populate the fields of the AddLocalResource transformation, ensuring that the same resource is referenced and uploaded.
func ociUploadAsLocalResource(toSpec runtime.Typed, ref *compref.Ref, addResourceID string, getResourceID string, referenceName string) transformv1alpha1.GenericTransformation {
	addResourceTransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: ChooseAddLocalResourceType(toSpec),
			ID:   addResourceID,
		},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"repository": AsUnstructured(toSpec).Data,
			"component":  ref.Component,
			"version":    ref.Version,
			"resource": map[string]any{
				"name":     fmt.Sprintf("${%s.output.resource.name}", getResourceID),
				"version":  fmt.Sprintf("${%s.output.resource.version}", getResourceID),
				"type":     fmt.Sprintf("${%s.output.resource.type}", getResourceID),
				"relation": fmt.Sprintf("${%s.output.resource.relation}", getResourceID),
				"access": map[string]interface{}{
					"type":          descriptor.GetLocalBlobAccessType().String(),
					"referenceName": referenceName,
					"labels":        fmt.Sprintf("${has(%s.output.resource.labels) ? %s.output.resource.labels  : []}", getResourceID, getResourceID),
					"extraIdentity": fmt.Sprintf("${has(%s.output.resource.extraIdentity) ? %s.output.resource.extraIdentity  : {}}", getResourceID, getResourceID),
					"srcRefs":       fmt.Sprintf("${has(%s.output.resource.srcRefs) ? %s.output.resource.srcRefs  : []}", getResourceID, getResourceID),
				},
				"digest": fmt.Sprintf("${%s.output.resource.digest}", getResourceID),
			},
			"file": fmt.Sprintf("${%s.output.file}", getResourceID),
		}},
	}
	return addResourceTransform
}

// ociUploadAsArtifact creates an AddOCIArtifact transformation that uploads the OCI artifact to the target repository as an OCI artifact.
// It constructs the target image reference from the toSpec and referenceName, and uses the output of the GetOCIArtifact transformation to populate the fields of the AddOCIArtifact transformation, ensuring that the same resource is referenced and uploaded.
func ociUploadAsArtifact(toSpec runtime.Typed, addResourceID string, getResourceID string, referenceName string) transformv1alpha1.GenericTransformation {
	// Construct target Image Reference from toSpec and referenceName
	var targetImageRef string
	// Default to referenceName if we can't determine the target repo
	targetImageRef = referenceName

	if toSpec != nil {
		raw, err := json.Marshal(toSpec)
		if err == nil {
			var repoSpec ocirepo.Repository
			if err := json.Unmarshal(raw, &repoSpec); err == nil && repoSpec.BaseUrl != "" {
				targetRepoURL := repoSpec.BaseUrl
				if repoSpec.SubPath != "" {
					targetRepoURL = targetRepoURL + "/" + repoSpec.SubPath
				}
				targetImageRef = fmt.Sprintf("%s/%s", strings.TrimRight(targetRepoURL, "/"), strings.TrimLeft(referenceName, "/"))
			}
		}
	}

	addResourceTransform := transformv1alpha1.GenericTransformation{
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
					"imageReference": targetImageRef,
					"labels":         fmt.Sprintf("${has(%s.output.resource.labels) ? %s.output.resource.labels  : []}", getResourceID, getResourceID),
					"extraIdentity":  fmt.Sprintf("${has(%s.output.resource.extraIdentity) ? %s.output.resource.extraIdentity  : {}}", getResourceID, getResourceID),
					"srcRefs":        fmt.Sprintf("${has(%s.output.resource.srcRefs) ? %s.output.resource.srcRefs  : []}", getResourceID, getResourceID),
				},
				"digest": fmt.Sprintf("${%s.output.resource.digest}", getResourceID),
			},
			"file": fmt.Sprintf("${%s.output.file}", getResourceID),
		}},
	}
	return addResourceTransform
}
