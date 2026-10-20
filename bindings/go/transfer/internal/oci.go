package internal

import (
	"encoding/json"
	"fmt"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

// processOCIArtifact streams an OCI artifact from its registry into the target as a local
// blob in a single TransferOCIArtifactToLocalBlob transformation, without buffering it in a file.
func processOCIArtifact(resource descriptorv2.Resource, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int) error {
	var ociAccess ociv1.OCIImage
	if err := json.Unmarshal(resource.Access.Data, &ociAccess); err != nil {
		return fmt.Errorf("cannot unmarshal OCI access: %w", err)
	}

	// e.g. ghcr.io/open-component-model/helmexample/charts/mariadb:12.2.7
	// strip the domain part and keep the rest
	referenceName, err := getReferenceName(ociAccess.ImageReference)
	if err != nil {
		return fmt.Errorf("cannot get reference name: %w", err)
	}
	return transferOCIArtifactToLocalBlob(resource, id, val, tgd, toSpec, resourceTransformIDs, i, referenceName)
}

// transferOCIArtifactToLocalBlob appends the TransferOCIArtifactToLocalBlob transformation that
// streams resource from its registry into the target as a local blob named referenceName.
func transferOCIArtifactToLocalBlob(resource descriptorv2.Resource, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int, referenceName string) error {
	transferID := fmt.Sprintf("%sTransfer%s", id, identityToTransformationID(resource.ToIdentity()))
	toRepo, err := asUnstructured(toSpec)
	if err != nil {
		return fmt.Errorf("cannot convert target spec to unstructured: %w", err)
	}
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
	spec, err := runtime.UnstructuredFromMixedData(map[string]any{
		"resource":       resource,
		"repository":     toRepo.Data,
		"component":      val.Descriptor.Component.Name,
		"version":        val.Descriptor.Component.Version,
		"targetResource": targetResource,
	})
	if err != nil {
		return fmt.Errorf("cannot create unstructured spec for TransferOCIArtifactToLocalBlob transformation: %w", err)
	}
	tgd.Transformations = append(tgd.Transformations, transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type:  ociv1alpha1.TransferOCIArtifactToLocalBlobV1alpha1,
			ID:    transferID,
			Label: transferLabel(&val.Descriptor.Component, resource.Name, "LocalBlob", toSpec),
		},
		Spec: spec,
	})
	resourceTransformIDs[i] = transferID
	return nil
}

// processOCIArtifactStreaming emits a single TransferOCIArtifact node that streams
// the OCI artifact directly from source to imageReference without tar materialization.
func processOCIArtifactStreaming(resource descriptorv2.Resource, id string, tgd *transformv1alpha1.TransformationGraphDefinition, resourceTransformIDs map[int]string, i int, imageReference, label string) error {
	resourceIdentity := resource.ToIdentity()
	resourceID := identityToTransformationID(resourceIdentity)
	transferID := fmt.Sprintf("%sTransfer%s", id, resourceID)

	unstructured, err := runtime.UnstructuredFromMixedData(map[string]any{
		"resource":       resource,
		"targetResource": ociArtifactTargetResource(resource, imageReference),
	})
	if err != nil {
		return fmt.Errorf("cannot create unstructured spec for TransferOCIArtifact transformation: %w", err)
	}

	transferTransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type:  ociv1alpha1.TransferOCIArtifactV1alpha1,
			ID:    transferID,
			Label: label,
		},
		Spec: unstructured,
	}
	tgd.Transformations = append(tgd.Transformations, transferTransform)

	resourceTransformIDs[i] = transferID

	return nil
}

// ociArtifactTargetResource returns resource with an OCI image access to imageReference,
// the target resource of a transfer that uploads it as a separate OCI artifact.
func ociArtifactTargetResource(resource descriptorv2.Resource, imageReference string) map[string]any {
	targetResource := map[string]any{
		"name":     resource.Name,
		"version":  resource.Version,
		"type":     resource.Type,
		"relation": resource.Relation,
		"access": map[string]any{
			"type":           runtime.NewVersionedType(ociv1.LegacyType, ociv1.LegacyTypeVersion).String(),
			"imageReference": imageReference,
		},
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

// ociAddArtifact creates an AddOCIArtifact transformation that pushes the artifact
// produced by the getResourceID step to imageReference and records an OCI image access
// for it on the resource.
func ociAddArtifact(addResourceID, getResourceID, imageReference, label string) transformv1alpha1.GenericTransformation {
	return transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type:  runtime.NewVersionedType(ociv1alpha1.AddOCIArtifactType, ociv1alpha1.Version),
			ID:    addResourceID,
			Label: label,
		},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"resource": map[string]any{
				"name":     fmt.Sprintf("${%s.output.resource.name}", getResourceID),
				"version":  fmt.Sprintf("${%s.output.resource.version}", getResourceID),
				"type":     fmt.Sprintf("${%s.output.resource.type}", getResourceID),
				"relation": fmt.Sprintf("${%s.output.resource.relation}", getResourceID),
				"access": map[string]interface{}{
					"type":           runtime.NewVersionedType(ociv1.LegacyType, ociv1.LegacyTypeVersion).String(),
					"imageReference": imageReference,
				},
				"digest":        fmt.Sprintf("${has(%s.output.resource.digest) ? %s.output.resource.digest : null}", getResourceID, getResourceID),
				"labels":        fmt.Sprintf("${has(%s.output.resource.labels) ? %s.output.resource.labels  : []}", getResourceID, getResourceID),
				"extraIdentity": fmt.Sprintf("${has(%s.output.resource.extraIdentity) ? %s.output.resource.extraIdentity  : {}}", getResourceID, getResourceID),
				"srcRefs":       fmt.Sprintf("${has(%s.output.resource.srcRefs) ? %s.output.resource.srcRefs  : []}", getResourceID, getResourceID),
			},
			"file": fmt.Sprintf("${%s.output.file}", getResourceID),
		}},
	}
}

// processOCIImageLayer streams an OCIImageLayer resource into the target as a local blob
// (TransferOCIArtifactToLocalBlob, whose download path handles the layer access). A layer
// addresses one blob inside an OCI repository rather than a whole artifact, so there is no
// OCI-artifact representation and the OCI uploader does not handle layers. A layer carries
// no repository or tag that would name it in the target, so the resource name is used, as it
// is for other by-value resources.
func processOCIImageLayer(resource descriptorv2.Resource, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int) error {
	return transferOCIArtifactToLocalBlob(resource, id, val, tgd, toSpec, resourceTransformIDs, i, resource.Name)
}
