package componentversion

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/progress"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/resource"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/cmd/setup/hooks"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/enum"
	"ocm.software/open-component-model/cli/internal/flags/file"
	"ocm.software/open-component-model/cli/internal/flags/log"
	"ocm.software/open-component-model/cli/internal/reference/compref"
	ocmsync "ocm.software/open-component-model/cli/internal/sync"
)

const (
	FlagConcurrencyLimit               = "concurrency-limit"
	FlagRepositoryRef                  = "repository"
	FlagComponentConstructorPath       = "constructor"
	FlagBlobCacheDirectory             = "blob-cache-directory"
	FlagComponentVersionConflictPolicy = "component-version-conflict-policy"
	FlagSkipReferenceDigestProcessing  = "skip-reference-digest-processing"

	DefaultComponentConstructorBaseName = "component-constructor"
	LegacyDefaultArchiveName            = "transport-archive"
)

type ComponentVersionConflictPolicy string

const (
	ComponentVersionConflictPolicyAbortAndFail ComponentVersionConflictPolicy = "abort-and-fail"
	ComponentVersionConflictPolicySkip         ComponentVersionConflictPolicy = "skip"
	ComponentVersionConflictPolicyReplace      ComponentVersionConflictPolicy = "replace"
)

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
	}

	cmd.Flags().Int(FlagConcurrencyLimit, 4, "maximum number of component versions that can be constructed concurrently.")
	cmd.Flags().StringP(FlagRepositoryRef, string(FlagRepositoryRef[0]), LegacyDefaultArchiveName, "repository ref")
	file.VarP(cmd.Flags(), FlagComponentConstructorPath, string(FlagComponentConstructorPath[0]), DefaultComponentConstructorBaseName+".yaml", "path to the component constructor file")
	cmd.Flags().String(FlagBlobCacheDirectory, filepath.Join(".ocm", "cache"), "path to the blob cache directory")
	enum.Var(cmd.Flags(), FlagComponentVersionConflictPolicy, ComponentVersionConflictPolicies(), "policy to apply when a component version already exists in the repository")
	cmd.Flags().Bool(FlagSkipReferenceDigestProcessing, false, "skip digest processing for resources and sources. Any resource referenced via access type will not have their digest updated.")

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
	pluginManager := ocmctx.FromContext(cmd.Context()).PluginManager()
	if pluginManager == nil {
		return fmt.Errorf("could not retrieve plugin manager from context")
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

	instance := &constructorProvider{
		cache:          cacheDir,
		targetRepoSpec: repoSpec,
		pluginManager:  pluginManager,
		graph:          credentialGraph,
	}

	opts := constructor.Options{
		TargetRepositoryProvider:       instance,
		ResourceRepositoryProvider:     instance,
		SourceInputMethodProvider:      instance,
		ResourceInputMethodProvider:    instance,
		CredentialProvider:             instance.graph,
		ConcurrencyLimit:               concurrencyLimit,
		ComponentVersionConflictPolicy: ComponentVersionConflictPolicy(cvConflictPolicy).ToConstructorConflictPolicy(),
	}
	if !skipReferenceDigestProcessing {
		opts.ResourceDigestProcessorProvider = instance
	}

	opts, stop, err := registerConstructorProgressTracker(cmd, opts)
	if err != nil {
		return fmt.Errorf("registering constructor progress tracker failed: %w", err)
	}
	defer stop()

	_, err = constructor.ConstructDefault(cmd.Context(), constructorSpec, opts)

	return err
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

		logger.Debug("setting access mode for CTF repository", "path", ctfRepo.Path, "ref", repositoryRef)

		var accessMode ctfv1.AccessMode = ctfv1.AccessModeReadWrite
		if _, err := os.Stat(ctfRepo.Path); os.IsNotExist(err) {
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

var _ constructor.TargetRepositoryProvider = (*constructorProvider)(nil)

type constructorProvider struct {
	cache          string
	targetRepoSpec runtime.Typed
	pluginManager  *manager.PluginManager
	graph          credentials.GraphResolver
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
		if creds, err = prov.graph.Resolve(ctx, identity); err != nil {
			return nil, fmt.Errorf("getting credentials for repository %q failed: %w", prov.targetRepoSpec, err)
		}
	} else {
		slog.WarnContext(ctx, "could not get credential consumer identity for component version repository", "repository", prov.targetRepoSpec, "error", err)
	}
	repo, err := prov.pluginManager.ComponentVersionRepositoryRegistry.GetComponentVersionRepository(ctx, prov.targetRepoSpec, creds)
	if err != nil {
		return nil, fmt.Errorf("getting component version repository for %q failed: %w", prov.targetRepoSpec, err)
	}
	return repo, err
}

func registerConstructorProgressTracker(cmd *cobra.Command, options constructor.Options) (opts constructor.Options, stop func(), err error) {
	format, err := enum.Get(cmd.Flags(), log.FormatFlagName)
	if err != nil {
		return opts, nil, fmt.Errorf("failed to get the log format from the command flag: %w", err)
	}

	switch format {
	case log.FormatText:
		pw := progress.NewWriter()
		pw.SetOutputWriter(cmd.OutOrStdout())
		pw.SetUpdateFrequency(100 * time.Millisecond)
		pw.SetAutoStop(false)
		var trackers ocmsync.Map[string, *progress.Tracker]
		options.OnStartComponentConstruct = func(_ context.Context, component *constructorruntime.Component) error {
			key := component.Name + "/" + component.Version
			tracker := &progress.Tracker{
				Message: "component " + key,
				Total:   1,
				Units: progress.Units{
					Formatter: func(value int64) string {
						base := fmt.Sprintf("%d component version", value)
						if value > 1 {
							base += "s"
						}
						return base
					},
				},
			}
			trackers.Store(key, tracker)
			pw.AppendTracker(tracker)
			return nil
		}
		options.OnEndComponentConstruct = func(_ context.Context, descriptor *descriptor.Descriptor, err error) error {
			if err != nil {
				return nil
			}
			key := descriptor.Component.Name + "/" + descriptor.Component.Version
			tracker, ok := trackers.Load(key)
			if !ok {
				return fmt.Errorf("tracker for component %q not found", key)
			}
			tracker.UpdateMessage(tracker.Message + " constructed")
			tracker.Increment(1)
			tracker.MarkAsDone()
			return nil
		}
		// TODO(jakobmoellerdev): Add Resource and Source tracking in more detail so we can track those as well.
		go func() {
			// this is the actual blocking loop
			pw.Render()
		}()

		// Stop function to Poll for the progress writer to finish rendering
		// and to ensure that all renderings are complete before returning.
		// Bound to the command context to ensure it stops when the command is done and can
		// be cancelled.
		stop := func() {
			pw.Stop()
		wait:
			for {
				select {
				case <-cmd.Context().Done():
					return
				default:
					if !pw.IsRenderInProgress() {
						break wait
					}
				}
			}
		}

		return options, stop, nil
	case log.FormatJSON:
		logger, err := log.GetBaseLogger(cmd)
		if err != nil {
			return opts, nil, fmt.Errorf("could not retrieve logger: %w", err)
		}
		logger = logger.With("realm", "cli")
		options.OnStartComponentConstruct = func(ctx context.Context, component *constructorruntime.Component) error {
			logger.InfoContext(ctx, "starting component construction",
				"component", component.Name,
				"version", component.Version,
			)
			return nil
		}
		options.OnEndComponentConstruct = func(ctx context.Context, descriptor *descriptor.Descriptor, err error) error {
			if err != nil {
				logger.ErrorContext(ctx, "component construction failed",
					"error", err,
				)
			} else {
				logger.InfoContext(ctx, "component construction completed",
					"component", descriptor.Component.Name,
					"version", descriptor.Component.Version,
				)
			}
			return nil
		}
		return options, func() {}, nil
	}

	return opts, nil, fmt.Errorf("unknown log format to track component construction: %q", format)
}
