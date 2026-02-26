package internal

import (
	"context"
	"encoding/json"
	"fmt"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ocirepo "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
	"ocm.software/open-component-model/cli/internal/reference/compref"
)

type ociArtifactProcessor struct{}

var _ processor = (*ociArtifactProcessor)(nil)

func init() {
	registerProcessor(&ociv1.OCIImage{}, &ociArtifactProcessor{})
}

func (p *ociArtifactProcessor) ShouldUploadAsOCIArtifact(ctx context.Context, resource descriptorv2.Resource, toSpec runtime.Typed, access runtime.Typed, uploadType UploadType) (bool, error) {
	if _, isOCITarget := toSpec.(*ocirepo.Repository); isOCITarget {
		if uploadType == UploadAsOciArtifact {
			return true, nil
		}
	}
	return false, nil
}

func (p *ociArtifactProcessor) Process(ctx context.Context, resource descriptorv2.Resource, id string, ref *compref.Ref,val *discoveryValue,  tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int, uploadAsOCIArtifact bool) error {
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
	referenceName, err := ParseReferenceName(ociAccess.ImageReference)
	if err != nil {
		return fmt.Errorf("cannot get reference name: %w", err)
	}

	jRes, err := json.Marshal(resource)
	if err != nil {
		return fmt.Errorf("cannot marshal resource: %w", err)
	}
	var resourceMap map[string]any
	if err := json.Unmarshal(jRes, &resourceMap); err != nil {
		return fmt.Errorf("cannot unmarshal resource to map: %w", err)
	}

	// Create GetOCIArtifact transformation
	getArtifactTransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: ociv1alpha1.GetOCIArtifactV1alpha1,
			ID:   getResourceID,
		},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"resource": resourceMap,
		}},
	}
	tgd.Transformations = append(tgd.Transformations, getArtifactTransform)

	// Create AddLocalResource transformation
	var addResourceTransform transformv1alpha1.GenericTransformation
	if uploadAsOCIArtifact {
		if addResourceTransform, err = ociUploadAsArtifact(toSpec, addResourceID, getResourceID, staticReferenceName(referenceName)); err != nil {
			return fmt.Errorf("failed to create oci upload transformation: %w", err)
		}
	} else {
		if addResourceTransform, err = ociUploadAsLocalResource(toSpec, component, version, addResourceID, getResourceID, referenceName); err != nil {
			return fmt.Errorf("failed to create local resource upload transformation: %w", err)
		}
	}

	tgd.Transformations = append(tgd.Transformations, addResourceTransform)

	// Track this resource's transformation
	resourceTransformIDs[i] = addResourceID

	return nil
}

// ociUploadAsLocalResource creates an AddLocalResource transformation that uploads the OCI artifact as a local resource to the target repository.
// It uses the output of the GetOCIArtifact transformation to populate the fields of the AddLocalResource transformation, ensuring that the same resource is referenced and uploaded.
func ociUploadAsLocalResource(toSpec runtime.Typed, component, version, addResourceID, getResourceID, referenceName string) (transformv1alpha1.GenericTransformation, error) {
	addLocalResourceType, err := ChooseAddLocalResourceType(toSpec)
	if err != nil {
		return transformv1alpha1.GenericTransformation{}, fmt.Errorf("choosing add local resource type for target repository: %w", err)
	}

	addResourceTransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: addLocalResourceType,
			ID:   addResourceID,
		},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"repository": AsUnstructured(toSpec).Data,
			"component":  component,
			"version":    version,
			"resource": map[string]any{
				"name":     fmt.Sprintf("${%s.output.resource.name}", getResourceID),
				"version":  fmt.Sprintf("${%s.output.resource.version}", getResourceID),
				"type":     fmt.Sprintf("${%s.output.resource.type}", getResourceID),
				"relation": fmt.Sprintf("${%s.output.resource.relation}", getResourceID),
				"access": map[string]interface{}{
					"type":          descriptor.GetLocalBlobAccessType().String(),
					"referenceName": referenceName,
				},
				"digest":        fmt.Sprintf("${%s.output.resource.digest}", getResourceID),
				"labels":        fmt.Sprintf("${has(%s.output.resource.labels) ? %s.output.resource.labels  : []}", getResourceID, getResourceID),
				"extraIdentity": fmt.Sprintf("${has(%s.output.resource.extraIdentity) ? %s.output.resource.extraIdentity  : {}}", getResourceID, getResourceID),
				"srcRefs":       fmt.Sprintf("${has(%s.output.resource.srcRefs) ? %s.output.resource.srcRefs  : []}", getResourceID, getResourceID),
			},
			"file": fmt.Sprintf("${%s.output.file}", getResourceID),
		}},
	}
	return addResourceTransform, nil
}

type referenceNameOption func(targetRepoBaseURL string) string

func staticReferenceName(referenceName string) referenceNameOption {
	return func(targetRepoBaseURL string) string {
		return fmt.Sprintf("%s/%s", targetRepoBaseURL, referenceName)
	}
}

func celExpReferenceName(fromResourceID, referenceName string) referenceNameOption {
	return func(targetRepoBaseURL string) string {
		return fmt.Sprintf("%s/%s:${%s.output.resource.access.version}", targetRepoBaseURL, referenceName, fromResourceID)
	}
}

// ociUploadAsArtifact creates an AddOCIArtifact transformation that uploads the OCI artifact to the target repository as an OCI artifact.
// It constructs the target image reference from the toSpec and referenceName, and uses the output of the GetOCIArtifact transformation to populate the fields of the AddOCIArtifact transformation, ensuring that the same resource is referenced and uploaded.
func ociUploadAsArtifact(toSpec runtime.Typed, addResourceID string, getResourceID string, referenceName referenceNameOption) (transformv1alpha1.GenericTransformation, error) {
	var ociSpec ocirepo.Repository
	if err := Scheme.Convert(toSpec, &ociSpec); err != nil {
		return transformv1alpha1.GenericTransformation{}, err
	}
	targetRepoBaseURL := ociSpec.BaseUrl
	if ociSpec.SubPath != "" {
		targetRepoBaseURL = targetRepoBaseURL + "/" + ociSpec.SubPath
	}

	addResourceTransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: runtime.NewVersionedType(ociv1alpha1.AddOCIArtifactType, ociv1alpha1.Version),
			ID:   addResourceID,
		},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"resource": map[string]any{
				"name":     fmt.Sprintf("${%s.output.resource.name}", getResourceID),
				"version":  fmt.Sprintf("${%s.output.resource.version}", getResourceID),
				"type":     fmt.Sprintf("${%s.output.resource.type}", getResourceID),
				"relation": fmt.Sprintf("${%s.output.resource.relation}", getResourceID),
				"access": map[string]interface{}{
					"type":           runtime.NewVersionedType(ociv1.LegacyType, ociv1.LegacyTypeVersion).String(),
					"imageReference": referenceName(targetRepoBaseURL),
				},
				"digest":        fmt.Sprintf("${%s.output.resource.digest}", getResourceID),
				"labels":        fmt.Sprintf("${has(%s.output.resource.labels) ? %s.output.resource.labels  : []}", getResourceID, getResourceID),
				"extraIdentity": fmt.Sprintf("${has(%s.output.resource.extraIdentity) ? %s.output.resource.extraIdentity  : {}}", getResourceID, getResourceID),
				"srcRefs":       fmt.Sprintf("${has(%s.output.resource.srcRefs) ? %s.output.resource.srcRefs  : []}", getResourceID, getResourceID),
			},
			"file": fmt.Sprintf("${%s.output.file}", getResourceID),
		}},
	}
	return addResourceTransform, nil
}