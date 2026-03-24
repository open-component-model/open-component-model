package internal

import (
	"fmt"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	helmv1alpha1 "ocm.software/open-component-model/bindings/go/helm/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

func processHelm(resource v2.Resource, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int, uploadAsOCIArtifact bool) error {
	resourceIdentity := resource.ToIdentity()
	resourceID := identityToTransformationID(resourceIdentity)
	getResourceID := fmt.Sprintf("%sGet%s", id, resourceID)
	convertResourceID := fmt.Sprintf("%sConvert%s", id, resourceID)
	addResourceID := fmt.Sprintf("%sAdd%s", id, resourceID)

	unstructured, err := runtime.UnstructuredFromMixedData(map[string]any{
		"resource": resource,
	})
	if err != nil {
		return fmt.Errorf("cannot create unstructured spec for GetHelmChartV1alpha1 transformation: %w", err)
	}

	// Create GetHelmChart transformation
	getChartTransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: helmv1alpha1.GetHelmChartV1alpha1,
			ID:   getResourceID,
		},
		Spec: unstructured,
	}
	tgd.Transformations = append(tgd.Transformations, getChartTransform)

	// convert chart to oci artifact transformation
	convertToOCITransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: helmv1alpha1.ConvertHelmToOCIV1alpha1,
			ID:   convertResourceID,
		},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"resource":  fmt.Sprintf("${%s.output.resource}", getResourceID),
			"chartFile": fmt.Sprintf("${%s.output.chartFile}", getResourceID),
			"provFile":  fmt.Sprintf("${%s.output.?provFile}", getResourceID),
		}},
	}
	tgd.Transformations = append(tgd.Transformations, convertToOCITransform)

	// Create upload transformations
	var addResourceTransform transformv1alpha1.GenericTransformation
	if uploadAsOCIArtifact {
		if addResourceTransform, err = ociUploadAsArtifact(toSpec, addResourceID, convertResourceID, imageReferenceFromAccess(convertResourceID)); err != nil {
			return fmt.Errorf("failed to create oci upload transformation: %w", err)
		}
	} else {
		if addResourceTransform, err = ociUploadAsLocalResource(toSpec, val.Descriptor.Component.Name, val.Descriptor.Component.Version, addResourceID, convertResourceID, imageReferenceFromAccess(convertResourceID)); err != nil {
			return fmt.Errorf("failed to create oci upload as local resource transformation: %w", err)
		}
	}

	tgd.Transformations = append(tgd.Transformations, addResourceTransform)

	// Track this resource's transformation
	resourceTransformIDs[i] = addResourceID

	return nil
}
