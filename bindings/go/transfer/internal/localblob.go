package internal

import (
	"fmt"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

// appendGetLocalResourceTransform appends the GetLocalResource transformation that fetches
// a repository-local resource (a local blob or a relativeOciReference) from the source.
// These accesses are only resolvable with component context, so the concrete transformation
// type is chosen from the source repository (OCI registry or CTF) via
// chooseGetLocalResourceType.
func appendGetLocalResourceTransform(tgd *transformv1alpha1.TransformationGraphDefinition, resource descriptorv2.Resource, sourceRepo runtime.Typed, component, version, getResourceID, label string) error {
	resourceIdentityMap := make(map[string]any)
	for k, v := range resource.ToIdentity() {
		resourceIdentityMap[k] = v
	}

	getLocalResourceType, err := chooseGetLocalResourceType(sourceRepo)
	if err != nil {
		return fmt.Errorf("choosing get local resource type for source repository: %w", err)
	}

	sourceRepoUnstructured, err := asUnstructured(sourceRepo)
	if err != nil {
		return fmt.Errorf("cannot convert source repository spec to unstructured: %w", err)
	}

	tgd.Transformations = append(tgd.Transformations, transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type:  getLocalResourceType,
			ID:    getResourceID,
			Label: label,
		},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"repository":       sourceRepoUnstructured.Data,
			"component":        component,
			"version":          version,
			"resourceIdentity": resourceIdentityMap,
		}},
	})
	return nil
}

// processLocalBlob fetches a local blob from the source. With an empty ociImageReference it
// embeds the blob as a local blob in the target; otherwise it pushes the blob (an OCI
// manifest) as a separate OCI artifact to ociImageReference.
func processLocalBlob(resource descriptorv2.Resource, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int, ociImageReference string) error {
	component := val.Descriptor.Component.Name
	version := val.Descriptor.Component.Version
	sourceRepo := val.SourceRepository

	// Generate transformation IDs
	resourceIdentity := resource.ToIdentity()
	resourceID := identityToTransformationID(resourceIdentity)
	getResourceID := fmt.Sprintf("%sGet%s", id, resourceID)
	addResourceID := fmt.Sprintf("%sAdd%s", id, resourceID)

	if err := appendGetLocalResourceTransform(tgd, resource, sourceRepo, component, version, getResourceID, getLabel(&val.Descriptor.Component, resource.Name)); err != nil {
		return err
	}

	var addResourceTransform transformv1alpha1.GenericTransformation
	if ociImageReference == "" {
		toRepo, err := asUnstructured(toSpec)
		if err != nil {
			return fmt.Errorf("cannot convert target spec to unstructured: %w", err)
		}

		addLocalResourceType, err := chooseAddLocalResourceType(toSpec)
		if err != nil {
			return fmt.Errorf("choosing add local resource type for target repository: %w", err)
		}
		// Create AddLocalResource transformation
		addResourceTransform = transformv1alpha1.GenericTransformation{
			TransformationMeta: meta.TransformationMeta{
				Type:  addLocalResourceType,
				ID:    addResourceID,
				Label: addLabel(&val.Descriptor.Component, resource.Name, "LocalBlob", toSpec),
			},
			Spec: &runtime.Unstructured{Data: map[string]any{
				"repository": toRepo.Data,
				"component":  component,
				"version":    version,
				"resource":   fmt.Sprintf("${%s.output.resource}", getResourceID),
				"file":       fmt.Sprintf("${%s.output.file}", getResourceID),
			}},
		}
	} else {
		addResourceTransform = ociAddArtifact(addResourceID, getResourceID, ociImageReference,
			addLabel(&val.Descriptor.Component, resource.Name, "OCIArtifact", toSpec))
	}
	tgd.Transformations = append(tgd.Transformations, addResourceTransform)
	// Track this resource's transformation
	resourceTransformIDs[i] = addResourceID
	return nil
}

// processRelativeOCIReference fetches a relativeOciReference from the source (which needs
// component context, like a local blob) and embeds it in the target as a local blob whose
// referenceName is the relative reference verbatim. Unlike processLocalBlob's by-value path,
// which passes the source access through, this re-stamps the target access to localBlob so
// the relativeOciReference type does not leak into the target descriptor, and preserves the
// reference so the artifact can be re-materialised as an OCI image on a later transfer. The
// reference is already registry-relative, so it is the referenceName as-is (not run through
// getReferenceName, which would strip a leading path segment as a host).
func processRelativeOCIReference(resource descriptorv2.Resource, reference, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int) error {
	component := val.Descriptor.Component.Name
	version := val.Descriptor.Component.Version

	resourceID := identityToTransformationID(resource.ToIdentity())
	getResourceID := fmt.Sprintf("%sGet%s", id, resourceID)
	addResourceID := fmt.Sprintf("%sAdd%s", id, resourceID)

	if err := appendGetLocalResourceTransform(tgd, resource, val.SourceRepository, component, version, getResourceID, getLabel(&val.Descriptor.Component, resource.Name)); err != nil {
		return err
	}

	addResourceTransform, err := uploadAsLocalResource(toSpec, component, version, addResourceID, getResourceID, reference,
		addLabel(&val.Descriptor.Component, resource.Name, "LocalBlob", toSpec))
	if err != nil {
		return err
	}
	tgd.Transformations = append(tgd.Transformations, addResourceTransform)
	resourceTransformIDs[i] = addResourceID
	return nil
}

// uploadAsLocalResource creates an AddLocalResource transformation that embeds the fetched
// blob as a local resource (localBlob access) in the target repository. It is independent of
// the source content type: OCI artifacts, helm charts and wget downloads all transfer by value
// through this path. The concrete transformation type is chosen from the target repository
// (OCI registry or CTF) via chooseAddLocalResourceType.
// It uses the output of the preceding Get transformation to populate the fields of the
// AddLocalResource transformation, ensuring that the same resource is referenced and uploaded.
//
// referenceName is a legacy OCI compatibility field: it names the global OCI artifact the
// local blob can additionally be exposed as, and only makes sense for blobs that hold an OCI
// manifest. It is set only by the OCI-family callers (OCI artifacts, Helm charts converted to
// OCI, relativeOciReference). Non-OCI content types (git, github, s3, wget) pass an empty
// referenceName, in which case the field is omitted from the generated access entirely.
func uploadAsLocalResource(toSpec runtime.Typed, component, version, addResourceID, getResourceID, referenceName, label string) (transformv1alpha1.GenericTransformation, error) {
	addLocalResourceType, err := chooseAddLocalResourceType(toSpec)
	if err != nil {
		return transformv1alpha1.GenericTransformation{}, fmt.Errorf("choosing add local resource type for target repository: %w", err)
	}

	toRepo, err := asUnstructured(toSpec)
	if err != nil {
		return transformv1alpha1.GenericTransformation{}, fmt.Errorf("cannot convert target spec to unstructured: %w", err)
	}

	access := map[string]any{
		"type": descriptor.GetLocalBlobAccessType().String(),
	}
	if referenceName != "" {
		access["referenceName"] = referenceName
	}

	addResourceTransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type:  addLocalResourceType,
			ID:    addResourceID,
			Label: label,
		},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"repository": toRepo.Data,
			"component":  component,
			"version":    version,
			"resource": map[string]any{
				"name":          fmt.Sprintf("${%s.output.resource.name}", getResourceID),
				"version":       fmt.Sprintf("${%s.output.resource.version}", getResourceID),
				"type":          fmt.Sprintf("${%s.output.resource.type}", getResourceID),
				"relation":      fmt.Sprintf("${%s.output.resource.relation}", getResourceID),
				"access":        access,
				"digest":        fmt.Sprintf("${has(%s.output.resource.digest) ? %s.output.resource.digest : null}", getResourceID, getResourceID),
				"labels":        fmt.Sprintf("${has(%s.output.resource.labels) ? %s.output.resource.labels  : []}", getResourceID, getResourceID),
				"extraIdentity": fmt.Sprintf("${has(%s.output.resource.extraIdentity) ? %s.output.resource.extraIdentity  : {}}", getResourceID, getResourceID),
				"srcRefs":       fmt.Sprintf("${has(%s.output.resource.srcRefs) ? %s.output.resource.srcRefs  : []}", getResourceID, getResourceID),
			},
			"file": fmt.Sprintf("${%s.output.file}", getResourceID),
		}},
	}
	return addResourceTransform, nil
}
