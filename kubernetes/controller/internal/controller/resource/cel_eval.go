package resource

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/cel-go/cel"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	ocmcel "ocm.software/open-component-model/kubernetes/controller/internal/cel"
	celconv "ocm.software/open-component-model/kubernetes/controller/internal/controller/resource/conversion"
)

// ComputeAdditionalStatusFields compiles and evaluates CEL expressions for additional fields.
func ComputeAdditionalStatusFields(
	ctx context.Context,
	res *descriptor.Resource,
	resource *v1alpha1.Resource,
	component *v1alpha1.ComponentInfo,
) error {
	if resource.Spec.AdditionalStatusFields == nil || len(resource.Spec.AdditionalStatusFields.Raw) == 0 {
		return nil
	}

	var fields map[string]any
	if err := json.Unmarshal(resource.Spec.AdditionalStatusFields.Raw, &fields); err != nil {
		return fmt.Errorf("failed to unmarshal additionalStatusFields: %w", err)
	}

	env, err := ocmcel.ComponentInfoEnv(component)
	if err != nil {
		return fmt.Errorf("failed to get base CEL env: %w", err)
	}
	env, err = env.Extend(
		cel.Variable("resource", cel.DynType),
	)
	if err != nil {
		return fmt.Errorf("failed to extend CEL env: %w", err)
	}

	resV2, err := descriptor.ConvertToV2Resource(runtime.NewScheme(runtime.WithAllowUnknown()), res)
	if err != nil {
		return fmt.Errorf("failed to convert resource to v2: %w", err)
	}

	resourceMap, err := toGenericMapViaJSON(resV2)
	if err != nil {
		return fmt.Errorf("failed to prepare CEL variables: %w", err)
	}

	result, err := processAdditionalFields(ctx, env, resourceMap, fields)
	if err != nil {
		return fmt.Errorf("failed to process additional status fields: %w", err)
	}

	raw, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal additional status result: %w", err)
	}
	resource.Status.Additional = &apiextensionsv1.JSON{Raw: raw}

	return nil
}

// processAdditionalFields recursively processes a map of additional status fields.
// String values are evaluated as CEL expressions, nested maps are recursed into.
//
// This allows users to specify flat fields like:
//
//	additionalStatusFields:
//	  oci: "resource.access.toOCI()"
//
// or nested objects like:
//
//	additionalStatusFields:
//	  oci:
//	    registry: "resource.access.toOCI().registry"
//	    repository: "resource.access.toOCI().repository"
func processAdditionalFields(
	ctx context.Context,
	env *cel.Env,
	resourceMap map[string]any,
	fields map[string]any,
) (map[string]any, error) {
	result := make(map[string]any, len(fields))

	for name, val := range fields {
		switch v := val.(type) {
		case string:
			resolved, err := evalCEL(ctx, env, resourceMap, name, v)
			if err != nil {
				return nil, fmt.Errorf("failed to process field %s: %w", name, err)
			}
			result[name] = resolved
		case map[string]any:
			nested, err := processAdditionalFields(ctx, env, resourceMap, v)
			if err != nil {
				return nil, fmt.Errorf("failed to process field %s: %w", name, err)
			}
			result[name] = nested
		default:
			return nil, fmt.Errorf("additional status field %q: value must be a CEL expression string or an object", name)
		}
	}

	return result, nil
}

// evalCEL compiles and evaluates a single CEL expression against the resource data.
func evalCEL(
	ctx context.Context,
	env *cel.Env,
	resourceMap map[string]any,
	name string,
	expr string,
) (any, error) {
	ast, issues := env.Compile(expr)
	if issues.Err() != nil {
		return nil, fmt.Errorf("failed to compile CEL expression %q: %w", name, issues.Err())
	}
	prog, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("failed to build CEL program %q: %w", name, err)
	}
	val, _, err := prog.ContextEval(ctx, map[string]any{"resource": resourceMap})
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate CEL expression %q: %w", name, err)
	}
	return celconv.GoNativeType(val)
}

// toGenericMapViaJSON marshals and unmarshals a struct into a generic map representation through JSON tags.
func toGenericMapViaJSON(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return m, nil
}
