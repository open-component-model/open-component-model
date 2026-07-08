package internal

import (
	"fmt"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	githubv1alpha1 "ocm.software/open-component-model/bindings/go/github/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

// processGitHub builds the transfer chain for a resource with a gitHub access:
// a GetGitHubCommit node downloads and buffers the repository archive at the
// pinned commit, then an AddLocalResource node stores that archive as a
// localBlob in the target (OCI or CTF). A GitHub source tarball is already a
// finished blob, so — unlike Helm — there is no conversion step.
func processGitHub(resource descriptorv2.Resource, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int) error {
	component := val.Descriptor.Component.Name
	version := val.Descriptor.Component.Version

	resourceIdentity := resource.ToIdentity()
	resourceID := identityToTransformationID(resourceIdentity)
	getResourceID := fmt.Sprintf("%sGet%s", id, resourceID)
	addResourceID := fmt.Sprintf("%sAdd%s", id, resourceID)

	// Create GetGitHubCommit transformation.
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

	// Create AddLocalResource transformation referencing the buffered tarball.
	// The github get output buffers the archive under "contentFile".
	addResourceTransform, err := ociUploadAsLocalResource(
		toSpec, component, version, addResourceID, getResourceID,
		staticReferenceName(resource.Name), "contentFile",
	)
	if err != nil {
		return fmt.Errorf("failed to create local resource upload transformation: %w", err)
	}
	tgd.Transformations = append(tgd.Transformations, addResourceTransform)

	resourceTransformIDs[i] = addResourceID
	return nil
}
