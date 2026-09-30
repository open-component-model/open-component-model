package internal

import (
	"context"
	"fmt"
	"strings"

	celparser "ocm.software/open-component-model/bindings/go/cel/expression/parser"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	helmv1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

// ociImageReference templates the target image reference under u (see
// [transferv1alpha1.DefaultOCIImageReference] for the template used when none is set),
// with aliases (see uploaderAliases) rewritten.
//
// The template is evaluated once here, against the same environment the graph uses, so a
// template that does not evaluate for the selected resource (e.g. reading target.baseUrl
// on a CTF target, or a field the resource does not have) fails the build instead of the
// running transfer. The emitted spec keeps the template, so the graph evaluates it again
// when it runs.
func ociImageReference(ctx context.Context, u *transferv1alpha1.OCIUploaderConfig, aliases map[string]string, env *uploaderEnv) (string, error) {
	template := u.ImageReference
	if template == "" {
		template = transferv1alpha1.DefaultOCIImageReference
	}
	imageReference, _, err := templateString(template, aliases)
	if err != nil {
		return "", fmt.Errorf("cannot template imageReference: %w", err)
	}

	fields, err := celparser.ParseSchemaless(map[string]any{"imageReference": imageReference})
	if err != nil {
		return "", fmt.Errorf("invalid imageReference: %w", err)
	}
	for _, field := range fields {
		for _, expr := range field.Expressions {
			prg, err := env.program(expr.Value)
			if err != nil {
				return "", fmt.Errorf("invalid imageReference: %w", err)
			}
			out, _, err := prg.ContextEval(ctx, map[string]any{})
			if err != nil {
				return "", fmt.Errorf("imageReference does not evaluate: %w", err)
			}
			if _, ok := out.Value().(string); !ok {
				return "", fmt.Errorf("invalid imageReference: expression %q evaluates to %T, not a string", strings.TrimSpace(expr.Value), out.Value())
			}
		}
	}
	return imageReference, nil
}

// processOCIUploader emits the transformations that upload resource, selected by u, as a
// separate OCI artifact. A resource the uploader cannot upload is an error: the uploader's
// match selected it, so the config must be adjusted. It returns the CEL spec-field
// expressions of the file buffers produced, for cleanup.
func processOCIUploader(ctx context.Context, resource descriptorv2.Resource, access runtime.Typed, u *transferv1alpha1.OCIUploaderConfig, aliases map[string]string, env *uploaderEnv, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int) ([]string, error) {
	switch acc := access.(type) {
	case *ociv1.OCIImage, *helmv1.Helm:
	case *descriptorv2.LocalBlob:
		if !isOCICompliantManifest(acc.MediaType) {
			return nil, fmt.Errorf("oci uploader cannot upload local blob with media type %q: not an OCI manifest (adjust match)", acc.MediaType)
		}
	default:
		return nil, fmt.Errorf("oci uploader cannot upload access type %s (adjust match)", resource.Access.Type)
	}

	imageReference, err := ociImageReference(ctx, u, aliases, env)
	if err != nil {
		return nil, err
	}

	resourceID := identityToTransformationID(resource.ToIdentity())
	switch access.(type) {
	case *ociv1.OCIImage:
		// Streaming (TransferOCIArtifact) produces no temp file, so there is nothing to clean up.
		if err := processOCIArtifactStreaming(resource, id, tgd, resourceTransformIDs, i, imageReference, transferLabel(&val.Descriptor.Component, resource.Name, toSpec)); err != nil {
			return nil, fmt.Errorf("cannot process OCI artifact resource: %w", err)
		}
		return nil, nil
	case *helmv1.Helm:
		if err := processHelm(resource, id, val, tgd, toSpec, resourceTransformIDs, i, imageReference); err != nil {
			return nil, fmt.Errorf("cannot process Helm Chart resource: %w", err)
		}
		return helmFileExpressions(id, resourceID), nil
	default: // *descriptorv2.LocalBlob holding an OCI manifest, checked above
		if err := processLocalBlob(resource, id, val, tgd, toSpec, resourceTransformIDs, i, imageReference); err != nil {
			return nil, fmt.Errorf("failed processing local blob resource: %w", err)
		}
		return []string{fmt.Sprintf("${%sAdd%s.spec.file}", id, resourceID)}, nil
	}
}
