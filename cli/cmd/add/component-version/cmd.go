package componentversion

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/dag"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/resource"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/cmd/setup/hooks"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/enum"
	"ocm.software/open-component-model/cli/internal/flags/file"
	"ocm.software/open-component-model/cli/internal/flags/log"
	"ocm.software/open-component-model/cli/internal/reference/compref"
	"ocm.software/open-component-model/cli/internal/render"
	"ocm.software/open-component-model/cli/internal/render/graph/list"
	"ocm.software/open-component-model/cli/internal/render/graph/tree"
	"ocm.software/open-component-model/cli/internal/repository/ocm"
	"ocm.software/open-component-model/cli/internal/subsystem"
)

const (
	FlagConcurrencyLimit                   = "concurrency-limit"
	FlagRepositoryRef                      = "repository"
	FlagComponentConstructorPath           = "constructor"
	FlagBlobCacheDirectory                 = "blob-cache-directory"
	FlagComponentVersionConflictPolicy     = "component-version-conflict-policy"
	FlagExternalComponentVersionCopyPolicy = "external-component-version-copy-policy"
	FlagSkipReferenceDigestProcessing      = "skip-reference-digest-processing"
	FlagOutput                             = "output"
	FlagDisplayMode                        = "display-mode"

	DefaultComponentConstructorBaseName = "component-constructor"
	LegacyDefaultArchiveName            = "transport-archive"
)

type ComponentVersionConflictPolicy string

const (
	ComponentVersionConflictPolicyAbortAndFail ComponentVersionConflictPolicy = "abort-and-fail"
	ComponentVersionConflictPolicySkip         ComponentVersionConflictPolicy = "skip"
	ComponentVersionConflictPolicyReplace      ComponentVersionConflictPolicy = "replace"
)

type ExternalComponentVersionCopyPolicy string

const (
	ExternalComponentVersionCopyPolicyCopyOrFail ExternalComponentVersionCopyPolicy = "copy-or-fail"
	ExternalComponentVersionCopyPolicySkip       ExternalComponentVersionCopyPolicy = "skip"
)

func (p ExternalComponentVersionCopyPolicy) ToConstructorPolicy() constructor.ExternalComponentVersionCopyPolicy {
	switch p {
	case ExternalComponentVersionCopyPolicyCopyOrFail:
		return constructor.ExternalComponentVersionCopyPolicyCopyOrFail
	case ExternalComponentVersionCopyPolicySkip:
		return constructor.ExternalComponentVersionCopyPolicySkip
	default:
		return constructor.ExternalComponentVersionCopyPolicySkip
	}
}

func ExternalComponentVersionCopyPolicies() []string {
	return []string{
		string(ExternalComponentVersionCopyPolicySkip),
		string(ExternalComponentVersionCopyPolicyCopyOrFail),
	}
}

func (p ComponentVersionConflictPolicy) ToConstructorConflictPolicy() constructor.ComponentVersionConflictPolicy {
	switch p {
	case ComponentVersionConflictPolicyReplace:
		return constructor.ComponentVersionConflictReplace
	case ComponentVersionConflictPolicySkip:
		return constructor.ComponentVersionConflictSkip
	default:
		return constructor.ComponentVersionConflictAbortAndFail
	}
}

func ComponentVersionConflictPolicies() []string {
	return []string{
		string(ComponentVersionConflictPolicyAbortAndFail),
		string(ComponentVersionConflictPolicySkip),
		string(ComponentVersionConflictPolicyReplace),
	}
}

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:        "component-version",
		Aliases:    []string{"cv", "componentversion", "component-versions", "cvs", "componentversions"},
		SuggestFor: []string{"component", "components", "version", "versions"},
		Short:      fmt.Sprintf("Add component version(s) to an OCM Repository based on a %[1]q file", DefaultComponentConstructorBaseName),
		Args:       cobra.NoArgs,
		Long: fmt.Sprintf(`Add component version(s) to an OCM repository that can be reused for transfers.

A %[1]q file is used to specify the component version(s) to be added. It can contain both a single component or many components.

By default, the command will look for a file named "%[1]s.yaml" or "%[1]s.yml" in the current directory.
If given a path to a directory, the command will look for a file named "%[1]s.yaml" or "%[1]s.yml" in that directory.
If given a path to a file, the command will attempt to use that file as the %[1]q file.

If you provide a working directory, all paths in the %[1]q file will be resolved relative to that directory.
Otherwise the path to the %[1]q file will be used as the working directory.
You are only allowed to reference files within the working directory or sub-directories of the working directory.

Repository Reference Format:
	[type::]{repository}

For known types, currently only {%[2]s} are supported, which can be shortened to {%[3]s} respectively for convenience.

If no type is given, the repository specification is interpreted based on introspection and heuristics:

- URL schemes or domain patterns -> OCI registry
- Local paths -> CTF archive

In case the CTF archive does not exist, it will be created by default.
If not specified, it will be created with the name "transport-archive".
`,
			DefaultComponentConstructorBaseName,
			strings.Join([]string{ociv1.Type, ctfv1.Type}, "|"),
			strings.Join([]string{ociv1.ShortType, ociv1.ShortType2, ctfv1.ShortType, ctfv1.ShortType2}, "|"),
		),
		Example: strings.TrimSpace(fmt.Sprintf(`
Adding component versions to a CTF archive:

add component-version --%[1]s ./path/to/transport-archive --%[2]s ./path/to/%[3]s.yaml
add component-version --%[1]s /tmp/my-archive --%[2]s constructor.yaml

Adding component versions to an OCI registry:

add component-version --%[1]s ghcr.io/my-org/my-repo --%[2]s %[3]s.yaml
add component-version --%[1]s https://my-registry.com/my-repo --%[2]s %[3]s.yaml
add component-version --%[1]s localhost:5000/my-repo --%[2]s %[3]s.yaml

Specifying repository types explicitly:

add component-version --%[1]s ctf::./local/archive --%[2]s %[3]s.yaml
add component-version --%[1]s oci::http://localhost:8080/my-repo --%[2]s %[3]s.yaml
`, FlagRepositoryRef, FlagComponentConstructorPath, DefaultComponentConstructorBaseName)),
		RunE:              AddComponentVersion,
		PersistentPreRunE: persistentPreRunE,
		DisableAutoGenTag: true,
		Annotations: map[string]string{
			subsystem.Annotation: "input-method",
		},
	}

	cmd.Flags().Int(FlagConcurrencyLimit, 4, "maximum number of component versions that can be constructed concurrently.")
	cmd.Flags().StringP(FlagRepositoryRef, string(FlagRepositoryRef[0]), LegacyDefaultArchiveName, "repository ref")
	file.VarP(cmd.Flags(), FlagComponentConstructorPath, string(FlagComponentConstructorPath[0]), DefaultComponentConstructorBaseName+".yaml", "path to the component constructor file")
	cmd.Flags().String(FlagBlobCacheDirectory, filepath.Join(".ocm", "cache"), "path to the blob cache directory")
	enum.Var(cmd.Flags(), FlagComponentVersionConflictPolicy, ComponentVersionConflictPolicies(), "policy to apply when a component version already exists in the repository")
	enum.Var(cmd.Flags(), FlagExternalComponentVersionCopyPolicy, ExternalComponentVersionCopyPolicies(), "policy to apply when a component reference to a component version outside of the constructor or target repository is encountered")
	cmd.Flags().Bool(FlagSkipReferenceDigestProcessing, false, "skip digest processing for resources and sources. Any resource referenced via access type will not have their digest updated.")
	enum.VarP(cmd.Flags(), FlagOutput, "o", []string{render.OutputFormatTable.String(), render.OutputFormatYAML.String(), render.OutputFormatJSON.String(), render.OutputFormatNDJSON.String(), render.OutputFormatTree.String()}, "output format of the component descriptors")
	enum.VarP(cmd.Flags(), FlagDisplayMode, "", []string{render.StaticRenderMode, render.LiveRenderMode}, `display mode can be used in combination with --recursive
  static: print the output once the complete component graph is discovered
  live (experimental): continuously updates the output to represent the current construction state of the component graph`)

	return cmd
}

func persistentPreRunE(cmd *cobra.Command, _ []string) error {
	constructorFile, err := getComponentConstructorFile(cmd)
	if err != nil {
		return fmt.Errorf("getting component constructor failed: %w", err)
	}

	// If the working directory isn't set yet, default to the constructorFile file's dir.
	cfg := hooks.Config{}
	ctx := cmd.Context()
	if fsCfg := ocmctx.FromContext(ctx).FilesystemConfig(); fsCfg == nil || fsCfg.WorkingDirectory == "" {
		path := constructorFile.String()
		// if our flag is not absolute, make it absolute to pass into potential plugins
		if path, err = filepath.Abs(path); err != nil {
			return err
		}
		cfg.WorkingDirectory = filepath.Dir(path)
		slog.DebugContext(ctx, "setting working directory from constructorFile path",
			slog.String("working-directory", cfg.WorkingDirectory))
	}

	if err := hooks.PreRunEWithConfig(cmd, cfg); err != nil {
		return fmt.Errorf("pre-run configuration for component constructors failed: %w", err)
	}

	return nil
}

func AddComponentVersion(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	pluginManager := ocmctx.FromContext(cmd.Context()).PluginManager()
	if pluginManager == nil {
		return fmt.Errorf("could not retrieve plugin manager from context")
	}

	ocmContext := ocmctx.FromContext(ctx)
	if ocmContext == nil {
		return fmt.Errorf("no OCM context found")
	}

	credentialGraph := ocmctx.FromContext(cmd.Context()).CredentialGraph()
	if credentialGraph == nil {
		return fmt.Errorf("could not retrieve credential graph from context")
	}

	concurrencyLimit, err := cmd.Flags().GetInt(FlagConcurrencyLimit)
	if err != nil {
		return fmt.Errorf("getting concurrency-limit flag failed: %w", err)
	}

	skipReferenceDigestProcessing, err := cmd.Flags().GetBool(FlagSkipReferenceDigestProcessing)
	if err != nil {
		return fmt.Errorf("getting skip-reference-digest-processing flag failed: %w", err)
	}

	cvConflictPolicy, err := enum.Get(cmd.Flags(), FlagComponentVersionConflictPolicy)
	if err != nil {
		return fmt.Errorf("getting component-version-conflict-policy flag failed: %w", err)
	}

	evCopyPolicy, err := enum.Get(cmd.Flags(), FlagExternalComponentVersionCopyPolicy)
	if err != nil {
		return fmt.Errorf("getting external-component-version-copy-policy flag failed: %w", err)
	}

	repoSpec, err := GetRepositorySpec(cmd)
	if err != nil {
		return fmt.Errorf("getting repository spec failed: %w", err)
	}

	cacheDir, err := cmd.Flags().GetString(FlagBlobCacheDirectory)
	if err != nil {
		return fmt.Errorf("getting blob cache directory flag failed: %w", err)
	}

	constructorFile, err := getComponentConstructorFile(cmd)
	if err != nil {
		return fmt.Errorf("getting component constructor path failed: %w", err)
	}

	constructorSpec, err := GetComponentConstructor(constructorFile)
	if err != nil {
		return fmt.Errorf("getting component constructor failed: %w", err)
	}

	output, err := enum.Get(cmd.Flags(), FlagOutput)
	if err != nil {
		return fmt.Errorf("getting output flag failed: %w", err)
	}

	displayMode, err := enum.Get(cmd.Flags(), FlagDisplayMode)
	if err != nil {
		return fmt.Errorf("getting display-mode flag failed: %w", err)
	}

	repositoryRef, err := cmd.Flags().GetString(FlagRepositoryRef)
	if err != nil {
		return fmt.Errorf("getting repository reference flag failed: %w", err)
	}

	config := ocmctx.FromContext(cmd.Context()).Configuration()
	ref, err := compref.ParseRepository(repositoryRef, compref.WithCTFAccessMode(ctfv1.AccessModeReadWrite))
	if err != nil {
		return fmt.Errorf("parsing repository reference %q failed: %w", repositoryRef, err)
	}

	repoProvider, err := ocm.NewComponentRepositoryProvider(cmd.Context(),
		pluginManager.ComponentVersionRepositoryRegistry,
		credentialGraph,
		ocm.WithRepository(ref), ocm.WithConfig(config),
	)
	if err != nil {
		return fmt.Errorf("could not initialize ocm repository: %w", err)
	}

	instance := &constructorProvider{
		cache:              cacheDir,
		targetRepoSpec:     repoSpec,
		repositoryProvider: repoProvider,
		pluginManager:      pluginManager,
		graph:              credentialGraph,
	}

	opts := constructor.Options{
		TargetRepositoryProvider:            instance,
		ResourceRepositoryProvider:          instance,
		SourceInputMethodProvider:           instance,
		ResourceInputMethodProvider:         instance,
		ExternalComponentRepositoryProvider: instance,
		Resolver:                            instance.graph,
		ConcurrencyLimit:                    concurrencyLimit,
		ComponentVersionConflictPolicy:      ComponentVersionConflictPolicy(cvConflictPolicy).ToConstructorConflictPolicy(),
		ExternalComponentVersionCopyPolicy:  ExternalComponentVersionCopyPolicy(evCopyPolicy).ToConstructorPolicy(),
	}
	if !skipReferenceDigestProcessing {
		opts.ResourceDigestProcessorProvider = instance
	}

	constr := constructor.NewDefaultConstructor(constructorSpec, opts)
	if err := renderComponents(cmd, constr, output, displayMode); err != nil {
		return fmt.Errorf("failed to render components recursively: %w", err)
	}
	return nil
}

func GetRepositorySpec(cmd *cobra.Command) (runtime.Typed, error) {
	repositoryRef, err := cmd.Flags().GetString(FlagRepositoryRef)
	if err != nil {
		return nil, fmt.Errorf("getting repository reference flag failed: %w", err)
	}

	typed, err := compref.ParseRepository(repositoryRef)
	if err != nil {
		return nil, fmt.Errorf("failed to parse repository: %w", err)
	}

	if ctfRepo, ok := typed.(*ctfv1.Repository); ok {
		logger, err := log.GetBaseLogger(cmd)
		if err != nil {
			return nil, fmt.Errorf("getting base logger failed: %w", err)
		}

		logger.Debug("setting access mode for CTF repository", "path", ctfRepo.FilePath, "ref", repositoryRef)

		var accessMode ctfv1.AccessMode = ctfv1.AccessModeReadWrite
		if _, err := os.Stat(ctfRepo.FilePath); os.IsNotExist(err) {
			accessMode += "|" + ctfv1.AccessModeCreate
		}
		ctfRepo.AccessMode = accessMode
	}

	return typed, nil
}

func GetComponentConstructor(file *file.Flag) (*constructorruntime.ComponentConstructor, error) {
	path := file.String()
	constructorStream, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("opening component constructor %q failed: %w", path, err)
	}
	constructorData, err := io.ReadAll(constructorStream)
	if err != nil {
		return nil, fmt.Errorf("reading component constructor %q failed: %w", path, err)
	}

	data := constructorv1.ComponentConstructor{}
	if err := yaml.Unmarshal(constructorData, &data); err != nil {
		return nil, fmt.Errorf("unmarshalling component constructor %q failed: %w", path, err)
	}

	return constructorruntime.ConvertToRuntimeConstructor(&data), nil
}

func getComponentConstructorFile(cmd *cobra.Command) (*file.Flag, error) {
	constructorFlag, err := file.Get(cmd.Flags(), FlagComponentConstructorPath)
	if err != nil {
		return nil, fmt.Errorf("getting component constructor path flag failed: %w", err)
	}
	if !constructorFlag.Exists() {
		return nil, fmt.Errorf("component constructor %q does not exist", constructorFlag.String())
	} else if constructorFlag.IsDir() {
		return nil, fmt.Errorf("path %q is a directory but must point to a component constructor", constructorFlag.String())
	}
	return constructorFlag, nil
}

var (
	_ constructor.TargetRepositoryProvider            = (*constructorProvider)(nil)
	_ constructor.ExternalComponentRepositoryProvider = (*constructorProvider)(nil)
)

type constructorProvider struct {
	cache              string
	targetRepoSpec     runtime.Typed
	repositoryProvider ocm.ComponentVersionRepositoryForComponentProvider
	pluginManager      *manager.PluginManager
	graph              credentials.Resolver
}

func (prov *constructorProvider) GetExternalRepository(ctx context.Context, name, version string) (repository.ComponentVersionRepository, error) {
	if prov.repositoryProvider == nil {
		return nil, fmt.Errorf("cannot fetch external component version %s:%s repository provider configured", name, version)
	}
	return prov.repositoryProvider.GetComponentVersionRepositoryForComponent(ctx, name, version)
}

func (prov *constructorProvider) GetDigestProcessor(ctx context.Context, resource *descriptor.Resource) (constructor.ResourceDigestProcessor, error) {
	return prov.pluginManager.DigestProcessorRegistry.GetPlugin(ctx, resource.Access)
}

func (prov *constructorProvider) GetResourceInputMethod(ctx context.Context, resource *constructorruntime.Resource) (constructor.ResourceInputMethod, error) {
	return prov.pluginManager.InputRegistry.GetResourceInputPlugin(ctx, resource.Input)
}

func (prov *constructorProvider) GetSourceInputMethod(ctx context.Context, src *constructorruntime.Source) (constructor.SourceInputMethod, error) {
	return prov.pluginManager.InputRegistry.GetSourceInputPlugin(ctx, src.Input)
}

func (prov *constructorProvider) GetResourceRepository(ctx context.Context, resource *constructorruntime.Resource) (constructor.ResourceRepository, error) {
	plugin, err := prov.pluginManager.ResourcePluginRegistry.GetResourcePlugin(ctx, resource.Access)
	if err != nil {
		return nil, fmt.Errorf("getting plugin for resource %q failed: %w", resource.Access, err)
	}
	return &constructorPlugin{plugin: plugin}, nil
}

type constructorPlugin struct {
	plugin resource.Repository
}

func (c *constructorPlugin) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *constructorruntime.Resource) (identity runtime.Identity, err error) {
	return c.plugin.GetResourceCredentialConsumerIdentity(ctx, constructorruntime.ConvertToDescriptorResource(resource))
}

func (c *constructorPlugin) DownloadResource(ctx context.Context, res *descriptor.Resource, credentials map[string]string) (content blob.ReadOnlyBlob, err error) {
	return c.plugin.DownloadResource(ctx, res, credentials)
}

func (prov *constructorProvider) GetTargetRepository(ctx context.Context, _ *constructorruntime.Component) (constructor.TargetRepository, error) {
	var creds map[string]string
	identity, err := prov.pluginManager.ComponentVersionRepositoryRegistry.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, prov.targetRepoSpec)
	if err == nil {
		if prov.graph != nil {
			if creds, err = prov.graph.Resolve(ctx, identity); err != nil {
				if errors.Is(err, credentials.ErrNotFound) {
					slog.DebugContext(ctx, fmt.Sprintf("resolving credentials for repository %q failed: %s", prov.targetRepoSpec, err.Error()))
				} else {
					return nil, fmt.Errorf("resolving credentials for repository %q failed: %w", prov.targetRepoSpec, err)
				}
			}
		}
	} else {
		slog.WarnContext(ctx, "could not get credential consumer identity for component version repository", "repository", prov.targetRepoSpec, "error", err)
	}

	return prov.pluginManager.ComponentVersionRepositoryRegistry.GetComponentVersionRepository(ctx, prov.targetRepoSpec, creds)
}

func renderComponents(cmd *cobra.Command, constr constructor.Constructor, format string, mode string) error {
	switch mode {
	case render.StaticRenderMode:
		graph := constr.GetGraph()
		err := constr.Construct(cmd.Context())
		if err != nil {
			return fmt.Errorf("constructing component versions failed: %w", err)
		}

		var roots []string
		if err := graph.WithReadLock(func(d *dag.DirectedAcyclicGraph[string]) error {
			roots = d.Roots()
			return nil
		}); err != nil {
			return fmt.Errorf("getting roots of component version graph failed: %w", err)
		}

		renderer, err := buildRenderer(cmd.Context(), graph, roots, format)
		if err != nil {
			return fmt.Errorf("building renderer failed: %w", err)
		}

		if err := render.RenderOnce(cmd.Context(), renderer, render.WithWriter(cmd.OutOrStdout())); err != nil {
			return err
		}
	case render.LiveRenderMode:
		graph := constr.GetGraph()
		// Start the render loop.
		renderCtx, cancel := context.WithCancel(cmd.Context())
		defer cancel()

		renderer, err := buildRenderer(cmd.Context(), graph, nil, format)
		if err != nil {
			return fmt.Errorf("building renderer failed: %w", err)
		}

		wait := render.RunRenderLoop(renderCtx, renderer, render.WithRenderOptions(render.WithWriter(cmd.OutOrStdout())))

		err = constr.Construct(cmd.Context())
		if err != nil {
			return fmt.Errorf("constructing component versions failed: %w", err)
		}

		if err := wait(); !errors.Is(err, context.Canceled) {
			return fmt.Errorf("rendering component version graph failed: %w", err)
		}
	}
	return nil
}

func buildRenderer(ctx context.Context, graph *syncdag.SyncedDirectedAcyclicGraph[string], roots []string, format string) (render.Renderer, error) {
	// Initialize renderer based on the requested output format.
	switch format {
	case render.OutputFormatJSON.String():
		serializer := list.NewSerializer(list.WithVertexSerializer(list.VertexSerializerFunc[string](serializeVertexToDescriptor)), list.WithOutputFormat[string](render.OutputFormatJSON))
		return list.New(ctx, graph, list.WithListSerializer(serializer), list.WithRoots(roots...)), nil
	case render.OutputFormatNDJSON.String():
		serializer := list.NewSerializer(list.WithVertexSerializer(list.VertexSerializerFunc[string](serializeVertexToDescriptor)), list.WithOutputFormat[string](render.OutputFormatNDJSON))
		return list.New(ctx, graph, list.WithListSerializer(serializer), list.WithRoots(roots...)), nil
	case render.OutputFormatYAML.String():
		serializer := list.NewSerializer(list.WithVertexSerializer(list.VertexSerializerFunc[string](serializeVertexToDescriptor)), list.WithOutputFormat[string](render.OutputFormatYAML))
		return list.New(ctx, graph, list.WithListSerializer(serializer), list.WithRoots(roots...)), nil
	case render.OutputFormatTree.String():
		serializer := tree.VertexSerializerFunc[string](serializeVertexToDescriptorTree)
		return tree.New(ctx, graph, tree.WithVertexSerializerFunc(serializer), tree.WithRoots(roots...)), nil
	case render.OutputFormatTable.String():
		serializer := list.ListSerializerFunc[string](serializeVerticesToTable)
		return list.New(ctx, graph, list.WithListSerializer(serializer), list.WithRoots(roots...)), nil
	default:
		return nil, fmt.Errorf("invalid output format %q", format)
	}
}

func serializeVertexToDescriptorTree(vertex *dag.Vertex[string]) (tree.Row, error) {
	untypedDescriptor, ok := vertex.Attributes[constructor.AttributeDescriptor]
	if !ok {
		return tree.Row{}, fmt.Errorf("vertex %s has no %s attribute", vertex.ID, constructor.AttributeDescriptor)
	}
	desc, ok := untypedDescriptor.(*descriptor.Descriptor)
	if !ok {
		return tree.Row{}, fmt.Errorf("expected vertex %s attribute %s to be of type %T, got type %T", vertex.ID, constructor.AttributeDescriptor, &descriptor.Descriptor{}, untypedDescriptor)
	}
	descriptorV2, err := descriptor.ConvertToV2(descriptorv2.Scheme, desc)
	if err != nil {
		return tree.Row{}, fmt.Errorf("converting descriptor to v2 failed: %w", err)
	}
	identity := descriptorV2.Component.ToIdentity()
	return tree.Row{
		Component: descriptorV2.Component.Name,
		Version:   descriptorV2.Component.Version,
		Provider:  descriptorV2.Component.Provider,
		Identity:  identity.String(),
	}, nil
}

func serializeVertexToDescriptor(vertex *dag.Vertex[string]) (any, error) {
	untypedDescriptor, ok := vertex.Attributes[constructor.AttributeDescriptor]
	if !ok {
		return nil, fmt.Errorf("vertex %s has no %s attribute", vertex.ID, constructor.AttributeDescriptor)
	}
	desc, ok := untypedDescriptor.(*descriptor.Descriptor)
	if !ok {
		return nil, fmt.Errorf("expected vertex %s attribute %s to be of type %T, got type %T", vertex.ID, constructor.AttributeDescriptor, &descriptor.Descriptor{}, untypedDescriptor)
	}
	descriptorV2, err := descriptor.ConvertToV2(descriptorv2.Scheme, desc)
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
		untypedDescriptor, ok := vertex.Attributes[constructor.AttributeDescriptor]
		if !ok {
			return fmt.Errorf("vertex %s has no %s attribute", vertex.ID, constructor.AttributeDescriptor)
		}
		desc, ok := untypedDescriptor.(*descriptor.Descriptor)
		if !ok {
			return fmt.Errorf("expected vertex %s attribute %s to be of type %T, got type %T", vertex.ID, constructor.AttributeDescriptor, &descriptor.Descriptor{}, desc)
		}

		t.AppendRow(table.Row{desc.Component.Name, desc.Component.Version, desc.Component.Provider.Name})
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
