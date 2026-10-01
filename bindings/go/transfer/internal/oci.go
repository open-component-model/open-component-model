package internal

import (
	"encoding/json"
	"fmt"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

// processOCIArtifact fetches an OCI artifact from the source and embeds it as a local blob
// in the target (GetOCIArtifact -> AddLocalResource).
func processOCIArtifact(resource descriptorv2.Resource, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int) error {
	component := val.Descriptor.Component.Name
	version := val.Descriptor.Component.Version

	resourceIdentity := resource.ToIdentity()
	resourceID := identityToTransformationID(resourceIdentity)
	getResourceID := fmt.Sprintf("%sGet%s", id, resourceID)
	addResourceID := fmt.Sprintf("%sAdd%s", id, resourceID)

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

	// Create GetOCIArtifact transformation
	unstructured, err := runtime.UnstructuredFromMixedData(map[string]any{
		"resource": resource,
	})
	if err != nil {
		return fmt.Errorf("cannot create unstructured spec for GetOCIArtifact transformation: %w", err)
	}

	getArtifactTransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type:  ociv1alpha1.GetOCIArtifactV1alpha1,
			ID:    getResourceID,
			Label: getLabel(&val.Descriptor.Component, resource.Name),
		},
		Spec: unstructured,
	}
	tgd.Transformations = append(tgd.Transformations, getArtifactTransform)

	// Create AddLocalResource transformation
	var addResourceTransform transformv1alpha1.GenericTransformation
	if addResourceTransform, err = uploadAsLocalResource(toSpec, component, version, addResourceID, getResourceID, referenceName, addLabel(&val.Descriptor.Component, resource.Name, "LocalBlob", toSpec)); err != nil {
		return fmt.Errorf("failed to create local resource upload transformation: %w", err)
	}

	tgd.Transformations = append(tgd.Transformations, addResourceTransform)

	// Track this resource's transformation
	resourceTransformIDs[i] = addResourceID

	return nil
}

// processOCIArtifactStreaming emits a single TransferOCIArtifact node that streams
// the OCI artifact directly from source to imageReference without tar materialization.
func processOCIArtifactStreaming(resource descriptorv2.Resource, id string, tgd *transformv1alpha1.TransformationGraphDefinition, resourceTransformIDs map[int]string, i int, imageReference, label string) error {
	resourceIdentity := resource.ToIdentity()
	resourceID := identityToTransformationID(resourceIdentity)
	transferID := fmt.Sprintf("%sTransfer%s", id, resourceID)

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

	unstructured, err := runtime.UnstructuredFromMixedData(map[string]any{
		"resource":       resource,
		"targetResource": targetResource,
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
