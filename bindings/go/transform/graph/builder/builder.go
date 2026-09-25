package builder

import (
	"context"
	"fmt"
	runtimepkg "runtime"

	"cel.dev/cel-go/cel"

	"ocm.software/open-component-model/bindings/go/dag"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transform/graph"
	"ocm.software/open-component-model/bindings/go/transform/graph/analysis"
	graphEnv "ocm.software/open-component-model/bindings/go/transform/graph/env"
	graphRuntime "ocm.software/open-component-model/bindings/go/transform/graph/runtime"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

// DefaultConcurrency is used when no explicit concurrency is configured on the
// Builder. It scales with the machine so large transformation graphs process
// independent nodes in parallel out of the box.
var DefaultConcurrency = runtimepkg.NumCPU()

type Builder struct {
	scheme       *runtime.Scheme
	transformers map[runtime.Type]graphRuntime.Transformer
	events       chan graphRuntime.ProgressEvent
	buildEvents  chan graphRuntime.ProgressEvent
	concurrency  int
}

func NewBuilder(scheme *runtime.Scheme) *Builder {
	return &Builder{scheme: scheme, transformers: map[runtime.Type]graphRuntime.Transformer{}}
}

// WithConcurrency sets the maximum number of transformation nodes that are
// processed in parallel during both static analysis and runtime evaluation.
// Independent nodes (those without a dependency relationship) run concurrently
// up to this limit, while dependency ordering is always respected. A value
// <= 0 resets the Builder to DefaultConcurrency.
func (b *Builder) WithConcurrency(concurrency int) *Builder {
	b.concurrency = concurrency
	return b
}

// resolvedConcurrency returns the effective concurrency limit, falling back to
// DefaultConcurrency when none was configured.
func (b *Builder) resolvedConcurrency() int {
	if b.concurrency > 0 {
		return b.concurrency
	}
	return DefaultConcurrency
}

func (b *Builder) BuildAndCheck(original *v1alpha1.TransformationGraphDefinition) (*Graph, error) {
	if b.buildEvents != nil {
		// Reset after closing so a reused builder cannot send to or close the
		// stale channel.
		defer func() {
			close(b.buildEvents)
			b.buildEvents = nil
		}()
	}
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
		return nil, fmt.Errorf("error discovering dependencies: %w", err)
	}

	synced := syncdag.ToSyncedGraph(g)

	pluginProcessor := &analysis.StaticPluginAnalysisProcessor{
		Scheme:                  b.scheme,
		Builder:                 builder,
		AnalyzedTransformations: make(map[string]graph.Transformation),
	}

	concurrency := b.resolvedConcurrency()

	var processor syncdag.Processor[graph.Transformation] = pluginProcessor
	if b.buildEvents != nil {
		processor = &progressProcessor{inner: pluginProcessor, events: b.buildEvents}
	}

	staticAnalysisProcessor := syncdag.NewGraphProcessor(synced, &syncdag.GraphProcessorOptions[string, graph.Transformation]{
		Processor: processor,
		// Independent nodes are analyzed in parallel. The shared env.Builder is
		// concurrency-safe and StaticPluginAnalysisProcessor guards its
		// AnalyzedTransformations map, so no ordering hazard remains beyond the
		// dependency ordering the DAG processor already enforces.
		Concurrency: concurrency,
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
		events:       b.events,
		concurrency:  concurrency,
	}, nil
}

func (b *Builder) WithTransformer(typed interface {
	runtime.Typed
	runtime.JSONSchemaIntrospectable
}, transformer graphRuntime.Transformer,
) *Builder {
	if b.transformers == nil {
		b.transformers = map[runtime.Type]graphRuntime.Transformer{}
	}
	runtimeType, err := b.scheme.TypeForPrototype(typed)
	if err != nil {
		panic(fmt.Sprintf("cannot get runtime type for transformer: %v", err))
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
	events       chan graphRuntime.ProgressEvent
	concurrency  int
}

func (g *Graph) Process(ctx context.Context) error {
	synced := syncdag.ToSyncedGraph(g.checked)
	runtimeEvaluationProcessor := syncdag.NewGraphProcessor(synced, &syncdag.GraphProcessorOptions[string, graph.Transformation]{
		Processor: &graphRuntime.Runtime{
			Environment:              g.env,
			Transformers:             g.transformers,
			EvaluatedExpressionCache: make(map[string]any),
			EvaluatedTransformations: make(map[string]any),
			Events:                   g.events,
		},
		// Independent nodes are evaluated in parallel. Runtime guards its
		// EvaluatedExpressionCache and EvaluatedTransformations maps, and the
		// DAG processor guarantees predecessors finish before dependents run.
		Concurrency: g.concurrency,
	})

	err := runtimeEvaluationProcessor.Process(ctx)
	if g.events != nil {
		close(g.events)
	}

	return err
}

// WithEvents sets the channel where progress events will be sent during Process().
// This is optional - if not set, no events will be emitted.
func (b *Builder) WithEvents(events chan graphRuntime.ProgressEvent) *Builder {
	b.events = events
	return b
}

// WithBuildEvents sets the channel where progress events are sent during
// BuildAndCheck. This is optional - if not set, no events are emitted.
// Unlike the channel passed to [Builder.WithEvents], which [Graph.Process]
// closes, BuildAndCheck closes the build events channel before it returns.
// The channel is single-use: to report progress for a later build on the same
// builder, call WithBuildEvents again.
func (b *Builder) WithBuildEvents(events chan graphRuntime.ProgressEvent) *Builder {
	b.buildEvents = events
	return b
}

// progressProcessor wraps the static analysis processor to report the build
// progress of each transformation on the build events channel.
type progressProcessor struct {
	inner  syncdag.Processor[graph.Transformation]
	events chan<- graphRuntime.ProgressEvent
}

func (p *progressProcessor) ProcessValue(ctx context.Context, transformation graph.Transformation) error {
	t := &transformation
	p.events <- graphRuntime.ProgressEvent{Transformation: t, State: graphRuntime.Running}
	if err := p.inner.ProcessValue(ctx, transformation); err != nil {
		p.events <- graphRuntime.ProgressEvent{Transformation: t, State: graphRuntime.Failed, Err: err}
		return err
	}
	p.events <- graphRuntime.ProgressEvent{Transformation: t, State: graphRuntime.Completed}
	return nil
}

// Events returns the channel where progress events are sent during Process().
func (g *Graph) Events() <-chan graphRuntime.ProgressEvent {
	return g.events
}

// NodeCount returns the total number of nodes in the graph.
func (g *Graph) NodeCount() int {
	return len(g.checked.Vertices)
}

// Concurrency returns the maximum number of transformation nodes that
// [Graph.Process] evaluates in parallel.
func (g *Graph) Concurrency() int {
	return g.concurrency
}
