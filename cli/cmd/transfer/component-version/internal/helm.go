package internal

import (
	"context"
	"encoding/json"
	"fmt"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	helmv1 "ocm.software/open-component-model/bindings/go/helm/access/spec/v1"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/helm/transformation/spec/v1alpha1"
	ocirepo "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
	"ocm.software/open-component-model/cli/internal/reference/compref"
)

type helmChartProcessor struct{}

var (
	_ processor          = (*helmChartProcessor)(nil)
	_ ociUploadSupported = (*helmChartProcessor)(nil)
)

func init() {
	registerProcessor(&helmv1.Helm{}, &helmChartProcessor{})
}

func (h helmChartProcessor) ShouldUploadAsOCIArtifact(ctx context.Context, resource v2.Resource, toSpec runtime.Typed, access runtime.Typed, uploadType UploadType) (bool, error) {
	if _, isOCITarget := toSpec.(*ocirepo.Repository); isOCITarget {
		if uploadType == UploadAsOciArtifact {
			return true, nil
		}
	}
	return true, nil
}

func (h helmChartProcessor) Process(ctx context.Context, resource v2.Resource, id string, ref *compref.Ref, tgd *v1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int, uploadAsOCIArtifact bool) error {
	resourceIdentity := resource.ToIdentity()
	resourceID := identityToTransformationID(resourceIdentity)
	getResourceID := fmt.Sprintf("%sGet%s", id, resourceID)
	convertResourceID := fmt.Sprintf("%sConvert%s", id, resourceID)
	addResourceID := fmt.Sprintf("%sAdd%s", id, resourceID)

	var helmAccess helmv1.Helm
	if err := json.Unmarshal(resource.Access.Data, &helmAccess); err != nil {
		return fmt.Errorf("cannot unmarshal Helm access: %w", err)
	}

	jRes, err := json.Marshal(resource)
	if err != nil {
		return fmt.Errorf("cannot marshal resource: %w", err)
	}
	var resourceMap map[string]any
	if err := json.Unmarshal(jRes, &resourceMap); err != nil {
		return fmt.Errorf("cannot unmarshal resource to map: %w", err)
	}

	// Create GetHelmChart transformation
	getChartTransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: ociv1alpha1.GetHelmChartV1alpha1,
			ID:   getResourceID,
		},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"resource": resourceMap,
		}},
	}
	tgd.Transformations = append(tgd.Transformations, getChartTransform)

	// convert chart to oci artifact transformation
	convertToOCITransform := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: ociv1alpha1.ConvertHelmToOCIV1alpha1,
			ID:   convertResourceID,
		},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"resource":  fmt.Sprintf("${%s.output.resource}", getResourceID),
			"chartFile": fmt.Sprintf("${%s.output.chartFile}", getResourceID),
			"provFile":  fmt.Sprintf("${has(%[1]s.output.provFile) ? %[1]s.output.provFile : null}", getResourceID),
		}},
	}
	tgd.Transformations = append(tgd.Transformations, convertToOCITransform)

	// Create AddLocalResource transformation
	var addResourceTransform transformv1alpha1.GenericTransformation
	if uploadAsOCIArtifact {
		if addResourceTransform, err = ociUploadAsArtifact(toSpec, addResourceID, convertResourceID, imageReferenceFromAccess(convertResourceID)); err != nil {
			return fmt.Errorf("failed to create oci upload transformation: %w", err)
		}
	} else {
		// Not supported, error out
		return fmt.Errorf("uploading Helm charts as local resources is not supported, please choose to upload as OCI artifacts instead")
	}

	tgd.Transformations = append(tgd.Transformations, addResourceTransform)

	// Track this resource's transformation
	resourceTransformIDs[i] = addResourceID

	return nil
}
