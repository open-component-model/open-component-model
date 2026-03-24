package internal

import (
	"encoding/json"
	"fmt"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ocirepo "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

func processOCIArtifact(resource descriptorv2.Resource, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int, uploadAsOCIArtifact bool) error {
	component := val.Descriptor.Component.Name
	version := val.Descriptor.Component.Version

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
	referenceName, err := getReferenceName(ociAccess.ImageReference)
	if err != nil {
		return fmt.Errorf("cannot get reference name: %w", err)
	}

	// Create GetOCIArtifact transformation
	unstructured, err := runtime.UnstructuredFromMixedData(map[string]any{
		"resource": resource,
	})
	if err != nil {
		return fmt.Errorf("cannot create unstructured spec for GetOCIArtifact transformation: %w", err)
	}

	getArtifactTransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: ociv1alpha1.GetOCIArtifactV1alpha1,
			ID:   getResourceID,
		},
		Spec: unstructured,
	}
	tgd.Transformations = append(tgd.Transformations, getArtifactTransform)

	// Create AddLocalResource transformation
	var addResourceTransform transformv1alpha1.GenericTransformation
	if uploadAsOCIArtifact {
		if addResourceTransform, err = ociUploadAsArtifact(toSpec, addResourceID, getResourceID, staticReferenceName(referenceName)); err != nil {
			return fmt.Errorf("failed to create oci upload transformation: %w", err)
		}
	} else {
		if addResourceTransform, err = ociUploadAsLocalResource(toSpec, component, version, addResourceID, getResourceID, staticReferenceName(referenceName)); err != nil {
			return fmt.Errorf("failed to create local resource upload transformation: %w", err)
		}
	}

	tgd.Transformations = append(tgd.Transformations, addResourceTransform)

	// Track this resource's transformation
	resourceTransformIDs[i] = addResourceID

	return nil
}

// ociUploadAsLocalResource creates an AddLocalResource transformation that uploads the OCI artifact as a local resource to the target repository.
// It uses the output of the GetOCIArtifact transformation to populate the fields of the AddLocalResource transformation, ensuring that the same resource is referenced and uploaded.
func ociUploadAsLocalResource(toSpec runtime.Typed, component, version, addResourceID, getResourceID string, referenceName referenceNameOption) (transformv1alpha1.GenericTransformation, error) {
	addLocalResourceType, err := chooseAddLocalResourceType(toSpec)
	if err != nil {
		return transformv1alpha1.GenericTransformation{}, fmt.Errorf("choosing add local resource type for target repository: %w", err)
	}

	toRepo, err := asUnstructured(toSpec)
	if err != nil {
		return transformv1alpha1.GenericTransformation{}, fmt.Errorf("cannot convert target spec to unstructured: %w", err)
	}

	addResourceTransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: addLocalResourceType,
			ID:   addResourceID,
		},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"repository": toRepo.Data,
			"component":  component,
			"version":    version,
			"resource": map[string]any{
				"name":     fmt.Sprintf("${%s.output.resource.name}", getResourceID),
				"version":  fmt.Sprintf("${%s.output.resource.version}", getResourceID),
				"type":     fmt.Sprintf("${%s.output.resource.type}", getResourceID),
				"relation": fmt.Sprintf("${%s.output.resource.relation}", getResourceID),
				"access": map[string]interface{}{
					"type":          descriptor.GetLocalBlobAccessType().String(),
					"referenceName": referenceName(""),
				},
				"digest":        fmt.Sprintf("${%s.output.resource.digest}", getResourceID),
				"labels":        fmt.Sprintf("${has(%s.output.resource.labels) ? %s.output.resource.labels  : []}", getResourceID, getResourceID),
				"extraIdentity": fmt.Sprintf("${has(%s.output.resource.extraIdentity) ? %s.output.resource.extraIdentity  : {}}", getResourceID, getResourceID),
				"srcRefs":       fmt.Sprintf("${has(%s.output.resource.srcRefs) ? %s.output.resource.srcRefs  : []}", getResourceID, getResourceID),
			},
			"file": fmt.Sprintf("${%s.output.file}", getResourceID),
		}},
	}
	return addResourceTransform, nil
}

// referenceNameOption is an option providing targetRepoBaseURL to construct the reference name for the OCI artifact in the target repository.
type referenceNameOption func(targetRepoBaseURL string) string

// staticReferenceName returns a referenceNameOption that constructs the reference name by combining the target repository base URL and the given reference name.
// If the target repository base URL is empty, it returns the given reference name as is.
func staticReferenceName(referenceName string) referenceNameOption {
	return func(targetRepoBaseURL string) string {
		if targetRepoBaseURL == "" {
			return referenceName
		}
		return fmt.Sprintf("%s/%s", targetRepoBaseURL, referenceName)
	}
}

// imageReferenceFromAccess returns a referenceNameOption that constructs the reference name by combining the target repository base URL and the image reference from the OCI access.
// If the target repository base URL is empty, it returns the image reference from the OCI access as is.
// id is the transformation ID of a previous step. The output of that step is expected to contain a resource with an OCI access, and the imageReference from that access will be used.
// The generated CEL expression will look like "${id.output.resource.access.imageReference}"
func imageReferenceFromAccess(id string) referenceNameOption {
	return func(targetRepoBaseURL string) string {
		if targetRepoBaseURL == "" {
			return fmt.Sprintf("${%s.output.resource.access.imageReference}", id)
		}
		return fmt.Sprintf("%s/${%s.output.resource.access.imageReference}", targetRepoBaseURL, id)
	}
}

// ociUploadAsArtifact creates an AddOCIArtifact transformation that uploads the OCI artifact to the target repository as an OCI artifact.
// It constructs the target image reference from the toSpec and referenceName, and uses the output of the GetOCIArtifact transformation to populate the fields of the AddOCIArtifact transformation, ensuring that the same resource is referenced and uploaded.
func ociUploadAsArtifact(toSpec runtime.Typed, addResourceID string, getResourceID string, referenceName referenceNameOption) (transformv1alpha1.GenericTransformation, error) {
	var ociSpec ocirepo.Repository
	if err := scheme.Convert(toSpec, &ociSpec); err != nil {
		return transformv1alpha1.GenericTransformation{}, err
	}
	targetRepoBaseURL := ociSpec.BaseUrl
	if ociSpec.SubPath != "" {
		targetRepoBaseURL = targetRepoBaseURL + "/" + ociSpec.SubPath
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
					"imageReference": referenceName(targetRepoBaseURL),
				},
				"digest":        fmt.Sprintf("${%s.output.resource.digest}", getResourceID),
				"labels":        fmt.Sprintf("${has(%s.output.resource.labels) ? %s.output.resource.labels  : []}", getResourceID, getResourceID),
				"extraIdentity": fmt.Sprintf("${has(%s.output.resource.extraIdentity) ? %s.output.resource.extraIdentity  : {}}", getResourceID, getResourceID),
				"srcRefs":       fmt.Sprintf("${has(%s.output.resource.srcRefs) ? %s.output.resource.srcRefs  : []}", getResourceID, getResourceID),
			},
			"file": fmt.Sprintf("${%s.output.file}", getResourceID),
		}},
	}
	return addResourceTransform, nil
}
