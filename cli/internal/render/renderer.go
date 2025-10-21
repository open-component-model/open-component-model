package render

import (
	"fmt"

	"context"
	"errors"
	"io"
	"log/slog"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
	"ocm.software/open-component-model/bindings/go/dag"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/internal/render/graph/list"
	"ocm.software/open-component-model/cli/internal/render/graph/tree"
	"ocm.software/open-component-model/cli/internal/repository/ocm"
)

func RenderComponents(cmd *cobra.Command, repoProvider ocm.ComponentVersionRepositoryForComponentProvider, roots []string, format string, mode string, recursive int) error {
	resAndDis := resolverAndDiscoverer{
		repositoryProvider: repoProvider,
		recursive:          recursive,
	}
	discoverer := syncdag.NewGraphDiscoverer(&syncdag.GraphDiscovererOptions[string, *descruntime.Descriptor]{
		Roots:      roots,
		Resolver:   &resAndDis,
		Discoverer: &resAndDis,
	})
	renderer, err := buildRenderer(cmd.Context(), discoverer.Graph(), roots, format)
	if err != nil {
		return fmt.Errorf("building renderer failed: %w", err)
	}

	switch mode {
	case StaticRenderMode:
		// Start traversing the graph from the root vertices (the initially resolved
		// component versions).
		err := discoverer.Discover(cmd.Context())
		if err != nil {
			return fmt.Errorf("traversing component version graph failed: %w", err)
		}
		if err := RenderOnce(cmd.Context(), renderer, WithWriter(cmd.OutOrStdout())); err != nil {
			return err
		}
	case LiveRenderMode:
		// Start the render loop.
		renderCtx, cancel := context.WithCancel(cmd.Context())
		wait := RunRenderLoop(renderCtx, renderer, WithRenderOptions(WithWriter(cmd.OutOrStdout())))
		// Start traversing the graph from the root vertices (the initially resolved
		// component versions).
		// The render loop is running concurrently and regularly displays the current
		// state of the graph.
		err := discoverer.Discover(cmd.Context())
		cancel()
		if err != nil {
			return fmt.Errorf("traversing component version graph failed: %w", err)
		}

		if err := wait(); !errors.Is(err, context.Canceled) {
			return fmt.Errorf("rendering component version graph failed: %w", err)
		}
	}
	return nil
}

func buildRenderer(ctx context.Context, dag *syncdag.SyncedDirectedAcyclicGraph[string], roots []string, format string) (Renderer, error) {
	// Initialize renderer based on the requested output format.
	switch format {
	case OutputFormatJSON.String():
		serializer := list.NewSerializer(list.WithVertexSerializer(list.VertexSerializerFunc[string](serializeVertexToDescriptor)), list.WithOutputFormat[string](OutputFormatJSON))
		return list.New(ctx, dag, list.WithListSerializer(serializer), list.WithRoots(roots...)), nil
	case OutputFormatNDJSON.String():
		serializer := list.NewSerializer(list.WithVertexSerializer(list.VertexSerializerFunc[string](serializeVertexToDescriptor)), list.WithOutputFormat[string](OutputFormatNDJSON))
		return list.New(ctx, dag, list.WithListSerializer(serializer), list.WithRoots(roots...)), nil
	case OutputFormatYAML.String():
		serializer := list.NewSerializer(list.WithVertexSerializer(list.VertexSerializerFunc[string](serializeVertexToDescriptor)), list.WithOutputFormat[string](OutputFormatYAML))
		return list.New(ctx, dag, list.WithListSerializer(serializer), list.WithRoots(roots...)), nil
	case OutputFormatTree.String():
		return tree.New(ctx, dag, tree.WithRoots(roots...)), nil
	case OutputFormatTable.String():
		serializer := list.ListSerializerFunc[string](serializeVerticesToTable)
		return list.New(ctx, dag, list.WithListSerializer(serializer), list.WithRoots(roots...)), nil
	default:
		return nil, fmt.Errorf("invalid output format %q", format)
	}
}

func serializeVertexToDescriptor(vertex *dag.Vertex[string]) (any, error) {
	untypedDescriptor, ok := vertex.Attributes[syncdag.AttributeValue]
	if !ok {
		return nil, fmt.Errorf("vertex %s has no %s attribute", vertex.ID, syncdag.AttributeValue)
	}
	descriptor, ok := untypedDescriptor.(*descruntime.Descriptor)
	if !ok {
		return nil, fmt.Errorf("expected vertex %s attribute %s to be of type %T, got type %T", vertex.ID, syncdag.AttributeValue, &descruntime.Descriptor{}, untypedDescriptor)
	}
	descriptorV2, err := descruntime.ConvertToV2(descriptorv2.Scheme, descriptor)
	if err != nil {
		return nil, fmt.Errorf("converting descriptor to v2 failed: %w", err)
	}
	return descriptorV2, nil
}

func serializeVerticesToTable(writer io.Writer, vertices []*dag.Vertex[string]) error {
	t := table.NewWriter()
	t.SetOutputMirror(writer)
	t.AppendHeader(table.Row{"Component", "Version", "Provider"})
	for _, vertex := range vertices {
		untypedDescriptor, ok := vertex.Attributes[syncdag.AttributeValue]
		if !ok {
			return fmt.Errorf("vertex %s has no %s attribute", vertex.ID, syncdag.AttributeValue)
		}
		descriptor, ok := untypedDescriptor.(*descruntime.Descriptor)
		if !ok {
			return fmt.Errorf("expected vertex %s attribute %s to be of type %T, got type %T", vertex.ID, syncdag.AttributeValue, &descruntime.Descriptor{}, descriptor)
		}

		t.AppendRow(table.Row{descriptor.Component.Name, descriptor.Component.Version, descriptor.Component.Provider.Name})
	}
	t.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1, AutoMerge: true},
		{Number: 3, AutoMerge: true},
	})
	style := table.StyleLight
	style.Options.DrawBorder = false
	t.SetStyle(style)
	t.Render()
	return nil
}

type resolverAndDiscoverer struct {
	repositoryProvider ocm.ComponentVersionRepositoryForComponentProvider
	recursive          int
}

var (
	_ syncdag.Resolver[string, *descruntime.Descriptor]   = (*resolverAndDiscoverer)(nil)
	_ syncdag.Discoverer[string, *descruntime.Descriptor] = (*resolverAndDiscoverer)(nil)
)

func (r *resolverAndDiscoverer) Resolve(ctx context.Context, key string) (*descruntime.Descriptor, error) {
	id, err := runtime.ParseIdentity(key)
	if err != nil {
		return nil, fmt.Errorf("parsing identity %q failed: %w", key, err)
	}
	component, version := id[descruntime.IdentityAttributeName], id[descruntime.IdentityAttributeVersion]
	repo, err := r.repositoryProvider.GetComponentVersionRepositoryForComponent(ctx, component, version)
	if err != nil {
		return nil, fmt.Errorf("getting component version repository for identity %q failed: %w", id, err)
	}
	desc, err := repo.GetComponentVersion(ctx, component, version)
	if err != nil {
		return nil, fmt.Errorf("getting component version for identity %q failed: %w", id, err)
	}
	return desc, nil
}

func (r *resolverAndDiscoverer) Discover(ctx context.Context, parent *descruntime.Descriptor) ([]string, error) {
	// TODO(fabianburth): Recursion depth has to be implemented as option for
	//  the dag discovery. Once that's implemented, we can pass the recursion
	//  depth to the discovery and remove this check here (https://github.com/open-component-model/ocm-project/issues/706).
	switch {
	case r.recursive < -1:
		return nil, fmt.Errorf("invalid recursion depth %d: must be -1 (unlimited) or >= 0", r.recursive)
	case r.recursive == 0:
		slog.InfoContext(ctx, "not discovering children, recursion depth 0", "component", parent.Component.ToIdentity().String())
		return nil, nil
	case r.recursive == -1:
		// unlimited recursion
		children := make([]string, len(parent.Component.References))
		for index, reference := range parent.Component.References {
			children[index] = reference.ToComponentIdentity().String()
		}
		slog.InfoContext(ctx, "discovering children", "component", parent.Component.ToIdentity().String(), "children", children)
		return children, nil
	case r.recursive > 0:
		return nil, fmt.Errorf("recursion depth > 0 not implemented yet")
	}
	return nil, fmt.Errorf("invalid recursion depth %d", r.recursive)
}
