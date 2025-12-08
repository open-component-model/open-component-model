package graph

import (
	"fmt"
	"maps"
	"slices"

	"github.com/google/cel-go/cel"
	ast "ocm.software/open-component-model/bindings/go/cel/expression/inspector"
	"ocm.software/open-component-model/bindings/go/cel/expression/parser"
	"ocm.software/open-component-model/bindings/go/cel/expression/variable"
	"ocm.software/open-component-model/bindings/go/dag"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

func getTransformationNodes(tgd *v1alpha1.TransformationGraphDefinition) (map[string]Transformation, error) {
	transformations := make(map[string]Transformation, len(tgd.Transformations))
	for order, transformation := range tgd.Transformations {
		typ := transformation.GetType()
		if typ.IsEmpty() {
			return nil, fmt.Errorf("transformations type is empty")
		}
		fieldDescriptors, err := parser.ParseSchemaless(transformation.Spec.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse resource of type %s: %w", typ, err)
		}
		if _, exists := transformations[transformation.ID]; exists {
			return nil, fmt.Errorf("duplicate transformation ID %s", transformation.ID)
		}
		transformations[transformation.ID] = Transformation{
			GenericTransformation: transformation,
			fieldDescriptors:      fieldDescriptors,
			order:                 order,
		}
	}
	return transformations, nil
}

func discoverDependencies(graph *dag.DirectedAcyclicGraph[string], env *cel.Env) error {
	keys := slices.Collect(maps.Keys(graph.Vertices))

	inspector := ast.NewInspectorWithEnv(env, append(keys))

	for id, vertex := range graph.Vertices {
		ttransformation, ok := vertex.Attributes[syncdag.AttributeValue].(Transformation)
		if !ok {
			return fmt.Errorf("unknown transformation type for transformation %q", id)
		}
		for _, fieldDescriptor := range ttransformation.fieldDescriptors {
			expressions, err := discoverExpressions(inspector, graph, id, fieldDescriptor)
			if err != nil {
				return fmt.Errorf("failed to discover resource expressions of transformation %q: %w", id, err)
			}
			ttransformation.expressions = append(ttransformation.expressions, expressions...)
		}
	}

	return nil
}

func discoverExpressions(
	inspector *ast.Inspector,
	graph *dag.DirectedAcyclicGraph[string],
	id string,
	fieldDescriptor variable.FieldDescriptor,
) ([]ast.ExpressionInspection, error) {
	expressionInspections := make([]ast.ExpressionInspection, 0, len(fieldDescriptor.Expressions))
	for _, expression := range fieldDescriptor.Expressions {
		inspection, err := inspector.Inspect(expression.Value)
		if err != nil {
			return nil, fmt.Errorf("failed to inspect expression %q: %w", expression, err)
		}
		for _, dependency := range inspection.ResourceDependencies {
			if !graph.Contains(dependency.ID) {
				return nil, fmt.Errorf("dependency %q of transformation %q not found in resolution graph", dependency.ID, id)
			}
			if err := graph.AddEdge(dependency.ID, id); err != nil {
				return nil, err
			}
		}
		expressionInspections = append(expressionInspections, inspection)
	}
	return expressionInspections, nil
}
