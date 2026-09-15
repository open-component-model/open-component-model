package internal

import (
	"fmt"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	mavenv2alpha1 "ocm.software/open-component-model/bindings/go/maven/spec/access/v2alpha1"
	mavenv1alpha1 "ocm.software/open-component-model/bindings/go/maven/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

// processMaven emits the transformation nodes for a maven/v2alpha1 resource
// under CopyModeAllResources. The listed files and their sibling signature and
// checksum files are downloaded as one archive (GetMavenArtifact), then
// embedded as a local blob (AddLocalResource). A Maven resource has no
// OCI-artifact representation, so uploadAsArtifact is not honored here.
//
// Only a pinned version is transferred. LATEST, RELEASE and a SNAPSHOT resolve
// to different files over time, so the blob would not match the digest the
// source descriptor records.
func processMaven(resource descriptorv2.Resource, access *mavenv2alpha1.Maven, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int) error {
	if !access.IsPinnedVersion() {
		return fmt.Errorf("maven resource %q has version %q: pin LATEST, RELEASE or a SNAPSHOT to a release version before transferring it by value", resource.Name, access.Version)
	}

	resourceIdentity := resource.ToIdentity()
	resourceID := identityToTransformationID(resourceIdentity)
	getResourceID := fmt.Sprintf("%sGet%s", id, resourceID)
	addResourceID := fmt.Sprintf("%sAdd%s", id, resourceID)

	unstructured, err := runtime.UnstructuredFromMixedData(map[string]any{
		"resource": resource,
	})
	if err != nil {
		return fmt.Errorf("cannot create unstructured spec for GetMavenArtifact transformation: %w", err)
	}

	getTransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: mavenv1alpha1.GetMavenArtifactV1alpha1,
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
