package internal

import (
	"fmt"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	pypiv1alpha1 "ocm.software/open-component-model/bindings/go/pypi/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

// processPyPI emits the transformation nodes for a pypi/v1alpha1 resource. A
// PyPI resource references distribution files behind a Simple Repository API
// index; it is always transferred by value: the files are downloaded as one
// archive (GetPyPIArtifact) and then embedded as a local blob in the target
// repository (AddLocalResource). Like wget, there is no OCI-artifact
// representation, so the upload always goes through the local-resource path
// regardless of the requested upload type. A PyPI release version is immutable,
// so there is no unpinned version to reject.
func processPyPI(resource v2.Resource, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int) error {
	resourceIdentity := resource.ToIdentity()
	resourceID := identityToTransformationID(resourceIdentity)
	getResourceID := fmt.Sprintf("%sGet%s", id, resourceID)
	addResourceID := fmt.Sprintf("%sAdd%s", id, resourceID)

	unstructured, err := runtime.UnstructuredFromMixedData(map[string]any{
		"resource": resource,
	})
	if err != nil {
		return fmt.Errorf("cannot create unstructured spec for GetPyPIArtifact transformation: %w", err)
	}

	getTransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: pypiv1alpha1.GetPyPIArtifactV1alpha1,
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

	resourceTransformIDs[i] = addResourceID

	return nil
}
