package analysis

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/cel-go/cel"
	"ocm.software/open-component-model/bindings/go/cel/expression/fieldpath"
	"ocm.software/open-component-model/bindings/go/cel/expression/variable"
	"ocm.software/open-component-model/bindings/go/cel/jsonschema/decl/check"
	stv6jsonschema "ocm.software/open-component-model/bindings/go/cel/jsonschema/santhosh-tekuri/v6"
	"ocm.software/open-component-model/bindings/go/transform/graph"
	"ocm.software/open-component-model/bindings/go/transform/graph/env"
	"ocm.software/open-component-model/bindings/go/transform/graph/registry"
)

type StaticPluginAnalysisProcessor struct {
	Registry                *registry.Registry
	Builder                 *env.Builder
	AnalyzedTransformations map[string]graph.Transformation
}

func (b *StaticPluginAnalysisProcessor) ProcessValue(_ context.Context, transformation graph.Transformation) error {
	celEnv, provider, err := b.Builder.CurrentEnv()
	if err != nil {
		return err
	}

	for i, fieldDescriptor := range transformation.FieldDescriptors {
		for j, expression := range fieldDescriptor.Expressions {
			ast, issues := celEnv.Compile(expression.Value)
			if issues.Err() != nil {
				return fmt.Errorf("cannot compile expression %q: %w", expression.Value, issues.Err())
			}
			fieldDescriptor.Expressions[j].AST = ast
		}
		transformation.FieldDescriptors[i] = fieldDescriptor
	}

	typ := transformation.GetType()
	if typ.IsEmpty() {
		return fmt.Errorf("transformation type after render is empty")
	}

	genericTransformation, ok := b.Registry.GetTransformation(typ)
	if !ok {
		return fmt.Errorf("no transformation registered for type %q", typ.String())
	}

	declType, err := genericTransformation.GetDeclType()
	if err != nil {
		return fmt.Errorf("getting decl type for transformation type %q: %w", typ.String(), err)
	}
	b.Builder.RegisterDeclTypes(declType)
	b.Builder.RegisterEnvOption(cel.Variable(transformation.ID, declType.CelType()))
	transformation.DeclType = declType

	if celEnv, provider, err = b.Builder.CurrentEnv(); err != nil {
		return err
	}

	specDeclType := declType.DeclTypeFromProperty("spec")
	specFieldDescriptors, err := stv6jsonschema.ParseResourceFromDeclType(transformation.Spec.Data, specDeclType)
	if err != nil {
		return fmt.Errorf("validate transformation resource against schema: %w", err)
	}

	slices.SortFunc(transformation.FieldDescriptors, func(a, b variable.FieldDescriptor) int {
		return fieldpath.Compare(a.Path, b.Path)
	})

	for i, fieldDescriptor := range transformation.FieldDescriptors {
		for j, expression := range fieldDescriptor.Expressions {
			outputType := expression.AST.OutputType()
			expectedType := specFieldDescriptors[i].ExpectedType
			ok, err := check.AreTypesStructurallyCompatible(outputType, expectedType, provider)
			if err != nil {
				return fmt.Errorf("checking type compatibility for expression %q at path %s failed: %w", expression, fieldDescriptor.Path, err)
			}
			if !ok {
				return fmt.Errorf("expression output type %s is not assignable to expected type %s for path %s based on schema",
					outputType.TypeName(),
					specFieldDescriptors[i].ExpectedType.TypeName(),
					fieldDescriptor.Path,
				)
			}
			specFieldDescriptors[i].Expressions[j].AST = expression.AST
		}
	}
	transformation.FieldDescriptors = specFieldDescriptors

	b.AnalyzedTransformations[transformation.ID] = transformation

	return nil
}
