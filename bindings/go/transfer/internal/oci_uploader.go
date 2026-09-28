package internal

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"cel.dev/cel-go/cel"
	celast "cel.dev/cel-go/common/ast"
	"cel.dev/cel-go/ext"

	celparser "ocm.software/open-component-model/bindings/go/cel/expression/parser"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	helmv1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	ocifunctions "ocm.software/open-component-model/bindings/go/oci/cel/functions"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	graphenv "ocm.software/open-component-model/bindings/go/transform/graph/env"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

// targetAlias is the imageReference alias for the OCI registry target of the transfer.
// It is rewritten to a map literal holding the target's baseUrl and subPath.
const targetAlias = "target"

// EnvOptions are the CEL functions the transfer graph offers beyond the graph's base
// environment: toOCI() exactly as the controller offers it (OCI image accesses), and the
// string extensions (split, join, ...) used to compose image references.
func EnvOptions() []cel.EnvOption {
	return []cel.EnvOption{ocifunctions.ToOCI(), ext.Strings()}
}

// imageReferenceEnv lazily builds the CEL environment an OCI uploader template is
// evaluated in while the graph is built: the component's descriptor environment node plus
// [EnvOptions], i.e. what the graph evaluates the template against at runtime.
type imageReferenceEnv struct {
	baseID string
	node   any
	env    *cel.Env
}

func (e *imageReferenceEnv) get() (*cel.Env, error) {
	if e.env != nil {
		return e.env, nil
	}
	builder, err := graphenv.NewEnvBuilder(map[string]any{e.baseID: e.node})
	if err != nil {
		return nil, fmt.Errorf("cannot build CEL environment: %w", err)
	}
	builder.RegisterEnvOption(EnvOptions()...)
	env, _, err := builder.CurrentEnv()
	if err != nil {
		return nil, fmt.Errorf("cannot build CEL environment: %w", err)
	}
	e.env = env
	return env, nil
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

// expressionIdents reports the identifiers the CEL source expr uses. It parses with the
// same syntax options as the graph environment (e.g. optional field selection `a.?b`).
func expressionIdents(expr string) (map[string]bool, error) {
	env, err := cel.NewEnv(cel.OptionalTypes())
	if err != nil {
		return nil, err
	}
	parsed, issues := env.Parse(expr)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("cannot parse expression %q: %w", expr, issues.Err())
	}
	idents := map[string]bool{}
	celast.PreOrderVisit(celast.NavigateAST(parsed.NativeRep()), celast.NewExprVisitor(func(e celast.Expr) {
		if e.Kind() == celast.IdentKind {
			idents[e.AsIdent()] = true
		}
	}))
	return idents, nil
}

// ociImageReference templates the target image reference for resource under u (see
// [transferv1alpha1.DefaultOCIImageReference] for the template used when none is set)
// and reports whether the uploader applies.
//
// `resource` is rewritten to dyn(<resource's path in the descriptor environment node>):
// fields are resolved dynamically, so a template may test and read fields of any access
// type even if no resource in the descriptor carries them. `target` is rewritten to a map
// literal of the OCI registry target.
//
// The template is evaluated once here, against the same environment the graph uses. The
// uploader applies only if it evaluates to a string: a template that uses `target` on a
// non-OCI target, or that reads a field the resource does not have, does not apply and
// reason says why, so the resource falls through instead of failing the transfer. A
// template that does not compile fails the build. The emitted spec keeps the template, so
// the graph evaluates it again when it runs.
func ociImageReference(u *transferv1alpha1.OCIUploaderConfig, access runtime.Typed, refEnv *imageReferenceEnv, i int, toSpec runtime.Typed) (imageReference string, applies bool, reason string, err error) {
	if !ociUploadable(access) {
		return "", false, "access type is not uploadable as an OCI artifact", nil
	}
	targetLiteral, isOCITarget := ociTarget(toSpec)

	template := u.ImageReference
	if template == "" {
		template = transferv1alpha1.DefaultOCIImageReference
	}
	aliases := map[string]string{resourceAlias: "dyn(" + resourceNodePath(refEnv.baseID, i) + ")"}
	if isOCITarget {
		aliases[targetAlias] = targetLiteral
	}
	imageReference, exprs, err := templateString(template, aliases)
	if err != nil {
		return "", false, "", fmt.Errorf("cannot template imageReference: %w", err)
	}
	if !isOCITarget {
		for _, expr := range exprs {
			idents, err := expressionIdents(expr)
			if err != nil {
				return "", false, "", fmt.Errorf("invalid imageReference: %w", err)
			}
			if idents[targetAlias] {
				return "", false, fmt.Sprintf("imageReference uses %s, but target %s is not an OCI registry", targetAlias, targetKind(toSpec)), nil
			}
		}
	}

	env, err := refEnv.get()
	if err != nil {
		return "", false, "", err
	}
	fields, err := celparser.ParseSchemaless(map[string]any{"imageReference": imageReference})
	if err != nil {
		return "", false, "", fmt.Errorf("invalid imageReference: %w", err)
	}
	for _, field := range fields {
		for _, expr := range field.Expressions {
			ast, issues := env.Compile(expr.Value)
			if issues != nil && issues.Err() != nil {
				return "", false, "", fmt.Errorf("invalid imageReference: %w", issues.Err())
			}
			prg, err := env.Program(ast)
			if err != nil {
				return "", false, "", fmt.Errorf("invalid imageReference: %w", err)
			}
			out, _, err := prg.Eval(map[string]any{})
			if err != nil {
				return "", false, fmt.Sprintf("imageReference does not evaluate for the resource: %v", err), nil
			}
			if _, ok := out.Value().(string); !ok {
				return "", false, "", fmt.Errorf("invalid imageReference: expression %q evaluates to %T, not a string", strings.TrimSpace(expr.Value), out.Value())
			}
		}
	}
	return imageReference, true, "", nil
}

// processOCIUploader emits the transformations that upload resource as a separate OCI
// artifact according to u. It reports false without emitting anything when the
// uploader does not apply to the resource, so the caller falls through to the next
// uploader or the default handling. It returns the CEL spec-field expressions of the
// file buffers produced, for cleanup.
func processOCIUploader(ctx context.Context, resource descriptorv2.Resource, access runtime.Typed, u *transferv1alpha1.OCIUploaderConfig, refEnv *imageReferenceEnv, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, toSpec runtime.Typed, resourceTransformIDs map[int]string, i int) (bool, []string, error) {
	imageReference, ok, reason, err := ociImageReference(u, access, refEnv, i, toSpec)
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
