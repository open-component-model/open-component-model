package internal

import (
	"context"
	"fmt"
	"log/slog"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ocirepo "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
	"ocm.software/open-component-model/cli/internal/reference/compref"
)

type localBlobProcessor struct{}

var (
	_ processor          = (*localBlobProcessor)(nil)
	_ ociUploadSupported = (*localBlobProcessor)(nil)
)

func init() {
	registerProcessor(&descriptorv2.LocalBlob{}, &localBlobProcessor{})
}

func (p *localBlobProcessor) ShouldUploadAsOCIArtifact(ctx context.Context, resource descriptorv2.Resource, toSpec runtime.Typed, access runtime.Typed, uploadType UploadType) (bool, error) {
	acc, ok := access.(*descriptorv2.LocalBlob)
	if !ok {
		return false, fmt.Errorf("unexpected access type %s, expected LocalBlob", access.GetType().String())
	}
	if _, isOCITarget := toSpec.(*ocirepo.Repository); isOCITarget {
		if uploadType == UploadAsOciArtifact && IsOCICompliantManifest(acc.MediaType) {
			// TODO(fabianburth): We currently do not support a way to specify a reference name
			//  based on input type. Long term, this whole scenario should be redesigned through
			//  a transfer config. Short term, we pray that we can neglect this scenario.
			if acc.ReferenceName != "" {
				return true, nil
			}

			slog.DebugContext(ctx, "local blob resource is not uploaded to individual oci repository since it does not have a reference name", "resource", resource.ToIdentity().String())
		}
	}
	return false, nil
}

func (p *localBlobProcessor) Process(ctx context.Context, resource descriptorv2.Resource, id string, ref *compref.Ref, val *discoveryValue,tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int, uploadAsOCIArtifact bool) error {
	component := val.Descriptor.Component.Name
	version := val.Descriptor.Component.Version
	sourceRepo := val.SourceRepository

	// Generate transformation IDs
	resourceIdentity := resource.ToIdentity()
	resourceID := identityToTransformationID(resourceIdentity)
	getResourceID := fmt.Sprintf("%sGet%s", id, resourceID)
	addResourceID := fmt.Sprintf("%sAdd%s", id, resourceID)

	// Convert resourceIdentity to map[string]any for deep copy compatibility
	resourceIdentityMap := make(map[string]any)
	for k, v := range resourceIdentity {
		resourceIdentityMap[k] = v
	}

	getLocalResourceType, err := ChooseGetLocalResourceType(sourceRepo)
	if err != nil {
		return fmt.Errorf("choosing get local resource type for source repository: %w", err)
	}

	// Create GetLocalResource transformation
	getResourceTransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: getLocalResourceType,
			ID:   getResourceID,
		},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"repository":       AsUnstructured(sourceRepo).Data,
			"component":        component,
			"version":          version,
			"resourceIdentity": resourceIdentityMap,
		}},
	}
	tgd.Transformations = append(tgd.Transformations, getResourceTransform)

	var addResourceTransform transformv1alpha1.GenericTransformation
	if !uploadAsOCIArtifact {
		addLocalResourceType, err := ChooseAddLocalResourceType(toSpec)
		if err != nil {
			return fmt.Errorf("choosing add local resource type for target repository: %w", err)
		}

		// Create AddLocalResource transformation
		addResourceTransform = transformv1alpha1.GenericTransformation{
			TransformationMeta: meta.TransformationMeta{
				Type: addLocalResourceType,
				ID:   addResourceID,
			},
			Spec: &runtime.Unstructured{Data: map[string]any{
				"repository": AsUnstructured(toSpec).Data,
				"component":  component,
				"version":    version,
				"resource":   fmt.Sprintf("${%s.output.resource}", getResourceID),
				"file":       fmt.Sprintf("${%s.output.file}", getResourceID),
			}},
		}
	} else {
		var ociSpec ocirepo.Repository
		if err := Scheme.Convert(toSpec, &ociSpec); err != nil {
			return err
		}
		targetRepoBaseURL := ociSpec.BaseUrl
		if ociSpec.SubPath != "" {
			targetRepoBaseURL = targetRepoBaseURL + "/" + ociSpec.SubPath
		}
		addResourceTransform = transformv1alpha1.GenericTransformation{
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
						"imageReference": fmt.Sprintf("%s/${%s.output.resource.access.referenceName}", targetRepoBaseURL, getResourceID),
					},
					"digest":        fmt.Sprintf("${%s.output.resource.digest}", getResourceID),
					"labels":        fmt.Sprintf("${has(%s.output.resource.labels) ? %s.output.resource.labels  : []}", getResourceID, getResourceID),
					"extraIdentity": fmt.Sprintf("${has(%s.output.resource.extraIdentity) ? %s.output.resource.extraIdentity  : {}}", getResourceID, getResourceID),
					"srcRefs":       fmt.Sprintf("${has(%s.output.resource.srcRefs) ? %s.output.resource.srcRefs  : []}", getResourceID, getResourceID),
				},
				"file": fmt.Sprintf("${%s.output.file}", getResourceID),
			}},
		}
	}
	tgd.Transformations = append(tgd.Transformations, addResourceTransform)
	// Track this resource's transformation
	resourceTransformIDs[i] = addResourceID
	return nil
}
