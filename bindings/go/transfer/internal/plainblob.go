package internal

import (
	"fmt"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
	s3v2 "ocm.software/open-component-model/bindings/go/s3/spec/access/v2"
	uploadv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/transformation/spec/v1alpha1"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
	wgetv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

// processPlainBlob appends the fused Transfer{Wget,S3}To{OCI,CTF}LocalBlob transformation
// that streams the content behind access into the target as a local blob, without
// buffering it in a file. The transformation type is chosen from the access and the target
// repository kind, so its spec carries the access and the repository as concrete types.
func processPlainBlob(resource descriptorv2.Resource, access runtime.Typed, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int) error {
	transferID := fmt.Sprintf("%sTransfer%s", id, identityToTransformationID(resource.ToIdentity()))
	transferType, err := choosePlainBlobToLocalBlobType(access, toSpec)
	if err != nil {
		return err
	}
	toRepo, err := asUnstructured(toSpec)
	if err != nil {
		return fmt.Errorf("cannot convert target spec to unstructured: %w", err)
	}
	spec, err := runtime.UnstructuredFromMixedData(map[string]any{
		"access":         access,
		"repository":     toRepo.Data,
		"component":      val.Descriptor.Component.Name,
		"version":        val.Descriptor.Component.Version,
		"targetResource": localBlobTargetResource(resource, ""),
	})
	if err != nil {
		return fmt.Errorf("cannot create unstructured spec for %s transformation: %w", transferType, err)
	}
	tgd.Transformations = append(tgd.Transformations, transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type:  transferType,
			ID:    transferID,
			Label: transferLabel(&val.Descriptor.Component, resource.Name, "LocalBlob", toSpec),
		},
		Spec: spec,
	})
	resourceTransformIDs[i] = transferID
	return nil
}

// choosePlainBlobToLocalBlobType returns the transformation that streams the content behind
// access into a component version in the to repository as a local blob.
func choosePlainBlobToLocalBlobType(access, to runtime.Typed) (runtime.Type, error) {
	toOCI, err := isOCIRepository(to)
	if err != nil {
		return runtime.Type{}, fmt.Errorf("target repository: %w", err)
	}
	switch access.(type) {
	case *wgetv1.Wget:
		if toOCI {
			return uploadv1alpha1.TransferWgetToOCILocalBlobV1alpha1, nil
		}
		return uploadv1alpha1.TransferWgetToCTFLocalBlobV1alpha1, nil
	case *s3v2.S3:
		if toOCI {
			return uploadv1alpha1.TransferS3ToOCILocalBlobV1alpha1, nil
		}
		return uploadv1alpha1.TransferS3ToCTFLocalBlobV1alpha1, nil
	default:
		return runtime.Type{}, fmt.Errorf("access %T cannot be streamed into a local blob", access)
	}
}

// localBlobTargetResource returns resource as it is added to the target: with a local blob
// access. referenceName is only set for OCI artifacts (see uploadAsLocalResource).
func localBlobTargetResource(resource descriptorv2.Resource, referenceName string) map[string]any {
	access := map[string]any{"type": descriptor.GetLocalBlobAccessType().String()}
	if referenceName != "" {
		access["referenceName"] = referenceName
	}
	targetResource := map[string]any{
		"name":     resource.Name,
		"version":  resource.Version,
		"type":     resource.Type,
		"relation": resource.Relation,
		"access":   access,
	}
	if resource.Digest != nil {
		targetResource["digest"] = resource.Digest
	}
	if len(resource.Labels) > 0 {
		targetResource["labels"] = resource.Labels
	}
	if len(resource.ExtraIdentity) > 0 {
		targetResource["extraIdentity"] = resource.ExtraIdentity
	}
	if len(resource.SourceRefs) > 0 {
		targetResource["srcRefs"] = resource.SourceRefs
	}
	return targetResource
}
