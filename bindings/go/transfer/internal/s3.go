package internal

import (
	"fmt"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
	s3v1alpha1 "ocm.software/open-component-model/bindings/go/s3/transformation/spec/v1alpha1"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

// processS3 emits the transformation node(s) for an S3 resource. An S3 resource references an
// object in a bucket; it is always transferred by value. Like wget, there is no conversion step
// and no OCI-artifact representation.
//
// For an OCI registry target the object is streamed straight into the target as a local blob
// through a single fused StreamLocalResource node, so no temporary file is produced and the
// stream never crosses a graph node boundary (the fused transformer internally falls back to a
// buffered add when the source repository is not streaming-capable). It contributes no
// FileCleanup expression.
//
// For a CTF target the blob must land on local disk regardless, so streaming offers no benefit.
// The resource takes the split DownloadS3Resource -> AddLocalResource path, which writes a
// temporary file; that file's expression is returned so the terminal FileCleanup removes it.
func processS3(resource v2.Resource, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int) ([]string, error) {
	resourceIdentity := resource.ToIdentity()
	resourceID := identityToTransformationID(resourceIdentity)
	addResourceID := fmt.Sprintf("%sAdd%s", id, resourceID)

	isOCI, err := targetIsOCI(toSpec)
	if err != nil {
		return nil, fmt.Errorf("determining target repository type for s3 resource: %w", err)
	}

	if isOCI {
		streamTransform, err := streamAsLocalResource(toSpec, val.Descriptor.Component.Name, val.Descriptor.Component.Version, addResourceID, resource, addLabel(&val.Descriptor.Component, resource.Name, "LocalBlob", toSpec))
		if err != nil {
			return nil, fmt.Errorf("failed to create streaming local resource transformation: %w", err)
		}
		tgd.Transformations = append(tgd.Transformations, streamTransform)

		resourceTransformIDs[i] = addResourceID
		return nil, nil
	}

	getResourceID := fmt.Sprintf("%sGet%s", id, resourceID)

	unstructured, err := runtime.UnstructuredFromMixedData(map[string]any{
		"resource": resource,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot create unstructured spec for DownloadS3Resource transformation: %w", err)
	}

	getTransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type:  s3v1alpha1.DownloadS3ResourceV1alpha1,
			ID:    getResourceID,
			Label: getLabel(&val.Descriptor.Component, resource.Name),
		},
		Spec: unstructured,
	}
	tgd.Transformations = append(tgd.Transformations, getTransform)

	addResourceTransform, err := uploadAsLocalResource(toSpec, val.Descriptor.Component.Name, val.Descriptor.Component.Version, addResourceID, getResourceID, resource.Name, addLabel(&val.Descriptor.Component, resource.Name, "LocalBlob", toSpec))
	if err != nil {
		return nil, fmt.Errorf("failed to create local resource upload transformation: %w", err)
	}
	tgd.Transformations = append(tgd.Transformations, addResourceTransform)

	// Track this resource's transformation.
	resourceTransformIDs[i] = addResourceID

	return []string{fmt.Sprintf("${%s.spec.file}", addResourceID)}, nil
}
