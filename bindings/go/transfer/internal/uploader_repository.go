package internal

import (
	"fmt"
	"net/url"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

// processRepositoryUploader emits a single transformation of type typ ([artifactory.Transformation] or
// [nexus.Transformation]) for resource from an [transferv1alpha1.ArtifactoryUploaderConfig] or
// [transferv1alpha1.NexusUploaderConfig]. The published access depends on the type of the target
// repository, which the transformation detects at runtime, so the descriptor picks the access up
// from its output. A local blob is read from the source component version. A configured path
// has its `resource` and `component` aliases rewritten like the HTTP uploader's targetURL (see
// templateExpressions); the graph runtime resolves it.
func processRepositoryUploader(resource descriptorv2.Resource, access runtime.Typed, typ runtime.Type, rawURL, repository, path, baseID, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, resourceTransformIDs map[int]string, i int) error {
	if resource.Access == nil {
		return fmt.Errorf("resource access is required")
	}
	base, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid repository url: %w", err)
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
	data := map[string]any{
		"resource":         resource,
		"componentVersion": cv,
		"url":              rawURL,
		"repository":       repository,
	}
	if path != "" {
		templated, err := templateString(path, baseID, i)
		if err != nil {
			return fmt.Errorf("cannot template uploader path: %w", err)
		}
		data["path"] = templated
	}
	spec, err := runtime.UnstructuredFromMixedData(data)
	if err != nil {
		return fmt.Errorf("cannot create unstructured spec for %s transformation: %w", typ.Name, err)
	}

	uploadID := fmt.Sprintf("%sUpload%s", id, identityToTransformationID(resource.ToIdentity()))
	tgd.Transformations = append(tgd.Transformations, transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type:  typ,
			ID:    uploadID,
			Label: uploaderLabel(&val.Descriptor.Component, resource.Name, base.Host),
		},
		Spec: spec,
	})
	resourceTransformIDs[i] = uploadID
	return nil
}
