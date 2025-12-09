package builder

import (
	"context"
	"fmt"

	"github.com/google/cel-go/cel"
	"ocm.software/open-component-model/bindings/go/dag"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transform/graph"
	"ocm.software/open-component-model/bindings/go/transform/graph/analysis"
	graphEnv "ocm.software/open-component-model/bindings/go/transform/graph/env"
	"ocm.software/open-component-model/bindings/go/transform/graph/registry"
	graphRuntime "ocm.software/open-component-model/bindings/go/transform/graph/runtime"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

type Builder struct {
	// holds all possible transformations
	transformationRegistry *registry.Registry
	transformers           map[runtime.Type]graphRuntime.Transformer
}

func NewBuilder() *Builder {
	return &Builder{transformationRegistry: registry.NewRegistry()}
}

func (b *Builder) BuildAndCheck(original *v1alpha1.TransformationGraphDefinition) (*Graph, error) {
	tgd := original.DeepCopy()

	nodes, err := getTransformationNodes(tgd)
	if err != nil {
		return nil, err
	}

	g := dag.NewDirectedAcyclicGraph[string]()
	for _, node := range nodes {
		if err := g.AddVertex(node.ID, map[string]any{syncdag.AttributeValue: node}); err != nil {
			return nil, err
		}
	}
	environmentData := tgd.GetEnvironmentData()
	builder, err := graphEnv.NewEnvBuilder(environmentData)
	if err != nil {
		return nil, err
	}
	env, _, err := builder.CurrentEnv()
	if err != nil {
		return nil, err
	}
	if err := discoverDependencies(g, env); err != nil {
		return nil, fmt.Errorf("error discovering dependencies: %v", err)
	}

	synced := syncdag.ToSyncedGraph(g)

	pluginProcessor := &analysis.StaticPluginAnalysisProcessor{
		Registry:                b.transformationRegistry,
		Builder:                 builder,
		AnalyzedTransformations: make(map[string]graph.Transformation),
	}

	staticAnalysisProcessor := syncdag.NewGraphProcessor(synced, &syncdag.GraphProcessorOptions[string, graph.Transformation]{
		Processor:   pluginProcessor,
		Concurrency: 1,
	})

	if err := staticAnalysisProcessor.Process(context.TODO()); err != nil {
		return nil, err
	}
	// refresh env after analysis
	if env, _, err = builder.CurrentEnv(); err != nil {
		return nil, err
	}

	for _, vertex := range g.Vertices {
		vertex.Attributes[syncdag.AttributeValue] = pluginProcessor.AnalyzedTransformations[vertex.ID]
	}

	return &Graph{
		env:          env,
		checked:      g,
		transformers: b.transformers,
	}, nil
}

func (b *Builder) WithTransformer(runtimeType runtime.Type, typed interface {
	runtime.Typed
	registry.GenericTransformation
}, transformer graphRuntime.Transformer) *Builder {
	if b.transformers == nil {
		b.transformers = map[runtime.Type]graphRuntime.Transformer{}
	}
	if err := b.transformationRegistry.RegisterTransformation(runtimeType, typed); err != nil {
		panic(fmt.Sprintf("failed to register transformation type %s: %v", runtimeType, err))
	}
	if _, exists := b.transformers[runtimeType]; exists {
		panic(fmt.Sprintf("transformer for type %s already registered", runtimeType))
	}
	b.transformers[runtimeType] = transformer
	return b
}

type Graph struct {
	env          *cel.Env
	checked      *dag.DirectedAcyclicGraph[string]
	transformers map[runtime.Type]graphRuntime.Transformer
}

func (g *Graph) Process(ctx context.Context) error {
	synced := syncdag.ToSyncedGraph(g.checked)
	runtimeEvaluationProcessor := syncdag.NewGraphProcessor(synced, &syncdag.GraphProcessorOptions[string, graph.Transformation]{
		Processor: &graphRuntime.Runtime{
			Environment:              g.env,
			Transformers:             g.transformers,
			EvaluatedExpressionCache: make(map[string]any),
			EvaluatedTransformations: make(map[string]any),
		},
		Concurrency: 1,
	})
	return runtimeEvaluationProcessor.Process(ctx)
}
