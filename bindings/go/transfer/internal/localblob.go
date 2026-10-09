package internal

import (
	"fmt"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

// processLocalBlob fetches a local blob from the source. With an empty ociImageReference it
// embeds the blob as a local blob in the target; otherwise it pushes the blob (an OCI
// manifest) as a separate OCI artifact to ociImageReference.
func processLocalBlob(resource descriptorv2.Resource, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int, ociImageReference string) error {
	component := val.Descriptor.Component.Name
	version := val.Descriptor.Component.Version
	sourceRepo := val.SourceRepository

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

	getLocalResourceType, err := chooseGetLocalResourceType(sourceRepo)
	if err != nil {
		return fmt.Errorf("choosing get local resource type for source repository: %w", err)
	}

	sourceRepoUnstructured, err := asUnstructured(sourceRepo)
	if err != nil {
		return fmt.Errorf("cannot convert source repository spec to unstructured: %w", err)
	}

	// Create GetLocalResource transformation
	getResourceTransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type:  getLocalResourceType,
			ID:    getResourceID,
			Label: getLabel(&val.Descriptor.Component, resource.Name),
		},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"repository":       sourceRepoUnstructured.Data,
			"component":        component,
			"version":          version,
			"resourceIdentity": resourceIdentityMap,
		}},
	}
	tgd.Transformations = append(tgd.Transformations, getResourceTransform)

	var addResourceTransform transformv1alpha1.GenericTransformation
	if ociImageReference == "" {
		toRepo, err := asUnstructured(toSpec)
		if err != nil {
			return fmt.Errorf("cannot convert target spec to unstructured: %w", err)
		}

		addLocalResourceType, err := chooseAddLocalResourceType(toSpec)
		if err != nil {
			return fmt.Errorf("choosing add local resource type for target repository: %w", err)
		}

		// Create AddLocalResource transformation
		addResourceTransform = transformv1alpha1.GenericTransformation{
			TransformationMeta: meta.TransformationMeta{
				Type:  addLocalResourceType,
				ID:    addResourceID,
				Label: addLabel(&val.Descriptor.Component, resource.Name, "LocalBlob", toSpec),
			},
			Spec: &runtime.Unstructured{Data: map[string]any{
				"repository": toRepo.Data,
				"component":  component,
				"version":    version,
				"resource":   fmt.Sprintf("${%s.output.resource}", getResourceID),
				"file":       fmt.Sprintf("${%s.output.file}", getResourceID),
			}},
		}
	} else {
		addResourceTransform = ociAddArtifact(addResourceID, getResourceID, ociImageReference,
			addLabel(&val.Descriptor.Component, resource.Name, "OCIArtifact", toSpec))
	}
	tgd.Transformations = append(tgd.Transformations, addResourceTransform)
	// Track this resource's transformation
	resourceTransformIDs[i] = addResourceID
	return nil
}

// uploadAsLocalResource creates an AddLocalResource transformation that embeds the fetched
// blob as a local resource (localBlob access) in the target repository. It is independent of
// the source content type: OCI artifacts, helm charts and wget downloads all transfer by value
// through this path. The concrete transformation type is chosen from the target repository
// (OCI registry or CTF) via chooseAddLocalResourceType.
// It uses the output of the preceding Get transformation to populate the fields of the
// AddLocalResource transformation, ensuring that the same resource is referenced and uploaded.
func uploadAsLocalResource(toSpec runtime.Typed, component, version, addResourceID, getResourceID, referenceName, label string) (transformv1alpha1.GenericTransformation, error) {
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
			Type:  addLocalResourceType,
			ID:    addResourceID,
			Label: label,
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
				"access": map[string]any{
					"type":          descriptor.GetLocalBlobAccessType().String(),
					"referenceName": referenceName,
				},
				"digest":        fmt.Sprintf("${has(%s.output.resource.digest) ? %s.output.resource.digest : null}", getResourceID, getResourceID),
				"labels":        fmt.Sprintf("${has(%s.output.resource.labels) ? %s.output.resource.labels  : []}", getResourceID, getResourceID),
				"extraIdentity": fmt.Sprintf("${has(%s.output.resource.extraIdentity) ? %s.output.resource.extraIdentity  : {}}", getResourceID, getResourceID),
				"srcRefs":       fmt.Sprintf("${has(%s.output.resource.srcRefs) ? %s.output.resource.srcRefs  : []}", getResourceID, getResourceID),
			},
			"file": fmt.Sprintf("${%s.output.file}", getResourceID),
		}},
	}
	return addResourceTransform, nil
}

// streamAsLocalResource creates a single fused streaming local-resource
// transformation that downloads a by-value source (wget/s3) and embeds it as a
// local blob in the target in one node. Fusing the download and the add keeps the
// stream from crossing a graph node boundary, so the content is pushed straight
// into the target registry via the OCI chunked streaming push with no temporary
// file. The transformer internally falls back to a buffered add when the source
// repository is not streaming-capable, so correctness is preserved either way.
//
// The source resource (with its original wget/s3 access and digest) is embedded
// directly in the spec; the transformer derives the LocalBlob access itself.
func streamAsLocalResource(toSpec runtime.Typed, component, version, addResourceID string, resource descriptorv2.Resource, label string) (transformv1alpha1.GenericTransformation, error) {
	streamType, err := chooseStreamLocalResourceType(toSpec)
	if err != nil {
		return transformv1alpha1.GenericTransformation{}, fmt.Errorf("choosing stream local resource type for target repository: %w", err)
	}

	toRepo, err := asUnstructured(toSpec)
	if err != nil {
		return transformv1alpha1.GenericTransformation{}, fmt.Errorf("cannot convert target spec to unstructured: %w", err)
	}

	spec, err := runtime.UnstructuredFromMixedData(map[string]any{
		"repository": toRepo.Data,
		"component":  component,
		"version":    version,
		"resource":   resource,
	})
	if err != nil {
		return transformv1alpha1.GenericTransformation{}, fmt.Errorf("cannot create unstructured spec for stream local resource transformation: %w", err)
	}

	return transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type:  streamType,
			ID:    addResourceID,
			Label: label,
		},
		Spec: spec,
	}, nil
}
