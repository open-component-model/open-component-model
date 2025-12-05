package graph

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/cel/expression/parser"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

func (b *Builder) getTransformationNodes(tgd *v1alpha1.TransformationGraphDefinition) (map[string]Transformation, error) {
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
		// TODO(fabianburth): we obviously will have to fetch the correct schema
		//   based on the transformation type from the plugin manager.
		schema := oci.Repository{}.JSONSchema()
		//for i, fd := range fieldDescriptors {
		//	fieldDescriptors[i].Path = fmt.Sprintf("spec.%s", fd.Path)
		//}

		//  transformation type
		// resource.uploader.oci --> oci.plugin (but this is not transformator)
		// resource.uploader.oci --> resource-upload-transformer(spec) output
		// resource.uploader.ctf --> resource-upload-transformer(spec) output

		transformations[transformation.ID] = Transformation{
			GenericTransformation: transformation,
			fieldDescriptors:      fieldDescriptors,
			order:                 order,
		}
	}
	return transformations, nil
}

//func discoverDependencies(graph *dag.DirectedAcyclicGraph[string], env *cel.Env) error {
//	functions := slices.Collect(maps.Keys(env.Functions()))
//	keys := slices.Collect(maps.Keys(graph.Vertices))
//
//	inspector := ast.NewInspectorWithEnv(env, keys, functions)
//
//	for id, vertex := range graph.Vertices {
//		ttransformation, ok := vertex.Attributes[syncdag.AttributeValue].(Transformation)
//		if !ok {
//			return fmt.Errorf("unknown transformation type for transformation %q", id)
//		}
//		for _, fieldDescriptor := range ttransformation.fieldDescriptors {
//			expressions, err := discoverExpressions(inspector, graph, id, fieldDescriptor)
//			if err != nil {
//				return fmt.Errorf("failed to discover resource expressions of transformation %q: %w", id, err)
//			}
//			ttransformation.expressions = append(ttransformation.expressions, expressions...)
//		}
//	}
//
//	return nil
//}
//
//func discoverExpressions(
//	inspector *ast.Inspector,
//	graph *dag.DirectedAcyclicGraph[string],
//	id string,
//	fieldDescriptor parser.FieldDescriptor,
//) ([]ast.ExpressionInspection, error) {
//	expressionInspections := make([]ast.ExpressionInspection, 0, len(fieldDescriptor.Expressions))
//	for _, expression := range fieldDescriptor.Expressions {
//		inspection, err := inspector.Inspect(expression.String)
//		if err != nil {
//			return nil, fmt.Errorf("failed to inspect expression %q: %w", expression.String, err)
//		}
//		for _, dependency := range inspection.ResourceDependencies {
//			if !graph.Contains(dependency.ID) {
//				return nil, fmt.Errorf("dependency %q of transformation %q not found in resolution graph", dependency.ID, id)
//			}
//			if err := graph.AddEdge(dependency.ID, id); err != nil {
//				return nil, err
//			}
//		}
//		expressionInspections = append(expressionInspections, inspection)
//	}
//	return expressionInspections, nil
//}
