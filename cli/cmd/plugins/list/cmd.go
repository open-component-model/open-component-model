package list

import (
	"context"
	"fmt"
	"io"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"

	"ocm.software/open-component-model/bindings/go/dag"
	"ocm.software/open-component-model/bindings/go/dag/sync"
	"ocm.software/open-component-model/cli/cmd/download/shared"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/enum"
	"ocm.software/open-component-model/cli/internal/reference/compref"
	"ocm.software/open-component-model/cli/internal/render"
	"ocm.software/open-component-model/cli/internal/render/graph/list"
	"ocm.software/open-component-model/cli/internal/repository/ocm"
)

const (
	FlagRegistry = "registry"
	FlagOutput   = "output"
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available plugin binaries from a registry.",
		Args:  cobra.ExactArgs(0),
		Long: ` # List available plugin binaries from a registry.
ocm plugin registry list --registry ghcr.io/open-component-model/plugin-registry:1.0.0`,
		Example: `  # List available plugin binaries from a registry.
  ocm plugin registry list --registry ocm.software/plugin-registry
`,

		RunE:              ListPlugins,
		DisableAutoGenTag: true,
	}

	enum.VarP(cmd.Flags(), FlagOutput, "o", []string{render.OutputFormatTable.String(), render.OutputFormatYAML.String(), render.OutputFormatJSON.String(), render.OutputFormatNDJSON.String(), render.OutputFormatTree.String()}, "output format of the plugin list")
	cmd.Flags().String(FlagRegistry, "", "registry URL to list plugins from")
	// TODO: Remove when https://github.com/open-component-model/ocm-project/issues/599 is implemented.
	_ = cmd.MarkFlagRequired(FlagRegistry)

	return cmd
}

func ListPlugins(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	pluginManager, credentialGraph, logger, err := shared.GetContextItems(cmd)
	if err != nil {
		return err
	}

	ocmContext := ocmctx.FromContext(ctx)
	if ocmContext == nil {
		return fmt.Errorf("no OCM context found")
	}

	config := ocmctx.FromContext(cmd.Context()).Configuration()

	output, err := enum.Get(cmd.Flags(), FlagOutput)
	if err != nil {
		return fmt.Errorf("getting output flag failed: %w", err)
	}

	var pluginRegistries []string

	pluginRegistryFlag, err := cmd.Flags().GetString(FlagRegistry)
	if err != nil {
		return fmt.Errorf("failed to get registryDesc flag: %w", err)
	}

	if pluginRegistryFlag != "" {
		pluginRegistries = append(pluginRegistries, pluginRegistryFlag)
	} else { //nolint:staticcheck // see TODOs below
		// TODO: Load registries from config
		// see https://github.com/open-component-model/ocm-project/issues/599

		// TODO: Set default registry if no registry is provided
		// see https://github.com/open-component-model/ocm-project/issues/598
	}

	// TODO: Remove after testing (Added additional hardcoded registry for testing)
	pluginRegistries = append(pluginRegistries, "ghcr.io/frewilhelm//ocm.software/test-plugin-registry-two:v1.0.0")

	// The config can contain several registries from which we want to list plugins
	// e.g. registries:
	//   - ghcr.io/open-component-model/plugin-registry:1.0.0
	//   - example.com/ocm/plugin-registry:2.0.0
	// Each registry (component) can contain several plugins (component references)

	// The DAG is used to represent the plugins and their versions in a pretty way
	// TODO: Discuss/Find out if the same is possible using dag.Discover (First attempts failed because of different
	//       types in the graph)
	graph := dag.NewDirectedAcyclicGraph[string]()
	// TODO: Remove "order" when we find a better way to sort the graph for an ordered output.
	var order []string
	for _, reg := range pluginRegistries {
		logger.Debug("Getting plugin registry", "registry", reg)

		ref, err := compref.Parse(reg)
		if err != nil {
			return fmt.Errorf("creating component reference for plugin registry %q failed: %w", reg, err)
		}

		repoProvider, err := ocm.NewComponentVersionRepositoryForComponentProvider(ctx, pluginManager.ComponentVersionRepositoryRegistry, credentialGraph, config, ref)
		if err != nil {
			return fmt.Errorf("could not initialize ocm repositoryProvider: %w", err)
		}

		repo, err := repoProvider.GetComponentVersionRepositoryForComponent(ctx, ref.Component, ref.Version)
		if err != nil {
			return fmt.Errorf("could not access ocm repository: %w", err)
		}

		descs, err := ocm.GetComponentVersions(ctx, ocm.GetComponentVersionsOptions{}, ref.Component, ref.Version, repo)
		if err != nil {
			return fmt.Errorf("getting component reference and versions failed: %w", err)
		}

		for _, d := range descs {
			for _, r := range d.Component.References {
				logger.Debug("Adding plugin to graph", "plugin", r.Name, "version", r.Version, "registry", reg)

				var desc string
				for _, l := range r.Labels {
					if l.Name == "description" {
						// TODO: check out
						desc = string(l.Value)
					}
				}
				if err := graph.AddVertex(r.Name, map[string]any{
					"version":     r.Version,
					"registry":    reg,
					"description": desc,
				}); err != nil {
					return fmt.Errorf("adding vertex to DAG failed: %w", err)
				}
				order = append(order, r.Name)
			}
		}
	}

	renderer, err := buildRenderer(ctx, sync.ToSyncedGraph(graph), order, output)
	if err != nil {
		return fmt.Errorf("building renderer failed: %w", err)
	}

	return render.RenderOnce(cmd.Context(), renderer, render.WithWriter(cmd.OutOrStdout()))
}

func buildRenderer(ctx context.Context, graph *sync.SyncedDirectedAcyclicGraph[string], roots []string, format string) (render.Renderer, error) {
	// Initialize renderer based on the requested output format.
	switch format {
	case render.OutputFormatJSON.String(), render.OutputFormatNDJSON.String(), render.OutputFormatYAML.String():
		return nil, fmt.Errorf("(currently) unsupported output format: %s", format)
	case render.OutputFormatTable.String():
		serializer := list.ListSerializerFunc[string](serializeVerticesToTable)
		return list.New(ctx, graph, list.WithListSerializer(serializer), list.WithRoots(roots...)), nil
	default:
		return nil, fmt.Errorf("invalid output format %q", format)
	}
}

func serializeVerticesToTable(writer io.Writer, vertices []*dag.Vertex[string]) error {
	t := table.NewWriter()
	t.SetOutputMirror(writer)
	t.AppendHeader(table.Row{"Name", "Version", "Description", "Registry"})
	for _, vertex := range vertices {
		t.AppendRow(table.Row{vertex.ID, vertex.Attributes["version"], vertex.Attributes["description"], vertex.Attributes["registry"]})
	}

	t.SetColumnConfigs([]table.ColumnConfig{
		{Number: 4, AutoMerge: true},
	})

	style := table.StyleLight
	style.Options.DrawBorder = false
	t.SetStyle(style)
	t.Render()
	return nil
}
