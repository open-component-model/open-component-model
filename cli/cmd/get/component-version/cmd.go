package componentversion

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"

	"ocm.software/open-component-model/bindings/go/dag"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
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
	// TODO(fabianburth): add concurrency limit to the dag discovery (https://github.com/open-component-model/ocm-project/issues/705)
	// cmd.Flags().Int(FlagConcurrencyLimit, 4, "maximum amount of parallel requests to the repository for resolving component versions")
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
	// TODO(fabianburth): add concurrency limit to the dag discovery (https://github.com/open-component-model/ocm-project/issues/705)
	// concurrencyLimit, err := cmd.Flags().GetInt(FlagConcurrencyLimit)
	// if err != nil {
	//	 return fmt.Errorf("getting concurrency-limit flag failed: %w", err)
	// }
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

	// we have a reference and parse it
	ref, err := compref.Parse(reference)
	if err != nil {
		return fmt.Errorf("parsing component reference %q failed: %w", reference, err)
	}
	slog.DebugContext(cmd.Context(), "parsed component reference", "reference", reference, "parsed", ref)

	repoProvider, err := ocm.NewComponentVersionRepositoryProvider(cmd.Context(), pluginManager, credentialGraph, config, ref)
	if err != nil {
		return fmt.Errorf("could not initialize ocm repositoryProvider: %w", err)
	}

	repo, err := repoProvider.GetComponentVersionRepository(cmd.Context(), ref.Identity())
	if err != nil {
		return fmt.Errorf("could not access ocm repository: %w", err)
	}

	descs, err := ocm.GetComponentVersions(cmd.Context(), ocm.GetComponentVersionsOptions{
		VersionOptions: ocm.VersionOptions{
			SemverConstraint: constraint,
			LatestOnly:       latestOnly,
		},
	}, ref.Component, ref.Version, repo)
	if err != nil {
		return fmt.Errorf("getting component reference and versions failed: %w", err)
	}
	roots := make([]string, 0, len(descs))
	for _, desc := range descs {
		identity := runtime.Identity{
			descruntime.IdentityAttributeName:    desc.Component.Name,
			descruntime.IdentityAttributeVersion: desc.Component.Version,
		}.String()
		roots = append(roots, identity)
	}

	if err := renderComponents(cmd, repoProvider, roots, output, displayMode, recursive); err != nil {
		return fmt.Errorf("failed to render components recursively: %w", err)
	}
	return nil
}

func renderComponents(cmd *cobra.Command, repoProvider ocm.ComponentVersionRepositoryProvider, roots []string, format string, mode string, recursive int) error {
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
	case render.StaticRenderMode:
		// Start traversing the graph from the root vertices (the initially resolved
		// component versions).
		err := discoverer.Discover(cmd.Context())
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

func buildRenderer(ctx context.Context, dag *syncdag.SyncedDirectedAcyclicGraph[string], roots []string, format string) (render.Renderer, error) {
	// Initialize renderer based on the requested output format.
	switch format {
	case render.OutputFormatJSON.String():
		serializer := list.NewSerializer(list.WithVertexSerializer(list.VertexSerializerFunc[string](serializeVertexToDescriptor)), list.WithOutputFormat[string](render.OutputFormatJSON))
		return list.New(ctx, dag, list.WithListSerializer(serializer), list.WithRoots(roots...)), nil
	case render.OutputFormatNDJSON.String():
		serializer := list.NewSerializer(list.WithVertexSerializer(list.VertexSerializerFunc[string](serializeVertexToDescriptor)), list.WithOutputFormat[string](render.OutputFormatNDJSON))
		return list.New(ctx, dag, list.WithListSerializer(serializer), list.WithRoots(roots...)), nil
	case render.OutputFormatYAML.String():
		serializer := list.NewSerializer(list.WithVertexSerializer(list.VertexSerializerFunc[string](serializeVertexToDescriptor)), list.WithOutputFormat[string](render.OutputFormatYAML))
		return list.New(ctx, dag, list.WithListSerializer(serializer), list.WithRoots(roots...)), nil
	case render.OutputFormatTree.String():
		return tree.New(ctx, dag, tree.WithRoots(roots...)), nil
	case render.OutputFormatTable.String():
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
	repositoryProvider ocm.ComponentVersionRepositoryProvider
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
	repo, err := r.repositoryProvider.GetComponentVersionRepository(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("getting component version repository for identity %q failed: %w", id, err)
	}
	desc, err := repo.GetComponentVersion(ctx, id[descruntime.IdentityAttributeName], id[descruntime.IdentityAttributeVersion])
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
