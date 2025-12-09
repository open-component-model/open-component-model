package list

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"ocm.software/open-component-model/bindings/go/dag"
	"ocm.software/open-component-model/bindings/go/dag/sync"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
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

	PluginInfoKey = "ocm.software/pluginInfo"
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available plugin binaries from a plugin registry.",
		Args:  cobra.ExactArgs(0),
		Long:  ``,
		Example: `  # List available plugin binaries from a registry using the flag.
  ocm plugin registry list --registry ocm.software/plugin-registry:v1.0.0
`,
		RunE:              ListPlugins,
		DisableAutoGenTag: true,
	}

	enum.VarP(cmd.Flags(), FlagOutput, "o", []string{render.OutputFormatTable.String(), render.OutputFormatYAML.String(), render.OutputFormatJSON.String(), render.OutputFormatNDJSON.String(), "wide"}, "output format of the plugin list")
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

	config := ocmctx.FromContext(ctx).Configuration()

	output, err := enum.Get(cmd.Flags(), FlagOutput)
	if err != nil {
		return fmt.Errorf("getting output flag failed: %w", err)
	}

	pluginRegistryFlag, err := cmd.Flags().GetString(FlagRegistry)
	if err != nil {
		return fmt.Errorf("failed to get plugin registry flag: %w", err)
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

	// The config can contain several registries from which we want to list plugins
	// e.g. registries:
	//   - ghcr.io/open-component-model/plugin-registry:1.0.0
	//   - example.com/ocm/plugin-registry:2.0.0
	// Each registry (component) can contain several plugins (component references)
	graph := dag.NewDirectedAcyclicGraph[string]()
	var roots []string
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
			return fmt.Errorf("failed getting repository: %w", err)
		}

		var desc *descriptor.Descriptor

		// Get latest version of plugin registry if no version is specified
		if ref.Version == "" {
			descs, err := ocm.GetComponentVersions(ctx, ocm.GetComponentVersionsOptions{
				VersionOptions: ocm.VersionOptions{
					LatestOnly: true,
				},
			}, ref.Component, ref.Version, repo)
			if err != nil {
				return fmt.Errorf("failed getting component versions for plugin registry: %w", err)
			}

			if len(descs) == 0 {
				return fmt.Errorf("no versions found for component %q in plugin registry", ref.Component)
			}

			desc = descs[0]

			// Add version to registry ref to be able to identify the source later
			reg = fmt.Sprintf("%s:%s", reg, descs[0].Component.Version)
		} else {
			desc, err = repo.GetComponentVersion(ctx, ref.Component, ref.Version)
			if err != nil {
				return fmt.Errorf("failed getting component constructor for plugin registry: %w", err)
			}
		}

		for _, r := range desc.Component.References {
			logger.Debug("Adding plugin to graph", "plugin", r.Name, "version", r.Version, "registry", reg)

			var info PluginInfo
			for _, l := range r.Labels {
				if l.Name == PluginInfoKey {
					// l.Value is JSON representing a *string* which itself is JSON
					var raw string
					if err := json.Unmarshal(l.Value, &raw); err != nil {
						return fmt.Errorf("decoding plugin info label (outer) failed: %w", err)
					}

					dec := json.NewDecoder(strings.NewReader(raw))
					dec.DisallowUnknownFields()
					if err := dec.Decode(&info); err != nil {
						return fmt.Errorf("decoding plugin info label failed: %w", err)
					}

					info.Name = r.Name
					info.Version = r.Version
					info.Registry = reg
					break
				}
			}

			if err := graph.AddVertex(
				info.String(),
				map[string]any{
					"name":        r.Name,
					"version":     r.Version,
					"registry":    reg,
					"description": info.Description,
					"platforms":   info.Platforms,
				}); err != nil {
				return fmt.Errorf("adding plugin vertex to graph failed: %w", err)
			}
			roots = append(roots, info.String())
		}
	}

	renderer, err := buildRenderer(ctx, sync.ToSyncedGraph(graph), roots, output)
	if err != nil {
		return fmt.Errorf("building renderer failed: %w", err)
	}

	return render.RenderOnce(cmd.Context(), renderer, render.WithWriter(cmd.OutOrStdout()))
}

func buildRenderer(ctx context.Context, graph *sync.SyncedDirectedAcyclicGraph[string], roots []string, format string) (render.Renderer, error) {
	switch format {
	case render.OutputFormatJSON.String():
		serializer := list.ListSerializerFunc[string](SerializeVerticesToJSON)
		return list.New(ctx, graph, list.WithListSerializer(serializer), list.WithRoots(roots...)), nil
	case render.OutputFormatNDJSON.String():
		serializer := list.ListSerializerFunc[string](SerializeVerticesToNDJSON)
		return list.New(ctx, graph, list.WithListSerializer(serializer), list.WithRoots(roots...)), nil
	case render.OutputFormatYAML.String():
		serializer := list.ListSerializerFunc[string](SerializeVerticesToYAML)
		return list.New(ctx, graph, list.WithListSerializer(serializer), list.WithRoots(roots...)), nil
	case render.OutputFormatTable.String():
		serializer := list.ListSerializerFunc[string](SerializeVerticesToTable)
		return list.New(ctx, graph, list.WithListSerializer(serializer), list.WithRoots(roots...)), nil
	case "wide":
		serializer := list.ListSerializerFunc[string](SerializeVerticesToTableWide)
		return list.New(ctx, graph, list.WithListSerializer(serializer), list.WithRoots(roots...)), nil
	default:
		return nil, fmt.Errorf("invalid output format %q", format)
	}
}

type PluginInfo struct {
	Name        string   `json:"name"        yaml:"name"`
	Version     string   `json:"version"     yaml:"version"`
	Platforms   []string `json:"platforms"   yaml:"platforms"`
	Description string   `json:"description" yaml:"description"`
	Registry    string   `json:"registry"    yaml:"registry"`
	Component   string   `json:"component"   yaml:"component"`
}

func (p *PluginInfo) String() string {
	return fmt.Sprintf("%s/%s:%s-%s", p.Registry, p.Name, p.Version, p.Platforms)
}

func convertVerticesToPluginInfos(vertices []*dag.Vertex[string]) []PluginInfo {
	// Sort vertices by name, then version, then registry
	sort.Slice(vertices, func(i, j int) bool {
		nameI := vertices[i].Attributes["name"].(string)
		nameJ := vertices[j].Attributes["name"].(string)
		if nameI != nameJ {
			return nameI < nameJ
		}

		versionI := vertices[i].Attributes["version"].(string)
		versionJ := vertices[j].Attributes["version"].(string)
		if versionI != versionJ {
			return versionI < versionJ
		}

		registryI := vertices[i].Attributes["registry"].(string)
		registryJ := vertices[j].Attributes["registry"].(string)
		return registryI < registryJ
	})

	plugins := make([]PluginInfo, 0, len(vertices))
	for _, vertex := range vertices {
		plugins = append(plugins, PluginInfo{
			Name:        vertex.Attributes["name"].(string),
			Version:     vertex.Attributes["version"].(string),
			Platforms:   vertex.Attributes["platforms"].([]string),
			Description: vertex.Attributes["description"].(string),
			Registry:    vertex.Attributes["registry"].(string),
		})
	}
	return plugins
}

func SerializeVerticesToJSON(writer io.Writer, vertices []*dag.Vertex[string]) error {
	plugins := convertVerticesToPluginInfos(vertices)
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(plugins)
}

func SerializeVerticesToNDJSON(writer io.Writer, vertices []*dag.Vertex[string]) error {
	plugins := convertVerticesToPluginInfos(vertices)
	encoder := json.NewEncoder(writer)
	for _, plugin := range plugins {
		if err := encoder.Encode(plugin); err != nil {
			return err
		}
	}
	return nil
}

func SerializeVerticesToYAML(writer io.Writer, vertices []*dag.Vertex[string]) error {
	plugins := convertVerticesToPluginInfos(vertices)
	encoder := yaml.NewEncoder(writer)
	defer encoder.Close()
	return encoder.Encode(plugins)
}

func SerializeVerticesToTable(writer io.Writer, vertices []*dag.Vertex[string]) error {
	// Sort vertices by name, then version, then registry
	sort.Slice(vertices, func(i, j int) bool {
		nameI := vertices[i].Attributes["name"].(string)
		nameJ := vertices[j].Attributes["name"].(string)
		if nameI != nameJ {
			return nameI < nameJ
		}

		versionI := vertices[i].Attributes["version"].(string)
		versionJ := vertices[j].Attributes["version"].(string)
		return versionI < versionJ
	})
	t := table.NewWriter()
	t.SetOutputMirror(writer)
	t.AppendHeader(table.Row{"Name", "Version", "Platforms", "Description"})
	for _, vertex := range vertices {
		t.AppendRow(table.Row{
			vertex.Attributes["name"],
			vertex.Attributes["version"],
			strings.Join(vertex.Attributes["platforms"].([]string), ", "),
			vertex.Attributes["description"],
		})
	}

	t.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1, AutoMerge: true},
		{Number: 2, AutoMerge: true},
		{Number: 4, AutoMerge: true},
	})

	style := table.StyleLight
	style.Options.DrawBorder = false
	t.SetStyle(style)
	t.Render()
	return nil
}

func SerializeVerticesToTableWide(writer io.Writer, vertices []*dag.Vertex[string]) error {
	// Sort vertices by name, then version, then registry
	sort.Slice(vertices, func(i, j int) bool {
		nameI := vertices[i].Attributes["name"].(string)
		nameJ := vertices[j].Attributes["name"].(string)
		if nameI != nameJ {
			return nameI < nameJ
		}

		versionI := vertices[i].Attributes["version"].(string)
		versionJ := vertices[j].Attributes["version"].(string)
		if versionI != versionJ {
			return versionI < versionJ
		}

		registryI := vertices[i].Attributes["registry"].(string)
		registryJ := vertices[j].Attributes["registry"].(string)
		return registryI < registryJ
	})
	t := table.NewWriter()
	t.SetOutputMirror(writer)
	t.AppendHeader(table.Row{"Name", "Version", "Platforms", "Description", "Registry"})
	for _, vertex := range vertices {
		t.AppendRow(table.Row{
			vertex.Attributes["name"],
			vertex.Attributes["version"],
			strings.Join(vertex.Attributes["platforms"].([]string), ", "),
			vertex.Attributes["description"],
			vertex.Attributes["registry"],
		})
	}

	t.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1, AutoMerge: true},
		{Number: 2, AutoMerge: true},
		{Number: 4, AutoMerge: true},
		{Number: 5, AutoMerge: true},
	})

	style := table.StyleLight
	style.Options.DrawBorder = false
	t.SetStyle(style)
	t.Render()
	return nil
}
