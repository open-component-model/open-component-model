package internal

import (
	"fmt"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	gitv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	gitv1alpha1 "ocm.software/open-component-model/bindings/go/git/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

// processGit emits the transformation nodes for a git resource selected by a local blob
// uploader. The snapshot at the pinned commit is downloaded (GetGitResource), then embedded
// as a local blob (AddLocalResource).
func processGit(resource descriptorv2.Resource, access *gitv1.Git, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int) error {
	if access.Commit == "" {
		return fmt.Errorf("git resource %q has no pinned commit: pin ref %q to a commit before transferring it by value", resource.Name, access.Ref)
	}

	resourceID := identityToTransformationID(resource.ToIdentity())
	getResourceID := fmt.Sprintf("%sGet%s", id, resourceID)
	addResourceID := fmt.Sprintf("%sAdd%s", id, resourceID)

	unstructured, err := runtime.UnstructuredFromMixedData(map[string]any{
		"resource": resource,
	})
	if err != nil {
		return fmt.Errorf("cannot create unstructured spec for GetGitResource transformation: %w", err)
	}

	getTransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type:  gitv1alpha1.GetGitResourceV1alpha1,
			ID:    getResourceID,
			Label: getLabel(&val.Descriptor.Component, resource.Name),
		},
		Spec: unstructured,
	}

	addResourceTransform, err := uploadAsLocalResource(toSpec, val.Descriptor.Component.Name, val.Descriptor.Component.Version, addResourceID, getResourceID, resource.Name, addLabel(&val.Descriptor.Component, resource.Name, "LocalBlob", toSpec))
	if err != nil {
		return fmt.Errorf("failed to create local resource upload transformation: %w", err)
	}
	tgd.Transformations = append(tgd.Transformations, getTransform, addResourceTransform)
	resourceTransformIDs[i] = addResourceID

	return nil
}
