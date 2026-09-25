package internal

import (
	"fmt"
	"net/url"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

// processJFrogHelmUploader emits a single JFrogHelmUpload transformation for resource from a
// [transferv1alpha1.JFrogHelmUploaderConfig]. The chart is stored under the component version's
// path in the repository and published with the chart name and version Artifactory records, so
// the transformation computes the published access at runtime and the descriptor picks it up
// from its output. A local blob is read from the source component version.
func processJFrogHelmUploader(resource descriptorv2.Resource, access runtime.Typed, u *transferv1alpha1.JFrogHelmUploaderConfig, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, resourceTransformIDs map[int]string, i int) error {
	if resource.Access == nil {
		return fmt.Errorf("resource access is required")
	}
	if err := u.Validate(); err != nil {
		return fmt.Errorf("invalid jfrog helm uploader: %w", err)
	}
	base, err := url.Parse(u.URL)
	if err != nil {
		return fmt.Errorf("invalid artifactory url: %w", err)
	}

	cv := map[string]any{
		"component": val.Descriptor.Component.Name,
		"version":   val.Descriptor.Component.Version,
	}
	if _, ok := access.(*descriptorv2.LocalBlob); ok {
		sourceRepo, err := asUnstructured(val.SourceRepository)
		if err != nil {
			return fmt.Errorf("cannot convert source repository spec to unstructured: %w", err)
		}
		cv["repository"] = sourceRepo.Data
	}
	spec, err := runtime.UnstructuredFromMixedData(map[string]any{
		"resource":         resource,
		"componentVersion": cv,
		"url":              u.URL,
		"repository":       u.Repository,
		"reindex":          u.ReindexEnabled(),
	})
	if err != nil {
		return fmt.Errorf("cannot create unstructured spec for jfrog helm upload transformation: %w", err)
	}

	uploadID := fmt.Sprintf("%sUpload%s", id, identityToTransformationID(resource.ToIdentity()))
	tgd.Transformations = append(tgd.Transformations, transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type:  JFrogHelmUploadVersionedType,
			ID:    uploadID,
			Label: uploaderLabel(&val.Descriptor.Component, resource.Name, base.Host),
		},
		Spec: spec,
	})
	resourceTransformIDs[i] = uploadID
	return nil
}
