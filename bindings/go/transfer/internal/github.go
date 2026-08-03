package internal

import (
	"fmt"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	githubv1 "ocm.software/open-component-model/bindings/go/github/spec/access/v1"
	githubv1alpha1 "ocm.software/open-component-model/bindings/go/github/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

// processGitHub emits the transformation nodes for a gitHub resource under
// CopyModeAllResources. The archive at the pinned commit is downloaded (GetGitHubCommit),
// then embedded as a local blob (AddLocalResource).
func processGitHub(resource descriptorv2.Resource, access *githubv1.GitHub, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int) error {
	if access.Commit == "" {
		return fmt.Errorf("github resource %q has no pinned commit: pin ref %q to a commit before transferring it by value", resource.Name, access.Ref)
	}

	resourceIdentity := resource.ToIdentity()
	resourceID := identityToTransformationID(resourceIdentity)
	getResourceID := fmt.Sprintf("%sGet%s", id, resourceID)
	addResourceID := fmt.Sprintf("%sAdd%s", id, resourceID)

	unstructured, err := runtime.UnstructuredFromMixedData(map[string]any{
		"resource": resource,
	})
	if err != nil {
		return fmt.Errorf("cannot create unstructured spec for GetGitHubCommit transformation: %w", err)
	}

	getTransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: githubv1alpha1.GetGitHubCommitV1alpha1,
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
