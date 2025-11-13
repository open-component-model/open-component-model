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
		Short: "List available plugin binaries from a plugin registry.",
		Args:  cobra.ExactArgs(0),
		Long:  ``,
		Example: `  # List available plugin binaries from a registry using the flag.
  ocm plugin registry list --registry ocm.software/plugin-registry:v1.0.0

 NAME       │ VERSION         │ PLATFORM    │ DESCRIPTION       │ REGISTRY
────────────┼─────────────────┼─────────────┼───────────────────┼───────────────────────────────────────────────────────
 helm       │ 1.2.0           │ linux/amd64 │ An exmpale desc   │ ocm.software/plugin-registry:v1.0.0
 docker     │ 1.0.0           │ macOs/arm64 │                   │
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
		// TODO: Discuss if multiple registries should be supported via flag
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

	// The DAG is used to represent the plugins and their versions in a pretty way
	// TODO: Discuss/Find out if the same is possible using dag.Discover (First attempts failed because of different
	//       types in the graph)
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
			return fmt.Errorf("could not access ocm repository: %w", err)
		}

		descs, err := ocm.GetComponentVersions(ctx, ocm.GetComponentVersionsOptions{}, ref.Component, ref.Version, repo)
		if err != nil {
			return fmt.Errorf("getting component reference and versions failed: %w", err)
		}

		for _, d := range descs {
			for _, r := range d.Component.References {
				logger.Debug("Adding plugin to graph", "plugin", r.Name, "version", r.Version, "registry", reg)

				var desc, os, arch string
				for _, l := range r.Labels {
					if l.Name == "description" {
						desc = strings.Trim(string(l.Value), "\"")
					}
					if l.Name == "os" {
						os = strings.Trim(string(l.Value), "\"")
					}
					if l.Name == "arch" {
						arch = strings.Trim(string(l.Value), "\"")
					}
				}

				// Need a unique key for each plugin version + platform
				id := fmt.Sprintf("%s/%s:%s-%s/%s", reg, r.Name, r.Version, os, arch)
				if err := graph.AddVertex(
					id,
					map[string]any{
						"name":        r.Name,
						"version":     r.Version,
						"registry":    reg,
						"description": desc,
						"os":          os,
						"arch":        arch,
					}); err != nil {
					// Currently, only an "Already exists" error is returned, which we can safely ignore.
					logger.Warn("Failed to add vertex", "id", id, "error", err)
					continue
				}
				roots = append(roots, id)
			}
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
		serializer := list.ListSerializerFunc[string](serializeVerticesToJSON)
		return list.New(ctx, graph, list.WithListSerializer(serializer), list.WithRoots(roots...)), nil
	case render.OutputFormatNDJSON.String():
		serializer := list.ListSerializerFunc[string](serializeVerticesToNDJSON)
		return list.New(ctx, graph, list.WithListSerializer(serializer), list.WithRoots(roots...)), nil
	case render.OutputFormatYAML.String():
		serializer := list.ListSerializerFunc[string](serializeVerticesToYAML)
		return list.New(ctx, graph, list.WithListSerializer(serializer), list.WithRoots(roots...)), nil
	case render.OutputFormatTable.String():
		serializer := list.ListSerializerFunc[string](serializeVerticesToTable)
		return list.New(ctx, graph, list.WithListSerializer(serializer), list.WithRoots(roots...)), nil
	default:
		return nil, fmt.Errorf("invalid output format %q", format)
	}
}

type PluginInfo struct {
	Name        string `json:"name"        yaml:"name"`
	Version     string `json:"version"     yaml:"version"`
	Os          string `json:"os"          yaml:"os"`
	Arch        string `json:"arch"        yaml:"arch"`
	Description string `json:"description" yaml:"description"`
	Registry    string `json:"registry"    yaml:"registry"`
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
			Os:          vertex.Attributes["os"].(string),
			Arch:        vertex.Attributes["arch"].(string),
			Description: vertex.Attributes["description"].(string),
			Registry:    vertex.Attributes["registry"].(string),
		})
	}
	return plugins
}

func serializeVerticesToJSON(writer io.Writer, vertices []*dag.Vertex[string]) error {
	plugins := convertVerticesToPluginInfos(vertices)
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(plugins)
}

func serializeVerticesToNDJSON(writer io.Writer, vertices []*dag.Vertex[string]) error {
	plugins := convertVerticesToPluginInfos(vertices)
	encoder := json.NewEncoder(writer)
	for _, plugin := range plugins {
		if err := encoder.Encode(plugin); err != nil {
			return err
		}
	}
	return nil
}

func serializeVerticesToYAML(writer io.Writer, vertices []*dag.Vertex[string]) error {
	plugins := convertVerticesToPluginInfos(vertices)
	encoder := yaml.NewEncoder(writer)
	defer encoder.Close()
	return encoder.Encode(plugins)
}

func serializeVerticesToTable(writer io.Writer, vertices []*dag.Vertex[string]) error {
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
	t.AppendHeader(table.Row{"Name", "Version", "Platform", "Description", "Registry"})
	for _, vertex := range vertices {
		// Prettify platform display
		platform := "n/a"
		switch {
		case vertex.Attributes["os"] == "" && vertex.Attributes["arch"] == "":
			platform = "n/a"
		case vertex.Attributes["os"] != "" && vertex.Attributes["arch"] == "":
			platform = vertex.Attributes["os"].(string)
		case vertex.Attributes["os"] == "" && vertex.Attributes["arch"] != "":
			platform = vertex.Attributes["arch"].(string)
		}

		t.AppendRow(table.Row{
			vertex.Attributes["name"],
			vertex.Attributes["version"],
			platform,
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
