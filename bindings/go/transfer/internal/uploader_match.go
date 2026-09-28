package internal

import (
	"fmt"
	"strconv"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"
	"cel.dev/cel-go/ext"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	githubv1 "ocm.software/open-component-model/bindings/go/github/spec/access/v1"
	helmv1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	ocifunctions "ocm.software/open-component-model/bindings/go/oci/cel/functions"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
	s3v2 "ocm.software/open-component-model/bindings/go/s3/spec/access/v2"
	graphenv "ocm.software/open-component-model/bindings/go/transform/graph/env"
	wgetv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

const (
	// targetAlias is the uploader alias for the transfer target. It is rewritten to a map
	// literal holding the target's type and its location fields.
	targetAlias = "target"
	// accessTypeAlias is the uploader alias for the canonical access type name of the
	// resource (see canonicalAccessType).
	accessTypeAlias = "accessType"

	isOCIManifestFunctionName = "isOCIManifest"
)

// EnvOptions are the CEL functions the transfer graph offers beyond the graph's base
// environment: toOCI() exactly as the controller offers it (OCI image accesses), the
// string extensions (split, join, ...) used to compose image references, and
// isOCIManifest() for match.when predicates.
func EnvOptions() []cel.EnvOption {
	return []cel.EnvOption{ocifunctions.ToOCI(), ext.Strings(), isOCIManifestFunction()}
}

// isOCIManifestFunction declares isOCIManifest(string) bool: whether a media type is an
// OCI image manifest or index, or a Docker manifest or manifest list.
func isOCIManifestFunction() cel.EnvOption {
	return cel.Function(isOCIManifestFunctionName,
		cel.Overload(isOCIManifestFunctionName+"_string", []*cel.Type{cel.StringType}, cel.BoolType,
			cel.UnaryBinding(func(v ref.Val) ref.Val {
				s, ok := v.Value().(string)
				if !ok {
					return types.NewErr("%s() expects a string, got %T", isOCIManifestFunctionName, v.Value())
				}
				return types.Bool(isOCICompliantManifest(s))
			}),
		),
	)
}

// uploaderEnv lazily builds the CEL environment uploader expressions (match.when and the
// OCI imageReference) are evaluated in while the graph is built: the component's
// descriptor environment node plus [EnvOptions], i.e. what the graph evaluates templates
// against at runtime.
type uploaderEnv struct {
	baseID string
	node   any
	env    *cel.Env
}

func (e *uploaderEnv) get() (*cel.Env, error) {
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

// canonicalAccessType names the access type of resource with aliases resolved (e.g.
// ociArtifact/v1 and ociImage/v1 are both OCIImage). Access types the transfer does not
// know keep their raw type name.
func canonicalAccessType(access runtime.Typed, resource descriptorv2.Resource) string {
	switch access.(type) {
	case *ociv1.OCIImage:
		return "OCIImage"
	case *helmv1.Helm:
		return "Helm"
	case *descriptorv2.LocalBlob:
		return "LocalBlob"
	case *wgetv1.Wget:
		return "Wget"
	case *s3v2.S3:
		return "S3"
	case *githubv1.GitHub:
		return "GitHub"
	default:
		return resource.Access.GetName()
	}
}

// targetLiteral returns the CEL map literal the target alias is rewritten to: an OCI
// registry has type, baseUrl and subPath; a CTF archive has type and filePath.
func targetLiteral(toSpec runtime.Typed) (string, error) {
	repo, err := convertToConcreteRepo(toSpec)
	if err != nil {
		return "", err
	}
	switch r := repo.(type) {
	case *oci.Repository:
		return fmt.Sprintf("{%q: %q, %q: %s, %q: %s}",
			"type", "OCIRepository",
			"baseUrl", strconv.Quote(r.BaseUrl),
			"subPath", strconv.Quote(r.SubPath)), nil
	case *ctfv1.Repository:
		return fmt.Sprintf("{%q: %q, %q: %s}",
			"type", "CommonTransportFormat",
			"filePath", strconv.Quote(r.FilePath)), nil
	default:
		return "", fmt.Errorf("unsupported target repository type %T", repo)
	}
}

// uploaderAliases returns the aliases uploader expressions of resource i see: `resource`
// as dyn(<its path in the descriptor environment node>), so fields of any access type can
// be tested and read; `accessType` as its canonical access type name; `target` as a map
// literal of the transfer target.
func uploaderAliases(env *uploaderEnv, i int, access runtime.Typed, resource descriptorv2.Resource, toSpec runtime.Typed) (map[string]string, error) {
	target, err := targetLiteral(toSpec)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		resourceAlias:   "dyn(" + resourceNodePath(env.baseID, i) + ")",
		accessTypeAlias: strconv.Quote(canonicalAccessType(access, resource)),
		targetAlias:     target,
	}, nil
}

// whenMatches evaluates the match.when predicate when with aliases rewritten. An empty
// predicate matches. A predicate that does not compile, does not evaluate, or does not
// return a bool is an error.
func whenMatches(when string, aliases map[string]string, env *uploaderEnv) (bool, error) {
	if when == "" {
		return true, nil
	}
	celEnv, err := env.get()
	if err != nil {
		return false, err
	}
	ast, issues := celEnv.Compile(rewriteExpression(when, aliases))
	if issues != nil && issues.Err() != nil {
		return false, fmt.Errorf("invalid match.when %q: %w", when, issues.Err())
	}
	prg, err := celEnv.Program(ast)
	if err != nil {
		return false, fmt.Errorf("invalid match.when %q: %w", when, err)
	}
	out, _, err := prg.Eval(map[string]any{})
	if err != nil {
		return false, fmt.Errorf("match.when %q does not evaluate: %w", when, err)
	}
	selected, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("match.when %q must evaluate to a bool, got %T", when, out.Value())
	}
	return selected, nil
}
