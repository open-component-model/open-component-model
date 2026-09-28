package internal

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"cel.dev/cel-go/cel"
	celast "cel.dev/cel-go/common/ast"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	helmv1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	ocifunctions "ocm.software/open-component-model/bindings/go/oci/cel/functions"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

// targetAlias is the imageReference alias for the OCI registry target of the transfer.
// It is rewritten to a map literal holding the target's baseUrl and subPath.
const targetAlias = "target"

// ToOCIEnvOption registers the toOCI() CEL function for the transfer graph. Besides
// OCIImage accesses (handled by the function itself) it resolves Helm accesses to their
// chart reference and local blobs holding an OCI manifest to their referenceName, the
// same way the OCI uploader derives references at graph build time.
func ToOCIEnvOption() cel.EnvOption {
	return ocifunctions.ToOCI(ocifunctions.WithReferenceResolver(func(raw *runtime.Raw) (ocifunctions.Reference, bool, error) {
		access, err := scheme.NewObject(raw.GetType())
		if err != nil {
			return ocifunctions.Reference{}, false, nil //nolint:nilerr // unknown access types are not resolvable here
		}
		if err := scheme.Convert(raw, access); err != nil {
			return ocifunctions.Reference{}, false, fmt.Errorf("cannot convert access of type %s: %w", raw.GetType(), err)
		}
		ref, ok, err := ociReference(access)
		if err != nil {
			return ocifunctions.Reference{}, false, err
		}
		if !ok {
			return ocifunctions.Reference{}, false, nil
		}
		return ref, true, nil
	}))
}

// ociUploadable reports whether access can be uploaded as an OCI artifact: OCI images,
// Helm charts, and local blobs holding an OCI manifest.
func ociUploadable(access runtime.Typed) bool {
	switch acc := access.(type) {
	case *ociv1.OCIImage, *helmv1.Helm:
		return true
	case *descriptorv2.LocalBlob:
		return isOCICompliantManifest(acc.MediaType)
	default:
		return false
	}
}

// ociReference returns the OCI reference toOCI() yields for access and reports whether
// one can be derived: an OCI image's imageReference, a Helm chart's chart reference, or
// a local blob's referenceName. A local blob without referenceName has none.
func ociReference(access runtime.Typed) (ocifunctions.Reference, bool, error) {
	switch acc := access.(type) {
	case *ociv1.OCIImage:
		ref, err := ocifunctions.ParseReference(acc.ImageReference)
		if err != nil {
			return ocifunctions.Reference{}, false, fmt.Errorf("cannot parse imageReference %q: %w", acc.ImageReference, err)
		}
		return ref, true, nil
	case *helmv1.Helm:
		chartRef, err := acc.ChartReference()
		if err != nil {
			return ocifunctions.Reference{}, false, fmt.Errorf("cannot derive Helm chart reference: %w", err)
		}
		ref, err := ocifunctions.ParseReference(chartRef)
		if err != nil {
			return ocifunctions.Reference{}, false, fmt.Errorf("cannot parse Helm chart reference %q: %w", chartRef, err)
		}
		return ref, true, nil
	case *descriptorv2.LocalBlob:
		if !isOCICompliantManifest(acc.MediaType) || acc.ReferenceName == "" {
			return ocifunctions.Reference{}, false, nil
		}
		ref, err := ocifunctions.ParseReference(acc.ReferenceName)
		if err != nil {
			return ocifunctions.Reference{}, false, fmt.Errorf("cannot parse referenceName %q: %w", acc.ReferenceName, err)
		}
		// A referenceName is repository[:tag], usually without registry. Like Docker
		// references, the first path component only names a registry if it contains a
		// "." or ":" or is "localhost"; otherwise it belongs to the repository.
		if ref.Host != "" && !strings.ContainsAny(ref.Host, ".:") && ref.Host != "localhost" {
			ref.Repository = ref.Host + "/" + ref.Repository
			ref.Host = ""
		}
		return ref, true, nil
	default:
		return ocifunctions.Reference{}, false, nil
	}
}

// ociTarget returns the CEL map literal the target alias is rewritten to for an OCI
// registry target, and reports false for any other target.
func ociTarget(toSpec runtime.Typed) (string, bool) {
	repo, err := convertToConcreteRepo(toSpec)
	if err != nil {
		return "", false
	}
	ociRepo, ok := repo.(*oci.Repository)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("{%q: %s, %q: %s}",
		"baseUrl", strconv.Quote(ociRepo.BaseUrl),
		"subPath", strconv.Quote(ociRepo.SubPath)), true
}

// expressionUses reports which identifiers and function names the CEL source expr uses.
func expressionUses(expr string) (idents, functions map[string]bool, err error) {
	env, err := cel.NewEnv()
	if err != nil {
		return nil, nil, err
	}
	parsed, issues := env.Parse(expr)
	if issues != nil && issues.Err() != nil {
		return nil, nil, fmt.Errorf("cannot parse expression %q: %w", expr, issues.Err())
	}
	idents, functions = map[string]bool{}, map[string]bool{}
	celast.PreOrderVisit(celast.NavigateAST(parsed.NativeRep()), celast.NewExprVisitor(func(e celast.Expr) {
		switch e.Kind() {
		case celast.IdentKind:
			idents[e.AsIdent()] = true
		case celast.CallKind:
			functions[e.AsCall().FunctionName()] = true
		}
	}))
	return idents, functions, nil
}

// ociImageReference templates the target image reference for resource under u (see
// [transferv1alpha1.DefaultOCIImageReference] for the template used when none is set)
// and reports whether the uploader applies. The `resource` alias points at the resource
// in the descriptor environment node and `target` at the OCI registry target. A
// template that uses `target` on a non-OCI target, or calls toOCI() for a resource
// without an OCI reference (a local blob without referenceName), does not apply; reason
// says why, so the resource falls through instead of failing the transfer.
func ociImageReference(u *transferv1alpha1.OCIUploaderConfig, access runtime.Typed, baseID string, i int, toSpec runtime.Typed) (imageReference string, applies bool, reason string, err error) {
	if !ociUploadable(access) {
		return "", false, "access type is not uploadable as an OCI artifact", nil
	}
	_, hasReference, err := ociReference(access)
	if err != nil {
		return "", false, "", err
	}
	targetLiteral, isOCITarget := ociTarget(toSpec)

	template := u.ImageReference
	if template == "" {
		template = transferv1alpha1.DefaultOCIImageReference
	}
	aliases := map[string]string{resourceAlias: resourceNodePath(baseID, i)}
	if isOCITarget {
		aliases[targetAlias] = targetLiteral
	}
	imageReference, exprs, err := templateString(template, aliases)
	if err != nil {
		return "", false, "", fmt.Errorf("cannot template imageReference: %w", err)
	}
	for _, expr := range exprs {
		idents, functions, err := expressionUses(expr)
		if err != nil {
			return "", false, "", fmt.Errorf("invalid imageReference: %w", err)
		}
		if !isOCITarget && idents[targetAlias] {
			return "", false, fmt.Sprintf("imageReference uses %s, but target %s is not an OCI registry", targetAlias, targetKind(toSpec)), nil
		}
		if !hasReference && functions[ocifunctions.ToOCIFunctionName] {
			return "", false, fmt.Sprintf("imageReference calls %s(), but the resource has no OCI reference", ocifunctions.ToOCIFunctionName), nil
		}
	}
	return imageReference, true, "", nil
}

// processOCIUploader emits the transformations that upload resource as a separate OCI
// artifact according to u. It reports false without emitting anything when the
// uploader does not apply to the resource, so the caller falls through to the next
// uploader or the default handling. It returns the CEL spec-field expressions of the
// file buffers produced, for cleanup.
func processOCIUploader(ctx context.Context, resource descriptorv2.Resource, access runtime.Typed, u *transferv1alpha1.OCIUploaderConfig, baseID, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int) (bool, []string, error) {
	imageReference, ok, reason, err := ociImageReference(u, access, baseID, i, toSpec)
	if err != nil {
		return false, nil, err
	}
	if !ok {
		slog.DebugContext(ctx, "oci uploader does not apply to resource, using default handling",
			"component", val.Descriptor.Component.Name, "version", val.Descriptor.Component.Version,
			"resource", resource.ToIdentity().String(), "accessType", resource.Access.Type.String(),
			"target", targetKind(toSpec), "reason", reason)
		return false, nil, nil
	}

	resourceID := identityToTransformationID(resource.ToIdentity())
	switch access.(type) {
	case *ociv1.OCIImage:
		// Streaming (TransferOCIArtifact) produces no temp file, so there is nothing to clean up.
		if err := processOCIArtifactStreaming(resource, id, tgd, resourceTransformIDs, i, imageReference, transferLabel(&val.Descriptor.Component, resource.Name, toSpec)); err != nil {
			return false, nil, fmt.Errorf("cannot process OCI artifact resource: %w", err)
		}
		return true, nil, nil
	case *helmv1.Helm:
		if err := processHelm(resource, id, val, tgd, toSpec, resourceTransformIDs, i, imageReference); err != nil {
			return false, nil, fmt.Errorf("cannot process Helm Chart resource: %w", err)
		}
		return true, helmFileExpressions(id, resourceID), nil
	case *descriptorv2.LocalBlob:
		if err := processLocalBlob(resource, id, val, tgd, toSpec, resourceTransformIDs, i, imageReference); err != nil {
			return false, nil, fmt.Errorf("failed processing local blob resource: %w", err)
		}
		return true, []string{fmt.Sprintf("${%sAdd%s.spec.file}", id, resourceID)}, nil
	default:
		// ociUploadable only admits the access types above.
		return false, nil, fmt.Errorf("unsupported access type %T for oci uploader", access)
	}
}
