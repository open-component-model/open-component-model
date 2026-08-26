package internal

import (
	"fmt"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
	wgetv1alpha1 "ocm.software/open-component-model/bindings/go/wget/transformation/spec/v1alpha1"
)

// processWget emits the transformation nodes for a wget resource. A wget resource references
// content behind an HTTP/S URL; it is always transferred by value: the content is downloaded to a
// file (DownloadWget) and then embedded as a local blob in the target repository (AddLocalResource).
// Unlike helm charts, there is no conversion step and no OCI-artifact representation, so the
// upload always goes through the local-resource path regardless of the requested upload type.
func processWget(resource v2.Resource, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int) error {
	resourceIdentity := resource.ToIdentity()
	resourceID := identityToTransformationID(resourceIdentity)
	getResourceID := fmt.Sprintf("%sGet%s", id, resourceID)
	addResourceID := fmt.Sprintf("%sAdd%s", id, resourceID)

	unstructured, err := runtime.UnstructuredFromMixedData(map[string]any{
		"resource": resource,
	})
	if err != nil {
		return fmt.Errorf("cannot create unstructured spec for DownloadWget transformation: %w", err)
	}

	getTransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: wgetv1alpha1.DownloadWgetResourceV1alpha1,
			ID:   getResourceID,
		},
		Spec: unstructured,
	}
	tgd.Transformations = append(tgd.Transformations, getTransform)

	addResourceTransform, err := uploadAsLocalResource(toSpec, val.Descriptor.Component.Name, val.Descriptor.Component.Version, addResourceID, getResourceID, staticReferenceName(resource.Name))
	if err != nil {
		return fmt.Errorf("failed to create local resource upload transformation: %w", err)
	}
	tgd.Transformations = append(tgd.Transformations, addResourceTransform)

	// Track this resource's transformation.
	resourceTransformIDs[i] = addResourceID

	return nil
}
