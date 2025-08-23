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

	enum.VarP(cmd.Flags(), FlagOutput, "o", []string{"table", "yaml", "json", "tree"}, "output format of the component descriptors")
	cmd.Flags().String(FlagSemverConstraint, "> 0.0.0-0", "semantic version constraint restricting which versions to output")
	cmd.Flags().Int(FlagConcurrencyLimit, 4, "maximum amount of parallel requests to the repository for resolving component versions")
	cmd.Flags().Bool(FlagLatest, false, "if set, only the latest version of the component is returned")
	cmd.Flags().Int(FlagRecursive, 0, "depth of recursion for resolving referenced component versions (0=none, -1=unlimited, >0=levels (not implemented yet))\"")

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
	resolvers, err := resolversFromConfig(config, err)
	if err != nil {
		return fmt.Errorf("getting resolvers from configuration failed: %w", err)
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

	if recursive >= 0 {
		if err := renderComponentsRecursive(cmd, repo, descs, output); err != nil {
			return fmt.Errorf("failed to render components recursively: %w", err)
		}
		return nil
	}
	if err = renderComponents(cmd, descs, output); err != nil {
		return fmt.Errorf("failed to render components: %w", err)
	}
	return nil
}

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
	resolvers := resolverConfig.Resolvers
	return resolvers, nil
}

func renderComponents(cmd *cobra.Command, descs []*descruntime.Descriptor, format string) error {
	reader, size, err := encodeDescriptors(format, descs)
	if err != nil {
		return fmt.Errorf("generating format failed: %w", err)
	}

	if _, err := io.CopyN(cmd.OutOrStdout(), reader, size); err != nil {
		return fmt.Errorf("writing component version descriptor failed: %w", err)
	}
	return nil
}

func renderComponentsRecursive(cmd *cobra.Command, repo *ocm.ComponentRepository, descs []*descruntime.Descriptor, format string) error {
	dag := syncdag.NewDirectedAcyclicGraph[string]()

	var renderer render.Renderer
	const identityAttribute = "identity"
	const descriptorAttribute = "descriptor"

	// Prepare serializers for output formats.
	// Some output formats share the same serializer.
	descriptorVertexSerializer := list.VertexSerializerFunc[string](func(vertex *syncdag.Vertex[string]) (any, error) {
		descriptor, _ := vertex.MustGetAttribute(descriptorAttribute).(*descruntime.Descriptor)
		descriptorV2, err := descruntime.ConvertToV2(descriptorv2.Scheme, descriptor)
		if err != nil {
			return nil, fmt.Errorf("converting descriptor to v2 failed: %w", err)
		}
		return descriptorV2, nil
	})
	treeVertexSerializer := tree.VertexSerializerFunc[string](func(vertex *syncdag.Vertex[string]) (string, error) {
		id, _ := vertex.MustGetAttribute(identityAttribute).(runtime.Identity)
		return fmt.Sprintf("%s:%s", id[descruntime.IdentityAttributeName], id[descruntime.IdentityAttributeVersion]), nil
	})
	tableListSerializer := list.ListSerializerFunc[string](func(writer io.Writer, vertices []*syncdag.Vertex[string]) error {
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

	// Initialize renderer based on the requested output format.
	switch format {
	case render.OutputFormatJSON.String():
		serializer := list.NewSerializer(list.WithVertexSerializer(descriptorVertexSerializer), list.WithOutputFormat[string](render.OutputFormatJSON))
		renderer = list.New(cmd.Context(), dag, list.WithListSerializer(serializer))
	case render.OutputFormatYAML.String():
		serializer := list.NewSerializer(list.WithVertexSerializer(descriptorVertexSerializer), list.WithOutputFormat[string](render.OutputFormatYAML))
		renderer = list.New(cmd.Context(), dag, list.WithListSerializer(serializer))
	case render.OutputFormatNDJSON.String():
		serializer := list.NewSerializer(list.WithVertexSerializer(descriptorVertexSerializer), list.WithOutputFormat[string](render.OutputFormatNDJSON))
		renderer = list.New(cmd.Context(), dag, list.WithListSerializer(serializer))
	case render.OutputFormatTree.String():
		serializer := treeVertexSerializer
		renderer = tree.New(cmd.Context(), dag, tree.WithVertexSerializer(serializer))
	case render.OutputFormatTable.String():
		serializer := tableListSerializer
		renderer = list.New(cmd.Context(), dag, list.WithListSerializer(serializer))
	default:
		return fmt.Errorf("invalid format format %q", format)
	}

	// Start the render loop.
	renderCtx, cancel := context.WithCancel(cmd.Context())
	wait := render.RunRenderLoop(renderCtx, renderer)

	// Prepare function to discover neighbors (referenced component versions) of
	// a vertex (component version).
	discoverNeighborsFunc := func(ctx context.Context, v *syncdag.Vertex[string]) (neighbors []*syncdag.Vertex[string], err error) {
		id, _ := v.MustGetAttribute(identityAttribute).(runtime.Identity)
		desc, err := repo.ComponentVersionRepository().GetComponentVersion(ctx, id[descruntime.IdentityAttributeName], id[descruntime.IdentityAttributeVersion])
		if err != nil {
			return nil, fmt.Errorf("getting component version for identity %q failed: %w", id, err)
		}
		// Store the component version descriptor with the vertex.
		// It will be used by the serializers to generate the output.
		v.Attributes.Store(descriptorAttribute, desc)

		for _, reference := range desc.Component.References {
			refID := make(runtime.Identity, 2)
			refID[descruntime.IdentityAttributeName] = reference.Component
			refID[descruntime.IdentityAttributeVersion] = reference.Version
			// Create a new vertex for the referenced component version
			neighbor := syncdag.NewVertex(refID.String(), map[string]any{
				identityAttribute: refID,
			})
			neighbors = append(neighbors, neighbor)
		}
		return neighbors, nil
	}

	roots := make([]*syncdag.Vertex[string], 0, len(descs))
	for _, desc := range descs {
		roots = append(roots, syncdag.NewVertex(desc.Component.ToIdentity().String(), map[string]any{
			identityAttribute: desc.Component.ToIdentity(),
		}))
	}

	// Start traversing the graph from the root vertices (the initially resolved
	// component versions).
	// The render loop is running concurrently and regularly displays the current
	// state of the graph.
	err := dag.Traverse(cmd.Context(), syncdag.DiscoverNeighborsFunc[string](discoverNeighborsFunc), syncdag.WithRoots(roots...))
	cancel()
	if err != nil {
		return fmt.Errorf("traversing component version graph failed: %w", err)
	}

	if err := wait(); !errors.Is(err, context.Canceled) {
		return fmt.Errorf("rendering component version graph failed: %w", err)
	}
	return nil
}
