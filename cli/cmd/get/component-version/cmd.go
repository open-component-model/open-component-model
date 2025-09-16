package componentversion

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	resolverruntime "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/runtime"
	resolverv1 "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/spec"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/enum"
	"ocm.software/open-component-model/cli/internal/reference/compref"
	"ocm.software/open-component-model/cli/internal/render"
	"ocm.software/open-component-model/cli/internal/render/graph/list"
	"ocm.software/open-component-model/cli/internal/render/graph/tree"
	"ocm.software/open-component-model/cli/internal/repository/ocm"
)

const (
	FlagSemverConstraint = "semver-constraint"
	FlagOutput           = "output"
	FlagDisplayMode      = "display-mode"
	FlagConcurrencyLimit = "concurrency-limit"
	FlagLatest           = "latest"
	FlagRecursive        = "recursive"
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:        "component-version {reference}",
		Aliases:    []string{"cv", "component-versions", "cvs", "componentversion", "componentversions", "component", "components", "comp", "comps", "c"},
		SuggestFor: []string{"version", "versions"},
		Short:      "Get component version(s) from an OCM repository",
		Args:       cobra.MatchAll(cobra.ExactArgs(1), ComponentReferenceAsFirstPositional),
		Long: fmt.Sprintf(`Get component version(s) from an OCM repository.

The format of a component reference is:
	[type::]{repository}/[valid-prefix]/{component}[:version]

For valid prefixes {%[1]s|none} are available. If <none> is used, it defaults to %[1]q. This is because by default,
OCM components are stored within a specific sub-repository.

For known types, currently only {%[2]s} are supported, which can be shortened to {%[3]s} respectively for convenience.

If no type is given, the repository path is interpreted based on introspection and heuristics.
`,
			compref.DefaultPrefix,
			strings.Join([]string{ociv1.Type, ctfv1.Type}, "|"),
			strings.Join([]string{ociv1.ShortType, ociv1.ShortType2, ctfv1.ShortType, ctfv1.ShortType2}, "|"),
		),
		Example: strings.TrimSpace(`
Getting a single component version:

get component-version ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0
get cv ./path/to/ctf//ocm.software/ocmcli:0.23.0
get cv ./path/to/ctf/component-descriptors/ocm.software/ocmcli:0.23.0

Listing many component versions:

get component-versions ghcr.io/open-component-model/ocm//ocm.software/ocmcli
get cvs ghcr.io/open-component-model/ocm//ocm.software/ocmcli --output json
get cvs ghcr.io/open-component-model/ocm//ocm.software/ocmcli -oyaml

Specifying types and schemes:

get cv ctf::github.com/locally-checked-out-repo//ocm.software/ocmcli:0.23.0
get cvs oci::http://localhost:8080//ocm.software/ocmcli
`),
		RunE:              GetComponentVersion,
		DisableAutoGenTag: true,
	}

	enum.VarP(cmd.Flags(), FlagOutput, "o", []string{render.OutputFormatTable.String(), render.OutputFormatYAML.String(), render.OutputFormatJSON.String(), render.OutputFormatNDJSON.String(), render.OutputFormatTree.String()}, "output format of the component descriptors")
	enum.VarP(cmd.Flags(), FlagDisplayMode, "", []string{render.StaticRenderMode, render.LiveRenderMode}, `display mode can be used in combination with --recursive
  static: print the output once the complete component graph is discovered
  live (experimental): continuously updates the output to represent the current discovery state of the component graph`)
	cmd.Flags().String(FlagSemverConstraint, "> 0.0.0-0", "semantic version constraint restricting which versions to output")
	cmd.Flags().Int(FlagConcurrencyLimit, 4, "maximum amount of parallel requests to the repository for resolving component versions")
	cmd.Flags().Bool(FlagLatest, false, "if set, only the latest version of the component is returned")
	cmd.Flags().Int(FlagRecursive, 0, "depth of recursion for resolving referenced component versions (0=none, -1=unlimited, >0=levels (not implemented yet))")
	cmd.Flags().Lookup(FlagRecursive).NoOptDefVal = "-1"

	return cmd
}

func ComponentReferenceAsFirstPositional(_ *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing component reference as first positional argument")
	}
	if _, err := compref.Parse(args[0]); err != nil {
		return fmt.Errorf("parsing component reference from first position argument %q failed: %w", args[0], err)
	}
	return nil
}

func GetComponentVersion(cmd *cobra.Command, args []string) error {
	pluginManager := ocmctx.FromContext(cmd.Context()).PluginManager()
	if pluginManager == nil {
		return fmt.Errorf("could not retrieve plugin manager from context")
	}

	credentialGraph := ocmctx.FromContext(cmd.Context()).CredentialGraph()
	if credentialGraph == nil {
		return fmt.Errorf("could not retrieve credential graph from context")
	}

	output, err := enum.Get(cmd.Flags(), FlagOutput)
	if err != nil {
		return fmt.Errorf("getting output flag failed: %w", err)
	}
	displayMode, err := enum.Get(cmd.Flags(), FlagDisplayMode)
	if err != nil {
		return fmt.Errorf("getting display-mode flag failed: %w", err)
	}
	constraint, err := cmd.Flags().GetString(FlagSemverConstraint)
	if err != nil {
		return fmt.Errorf("getting semver-constraint flag failed: %w", err)
	}
	concurrencyLimit, err := cmd.Flags().GetInt(FlagConcurrencyLimit)
	if err != nil {
		return fmt.Errorf("getting concurrency-limit flag failed: %w", err)
	}
	latestOnly, err := cmd.Flags().GetBool(FlagLatest)
	if err != nil {
		return fmt.Errorf("getting latest flag failed: %w", err)
	}
	recursive, err := cmd.Flags().GetInt(FlagRecursive)
	if err != nil {
		return fmt.Errorf("getting recursive flag failed: %w", err)
	}

	reference := args[0]
	config := ocmctx.FromContext(cmd.Context()).Configuration()

	//nolint:staticcheck // no replacement for resolvers available yet https://github.com/open-component-model/ocm-project/issues/575
	var resolvers []resolverruntime.Resolver
	if config != nil {
		resolvers, err = resolversFromConfig(config, err)
		if err != nil {
			return fmt.Errorf("getting resolvers from configuration failed: %w", err)
		}
	}
	repo, err := ocm.NewFromRefWithFallbackRepo(cmd.Context(), pluginManager, credentialGraph, resolvers, reference)
	if err != nil {
		return fmt.Errorf("could not initialize ocm repository: %w", err)
	}

	descs, err := repo.GetComponentVersions(cmd.Context(), ocm.GetComponentVersionsOptions{
		VersionOptions: ocm.VersionOptions{
			SemverConstraint: constraint,
			LatestOnly:       latestOnly,
		},
		ConcurrencyLimit: concurrencyLimit,
	})
	if err != nil {
		return fmt.Errorf("getting component reference and versions failed: %w", err)
	}

	if err := renderComponents(cmd, repo, descs, output, displayMode, recursive); err != nil {
		return fmt.Errorf("failed to render components recursively: %w", err)
	}
	return nil
}

//nolint:staticcheck // no replacement for resolvers available yet (https://github.com/open-component-model/ocm-project/issues/575)
func resolversFromConfig(config *genericv1.Config, err error) ([]resolverruntime.Resolver, error) {
	filtered, err := genericv1.FilterForType[*resolverv1.Config](resolverv1.Scheme, config)
	if err != nil {
		return nil, fmt.Errorf("filtering configuration for resolver config failed: %w", err)
	}
	resolverConfigV1 := resolverv1.Merge(filtered...)

	resolverConfig, err := resolverruntime.ConvertFromV1(repository.Scheme, resolverConfigV1)
	if err != nil {
		return nil, fmt.Errorf("converting resolver configuration from v1 to runtime failed: %w", err)
	}
	var resolvers []resolverruntime.Resolver
	if resolverConfig != nil && len(resolverConfig.Resolvers) > 0 {
		resolvers = resolverConfig.Resolvers
	}
	return resolvers, nil
}

func renderComponents(cmd *cobra.Command, repo *ocm.ComponentRepository, descs []*descruntime.Descriptor, format string, mode string, recursive int) error {
	dag := syncdag.NewDirectedAcyclicGraph[string]()

	roots := make([]string, len(descs))
	for index, desc := range descs {
		root := desc.Component.ToIdentity().String()
		if err := dag.AddVertex(root, map[string]any{
			descriptorAttribute: desc,
		}); err != nil {
			return fmt.Errorf("adding root vertex %q failed: %w", root, err)
		}
		roots[index] = root
	}
	renderer, err := buildRenderer(cmd.Context(), dag, roots, format)
	if err != nil {
		return fmt.Errorf("building renderer failed: %w", err)
	}
	neighbourDiscoverer := buildNeighbourDiscoverer(dag, repo, recursive)

	switch mode {
	case render.StaticRenderMode:
		// Start traversing the graph from the root vertices (the initially resolved
		// component versions).
		err := dag.Discover(cmd.Context(), neighbourDiscoverer, syncdag.WithRoots(roots...))
		if err != nil {
			return fmt.Errorf("traversing component version graph failed: %w", err)
		}
		if err := render.RenderOnce(cmd.Context(), renderer, render.WithWriter(cmd.OutOrStdout())); err != nil {
			return err
		}
	case render.LiveRenderMode:
		// Start the render loop.
		renderCtx, cancel := context.WithCancel(cmd.Context())
		wait := render.RunRenderLoop(renderCtx, renderer, render.WithRenderOptions(render.WithWriter(cmd.OutOrStdout())))
		// Start traversing the graph from the root vertices (the initially resolved
		// component versions).
		// The render loop is running concurrently and regularly displays the current
		// state of the graph.
		err := dag.Discover(cmd.Context(), neighbourDiscoverer, syncdag.WithRoots(roots...))
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

const (
	descriptorAttribute = "descriptor"
)

func buildRenderer(ctx context.Context, dag *syncdag.DirectedAcyclicGraph[string], roots []string, format string) (render.Renderer, error) {
	// Initialize renderer based on the requested output format.
	switch format {
	case render.OutputFormatJSON.String(), render.OutputFormatNDJSON.String(), render.OutputFormatYAML.String():
		serializer := buildMachineFormatSerializer(format)
		return list.New(ctx, dag, list.WithListSerializer(serializer), list.WithRoots(roots...)), nil
	case render.OutputFormatTree.String():
		serializer := buildTreeFormatSerializer()
		return tree.New(ctx, dag, tree.WithVertexSerializer(serializer), tree.WithRoots(roots...)), nil
	case render.OutputFormatTable.String():
		serializer := buildTableFormatSerializer()
		return list.New(ctx, dag, list.WithListSerializer(serializer), list.WithRoots(roots...)), nil
	default:
		return nil, fmt.Errorf("invalid output format %q", format)
	}
}

func buildMachineFormatSerializer(format string) list.ListSerializer[string] {
	vertexSerializer := list.VertexSerializerFunc[string](func(vertex *syncdag.Vertex[string]) (any, error) {
		descriptor, _ := vertex.MustGetAttribute(descriptorAttribute).(*descruntime.Descriptor)
		descriptorV2, err := descruntime.ConvertToV2(descriptorv2.Scheme, descriptor)
		if err != nil {
			return nil, fmt.Errorf("converting descriptor to v2 failed: %w", err)
		}
		return descriptorV2, nil
	})

	switch format {
	case render.OutputFormatJSON.String():
		return list.NewSerializer(list.WithVertexSerializer(vertexSerializer), list.WithOutputFormat[string](render.OutputFormatJSON))
	case render.OutputFormatYAML.String():
		return list.NewSerializer(list.WithVertexSerializer(vertexSerializer), list.WithOutputFormat[string](render.OutputFormatYAML))
	case render.OutputFormatNDJSON.String():
		return list.NewSerializer(list.WithVertexSerializer(vertexSerializer), list.WithOutputFormat[string](render.OutputFormatNDJSON))
	default:
		panic(fmt.Errorf("invalid machine output format %q", format)) // should not happen as checked before
	}
}

func buildTreeFormatSerializer() tree.VertexSerializer[string] {
	return tree.VertexSerializerFunc[string](func(vertex *syncdag.Vertex[string]) (string, error) {
		id, _ := runtime.ParseIdentity(vertex.ID)
		return fmt.Sprintf("%s:%s", id[descruntime.IdentityAttributeName], id[descruntime.IdentityAttributeVersion]), nil
	})
}

func buildTableFormatSerializer() list.ListSerializer[string] {
	return list.ListSerializerFunc[string](func(writer io.Writer, vertices []*syncdag.Vertex[string]) error {
		t := table.NewWriter()
		t.SetOutputMirror(writer)
		t.AppendHeader(table.Row{"Component", "Version", "Provider"})
		for _, vertex := range vertices {
			descriptor, _ := vertex.MustGetAttribute(descriptorAttribute).(*descruntime.Descriptor)
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
	})
}

func buildNeighbourDiscoverer(dag *syncdag.DirectedAcyclicGraph[string], repo *ocm.ComponentRepository, recursive int) syncdag.DiscoverNeighborsFunc[string] {
	switch {
	case recursive != 0:
		return func(ctx context.Context, v string) ([]string, error) {
			var desc *descruntime.Descriptor
			var err error

			vertex := dag.MustGetVertex(v)
			id, _ := runtime.ParseIdentity(v)
			// root descriptors are already known
			if untypedDesc, ok := vertex.GetAttribute(descriptorAttribute); !ok {
				desc, err = repo.ComponentVersionRepository().GetComponentVersion(ctx, id[descruntime.IdentityAttributeName], id[descruntime.IdentityAttributeVersion])
				if err != nil {
					return nil, fmt.Errorf("getting component version for identity %q failed: %w", id, err)
				}
				vertex.Attributes.Store(descriptorAttribute, desc)
			} else {
				desc, _ = untypedDesc.(*descruntime.Descriptor)
			}
			// Store the component version descriptor with the vertex.
			// It will be used by the serializers to generate the output.
			neighbors := make([]string, len(desc.Component.References))
			for index, reference := range desc.Component.References {
				refID := make(runtime.Identity, 2)
				refID[descruntime.IdentityAttributeName] = reference.Component
				refID[descruntime.IdentityAttributeVersion] = reference.Version
				neighbors[index] = refID.String()
			}
			return neighbors, nil
		}
	default:
		return func(ctx context.Context, v string) (neighbors []string, err error) {
			return nil, nil
		}
	}
}
