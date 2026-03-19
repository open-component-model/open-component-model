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
) error {
	env, err := ocmcel.BaseEnv()
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

	result, err := processAdditionalFields(ctx, env, resourceMap, resource.Spec.AdditionalStatusFields)
	if err != nil {
		return fmt.Errorf("failed to process additional status fields: %w", err)
	}
	resource.Status.Additional = result

	return nil
}

// processAdditionalFields recursively processes additional status fields.
// If a value is a JSON string, it is evaluated as a CEL expression.
// If a value is a JSON object, it is recursively processed.
func processAdditionalFields(
	ctx context.Context,
	env *cel.Env,
	resourceMap map[string]any,
	fields map[string]apiextensionsv1.JSON,
) (map[string]apiextensionsv1.JSON, error) {
	result := make(map[string]apiextensionsv1.JSON, len(fields))

	for name, val := range fields {
		resolved, err := processField(ctx, env, resourceMap, name, val)
		if err != nil {
			return nil, fmt.Errorf("failed to process field %s: %w", name, err)
		}
		result[name] = resolved
	}

	return result, nil
}

// processField resolves a single additional status field value.
// The value is either:
//   - a JSON string: treated as a CEL expression and evaluated via evalCEL
//   - a JSON object: each leaf value is recursively resolved as a CEL expression
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
func processField(
	ctx context.Context,
	env *cel.Env,
	resourceMap map[string]any,
	name string,
	val apiextensionsv1.JSON,
) (apiextensionsv1.JSON, error) {
	// Try to unmarshal as a string (CEL expression).
	var expr string
	if err := json.Unmarshal(val.Raw, &expr); err == nil {
		return evalCEL(ctx, env, resourceMap, name, expr)
	}

	// Try to unmarshal as an object (nested fields).
	var nested map[string]apiextensionsv1.JSON
	if err := json.Unmarshal(val.Raw, &nested); err == nil {
		nestedResult, err := processAdditionalFields(ctx, env, resourceMap, nested)
		if err != nil {
			return apiextensionsv1.JSON{}, fmt.Errorf("failed to process nested fields: %w", err)
		}
		raw, err := json.Marshal(nestedResult)
		if err != nil {
			return apiextensionsv1.JSON{}, fmt.Errorf("failed to marshal nested result %q: %w", name, err)
		}
		return apiextensionsv1.JSON{Raw: raw}, nil
	}

	return apiextensionsv1.JSON{}, fmt.Errorf("additional status field %q: value must be a CEL expression string or an object", name)
}

// evalCEL compiles and evaluates a single CEL expression against the resource data.
// The result is converted from CEL's internal types to native Go values via
// celValueToAny, then JSON-marshaled into an apiextensionsv1.JSON.
func evalCEL(
	ctx context.Context,
	env *cel.Env,
	resourceMap map[string]any,
	name string,
	expr string,
) (apiextensionsv1.JSON, error) {
	ast, issues := env.Compile(expr)
	if issues.Err() != nil {
		return apiextensionsv1.JSON{}, fmt.Errorf("failed to compile CEL expression %q: %w", name, issues.Err())
	}
	prog, err := env.Program(ast)
	if err != nil {
		return apiextensionsv1.JSON{}, fmt.Errorf("failed to build CEL program %q: %w", name, err)
	}
	val, _, err := prog.ContextEval(ctx, map[string]any{"resource": resourceMap})
	if err != nil {
		return apiextensionsv1.JSON{}, fmt.Errorf("failed to evaluate CEL expression %q: %w", name, err)
	}
	nativeVal, err := celconv.GoNativeType(val)
	if err != nil {
		return apiextensionsv1.JSON{}, fmt.Errorf("failed to convert CEL result %q: %w", name, err)
	}
	raw, err := json.Marshal(nativeVal)
	if err != nil {
		return apiextensionsv1.JSON{}, fmt.Errorf("failed to marshal CEL result %q: %w", name, err)
	}
	return apiextensionsv1.JSON{Raw: raw}, nil
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
