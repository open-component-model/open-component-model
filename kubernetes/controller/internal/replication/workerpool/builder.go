package workerpool

import (
	"context"

	"ocm.software/open-component-model/bindings/go/transform/graph/builder"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

// Graph is an executable transformation graph produced from a TGD.
// *builder.Graph from the transform bindings satisfies it.
type Graph interface {
	Process(ctx context.Context) error
}

// GraphBuilder builds and validates an executable [Graph] from a TGD.
//
// The reconciler constructs a concrete *builder.Builder via
// transfer.NewDefaultBuilder (carrying the effective credential graph) and
// adapts it with [NewGraphBuilder]. The interface keeps the pool decoupled from
// the credential and configuration machinery, which lands separately.
type GraphBuilder interface {
	BuildAndCheck(tgd *transformv1alpha1.TransformationGraphDefinition) (Graph, error)
}

// NewGraphBuilder adapts a concrete *builder.Builder to the [GraphBuilder]
// interface. The builder must not have a progress event channel configured via
// WithEvents: the worker pool does not drain progress events, and a configured
// channel would block Process once full.
func NewGraphBuilder(b *builder.Builder) GraphBuilder {
	return builderAdapter{builder: b}
}

type builderAdapter struct {
	builder *builder.Builder
}

func (a builderAdapter) BuildAndCheck(tgd *transformv1alpha1.TransformationGraphDefinition) (Graph, error) {
	graph, err := a.builder.BuildAndCheck(tgd)
	if err != nil {
		return nil, err
	}

	return graph, nil
}
