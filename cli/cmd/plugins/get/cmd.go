package get

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"ocm.software/open-component-model/bindings/go/dag"
	"ocm.software/open-component-model/bindings/go/dag/sync"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/repository/component/provider"
	"ocm.software/open-component-model/cli/cmd/download/shared"
	"ocm.software/open-component-model/cli/cmd/plugins/list"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/enum"
	"ocm.software/open-component-model/cli/internal/reference/compref"
	"ocm.software/open-component-model/cli/internal/render"
	daglist "ocm.software/open-component-model/cli/internal/render/graph/list"
	"ocm.software/open-component-model/cli/internal/repository/ocm"
)

const (
	FlagRegistry            = "registry"
	FlagOutput              = "output"
	FlagVersion             = "version"
	FlagComponentDescriptor = "component-descriptor"
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <plugin-name[:version]>",
		Short: "Get information about specified plugin from a plugin registry.",
		Args:  cobra.ExactArgs(1),
		Long:  ``,
		Example: `  # Get information about specified plugin from a plugin registry.
  ocm plugin registry get helminput`,
		RunE:              GetPlugin,
		DisableAutoGenTag: true,
	}

	enum.VarP(cmd.Flags(), FlagOutput, "o", []string{render.OutputFormatTable.String(), render.OutputFormatYAML.String(), render.OutputFormatJSON.String(), render.OutputFormatNDJSON.String()}, "output format of the plugin list")
	cmd.Flags().String(FlagVersion, "", "specific version of the plugin to display (default: latest version)")
	cmd.Flags().Bool(FlagComponentDescriptor, false, "return component descriptors of the plugins")
	cmd.Flags().String(FlagRegistry, "", "registry URL to list plugins from")
	// TODO: Remove when https://github.com/open-component-model/ocm-project/issues/599 is implemented.
	_ = cmd.MarkFlagRequired(FlagRegistry)

	return cmd
}

func GetPlugin(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	pluginManager, credentialGraph, logger, err := shared.GetContextItems(cmd)
	if err != nil {
		return err
	}
	ocmContext := ocmctx.FromContext(ctx)
	if ocmContext == nil {
		return fmt.Errorf("no OCM context found")
	}

	config := ocmctx.FromContext(ctx).Configuration()

	pluginRegistryFlag, err := cmd.Flags().GetString(FlagRegistry)
	if err != nil {
		return fmt.Errorf("failed to get registry flag: %w", err)
	}

	pluginArgVersion, err := cmd.Flags().GetString(FlagVersion)
	if err != nil {
		return fmt.Errorf("failed to get version flag: %w", err)
	}

	output, err := enum.Get(cmd.Flags(), FlagOutput)
	if err != nil {
		return fmt.Errorf("getting output flag failed: %w", err)
	}

	cd, err := cmd.Flags().GetBool(FlagComponentDescriptor)
	if err != nil {
		return fmt.Errorf("getting component-descriptor flag failed: %w", err)
	}

	pluginArgName := args[0]
	if strings.Contains(pluginArgName, ":") {
		parts := strings.SplitN(pluginArgName, ":", 2)
		pluginArgName = parts[0]
		pluginArgVersion = parts[1]
	}

	var pluginRegistries []string
	if pluginRegistryFlag != "" {
		regs := strings.Split(pluginRegistryFlag, ",")
		pluginRegistries = append(pluginRegistries, regs...)
	} else { //nolint:staticcheck // see TODOs below
		// TODO: Load registries from config
		// see https://github.com/open-component-model/ocm-project/issues/599

		// TODO: Set default registry if no registry is provided
		// see https://github.com/open-component-model/ocm-project/issues/598
	}

	// Get plugin registry descriptor to look for a component references of the passed plugin
	var plugins []list.PluginInfo
	// Keep repository providers that contain the requested plugin
	var repoProviders []provider.ComponentVersionRepositoryForComponentProvider
	for _, reg := range pluginRegistries {
		logger.Debug("Getting plugin registry descriptor", "registry", reg)

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
			return fmt.Errorf("failed getting repository: %w", err)
		}

		var desc *descruntime.Descriptor

		// Get latest version of plugin registry if no version is specified
		if ref.Version == "" {
			descs, err := ocm.GetComponentVersions(ctx, ocm.GetComponentVersionsOptions{VersionOptions: ocm.VersionOptions{LatestOnly: true}}, ref.Component, ref.Version, repo)
			if err != nil {
				return fmt.Errorf("failed getting component versions for plugin registry: %w", err)
			}

			if len(descs) == 0 {
				return fmt.Errorf("no versions found for component %q in plugin registry", ref.Component)
			}

			desc = descs[0]

			// Add version to registry ref to be able to identify the source later
			reg = fmt.Sprintf("%s:%s", reg, desc.Component.Version)
		} else {
			desc, err = repo.GetComponentVersion(ctx, ref.Component, ref.Version)
			if err != nil {
				return fmt.Errorf("failed getting component constructor for plugin registry: %w", err)
			}
		}

		logger.Debug("Looking for plugin in component version", slog.String("component", desc.Component.String()))
		for _, r := range desc.Component.References {
			logger.Debug("Checking component reference", slog.String("name", r.Name))
			// Check if component reference matches requested plugin
			if r.Name != pluginArgName {
				continue
			}

			var info list.PluginInfo
			for _, l := range r.Labels {
				if l.Name != "ocm.software/pluginInfo" {
					continue
				}

				// If version is specified, check if it matches. Otherwise return all versions
				if pluginArgVersion != "" && r.Version != pluginArgVersion {
					continue
				}

				slog.Debug("Found plugin in registry", "name", r.Name, "component", r.Component)
				dec := json.NewDecoder(strings.NewReader(string(l.Value)))
				dec.DisallowUnknownFields()
				if err := dec.Decode(&info); err != nil {
					return fmt.Errorf("decoding plugin info label failed: %w", err)
				}

				info.Name = r.Name
				info.Version = r.Version
				info.Registry = reg
				info.Component = r.Component

				// If we reach here the plugin was found and info was extracted
				plugins = append(plugins, info)

				if !slices.Contains(repoProviders, repoProvider) {
					repoProviders = append(repoProviders, repoProvider)
				}
				break
			}
		}
	}

	if len(plugins) == 0 {
		return fmt.Errorf("plugin %q not found in specified registries: %q", pluginArgName, strings.Join(pluginRegistries, ", "))
	}

	// Create DAG and render output
	// Depending on the --component-descriptor flag either render plugin info or component descriptors
	graph, roots, err := createDAG(ctx, plugins, repoProviders, cd, output)
	if err != nil {
		return fmt.Errorf("creating DAG failed: %w", err)
	}

	renderer, err := buildRenderer(ctx, sync.ToSyncedGraph(graph), roots, output, cd)
	if err != nil {
		return fmt.Errorf("building renderer failed: %w", err)
	}

	return render.RenderOnce(cmd.Context(), renderer, render.WithWriter(cmd.OutOrStdout()))
}

func createDAG(ctx context.Context, plugins []list.PluginInfo, providers []provider.ComponentVersionRepositoryForComponentProvider, cd bool, output string) (*dag.DirectedAcyclicGraph[string], []string, error) {
	graph := dag.NewDirectedAcyclicGraph[string]()
	var roots []string
	for _, plugin := range plugins {
		if cd && output != render.OutputFormatTable.String() {
			for _, repoProvider := range providers {
				repo, err := repoProvider.GetComponentVersionRepositoryForComponent(ctx, plugin.Component, plugin.Version)
				if err != nil {
					return nil, nil, fmt.Errorf("cannot get repository provider: %w", err)
				}

				desc, err := repo.GetComponentVersion(ctx, plugin.Component, plugin.Version)
				if err != nil {
					return nil, nil, fmt.Errorf("getting component descriptor for plugin failed: %w", err)
				}

				if err := graph.AddVertex(plugin.String(), map[string]any{
					sync.AttributeValue: desc,
				}); err != nil {
					return nil, nil, fmt.Errorf("adding vertex to graph failed: %w", err)
				}
				roots = append(roots, plugin.String())
			}
		} else {
			if err := graph.AddVertex(
				plugin.String(),
				map[string]any{
					"name":        plugin.Name,
					"version":     plugin.Version,
					"registry":    plugin.Registry,
					"description": plugin.Description,
					"platforms":   plugin.Platforms,
				}); err != nil {
				return nil, nil, fmt.Errorf("adding vertex to graph failed: %w", err)
			}
			roots = append(roots, plugin.String())
		}
	}

	return graph, roots, nil
}

func buildRenderer(ctx context.Context, graph *sync.SyncedDirectedAcyclicGraph[string], roots []string, format string, desc bool) (render.Renderer, error) {
	// Depending on the --component-descriptor flag either render plugin info or component descriptors
	if desc && format != render.OutputFormatTable.String() {
		switch format {
		case render.OutputFormatJSON.String():
			serializer := daglist.NewSerializer(daglist.WithVertexSerializer(daglist.VertexSerializerFunc[string](serializeVertexToDescriptor)), daglist.WithOutputFormat[string](render.OutputFormatJSON))
			return daglist.New(ctx, graph, daglist.WithListSerializer(serializer), daglist.WithRoots(roots...)), nil
		case render.OutputFormatNDJSON.String():
			serializer := daglist.NewSerializer(daglist.WithVertexSerializer(daglist.VertexSerializerFunc[string](serializeVertexToDescriptor)), daglist.WithOutputFormat[string](render.OutputFormatNDJSON))
			return daglist.New(ctx, graph, daglist.WithListSerializer(serializer), daglist.WithRoots(roots...)), nil
		case render.OutputFormatYAML.String():
			serializer := daglist.NewSerializer(daglist.WithVertexSerializer(daglist.VertexSerializerFunc[string](serializeVertexToDescriptor)), daglist.WithOutputFormat[string](render.OutputFormatYAML))
			return daglist.New(ctx, graph, daglist.WithListSerializer(serializer), daglist.WithRoots(roots...)), nil
		default:
			return nil, fmt.Errorf("invalid output format %q", format)
		}
	}

	switch format {
	case render.OutputFormatJSON.String():
		serializer := daglist.ListSerializerFunc[string](list.SerializeVerticesToJSON)
		return daglist.New(ctx, graph, daglist.WithListSerializer(serializer), daglist.WithRoots(roots...)), nil
	case render.OutputFormatNDJSON.String():
		serializer := daglist.ListSerializerFunc[string](list.SerializeVerticesToNDJSON)
		return daglist.New(ctx, graph, daglist.WithListSerializer(serializer), daglist.WithRoots(roots...)), nil
	case render.OutputFormatYAML.String():
		serializer := daglist.ListSerializerFunc[string](list.SerializeVerticesToYAML)
		return daglist.New(ctx, graph, daglist.WithListSerializer(serializer), daglist.WithRoots(roots...)), nil
	case render.OutputFormatTable.String():
		serializer := daglist.ListSerializerFunc[string](list.SerializeVerticesToTable)
		return daglist.New(ctx, graph, daglist.WithListSerializer(serializer), daglist.WithRoots(roots...)), nil
	default:
		return nil, fmt.Errorf("invalid output format %q", format)
	}
}

func serializeVertexToDescriptor(vertex *dag.Vertex[string]) (any, error) {
	untypedDescriptor, ok := vertex.Attributes[sync.AttributeValue]
	if !ok {
		return nil, fmt.Errorf("vertex %s has no %s attribute", vertex.ID, sync.AttributeValue)
	}
	desc, ok := untypedDescriptor.(*descruntime.Descriptor)
	if !ok {
		return nil, fmt.Errorf("expected vertex %s attribute %s to be of type %T, got type %T", vertex.ID, sync.AttributeValue, &descruntime.Descriptor{}, untypedDescriptor)
	}
	descriptorV2, err := descruntime.ConvertToV2(descriptorv2.Scheme, desc)
	if err != nil {
		return nil, fmt.Errorf("converting descriptor to v2 failed: %w", err)
	}
	return descriptorV2, nil
}
