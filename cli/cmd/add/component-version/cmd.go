package componentversion

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/progress"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/runtime"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/enum"
	"ocm.software/open-component-model/cli/internal/flags/file"
	"ocm.software/open-component-model/cli/internal/flags/log"
)

const (
	FlagConcurrencyLimit               = "concurrency-limit"
	FlagRepositoryRef                  = "repository"
	FlagComponentConstructorPath       = "constructor"
	FlagCopyResources                  = "copy-resources"
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

func ComponentVersionOverridePolicies() []string {
	return []string{
		string(ComponentVersionConflictPolicyAbortAndFail),
		string(ComponentVersionConflictPolicySkip),
		string(ComponentVersionConflictPolicyReplace),
	}
}

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:        fmt.Sprintf("component-version"),
		Aliases:    []string{"cv", "component-versions", "cvs"},
		SuggestFor: []string{"component", "components", "version", "versions"},
		Short:      fmt.Sprintf("Add component version(s) to an OCM Repository stored as Common Transport Format Archive (CTF) based on a %[1]q file", DefaultComponentConstructorBaseName),
		Args:       cobra.NoArgs,
		Long: fmt.Sprintf(`Add component version(s) to an OCM Common Transport Format Archive (CTF) that can be reused
for transfers.

A %[1]q file is used to specify the component version(s) to be added. It can contain both a single component or many
components. The component reference is used to determine the repository to add the components to.

By default, the command will look for a file named "%[1]q.yaml" or "%[1]q.yml" in the current directory.
If given a path to a directory, the command will look for a file named "%[1]s.yaml" or "%[1]s.yml" in that directory.
If given a path to a file, the command will attempt to use that file as the %[1]q file.

In case the component archive does not exist, it will be created by default.
`,
			DefaultComponentConstructorBaseName,
		),
		Example: strings.TrimSpace(fmt.Sprintf(`
Adding component versions to a non-default CTF named %[1]q based on a non-default default %[2]q file:

add component-version ./path/to/%[1]s ./path/to/%[2]s.yaml
`, LegacyDefaultArchiveName, DefaultComponentConstructorBaseName)),
		RunE:              AddComponentVersion,
		DisableAutoGenTag: true,
	}

	cmd.Flags().Int(FlagConcurrencyLimit, 4, "maximum amount of parallel requests to the repository for resolving component versions")
	file.VarP(cmd.Flags(), FlagRepositoryRef, string(FlagRepositoryRef[0]), LegacyDefaultArchiveName, "path to the repository")
	file.VarP(cmd.Flags(), FlagComponentConstructorPath, string(FlagComponentConstructorPath[0]), DefaultComponentConstructorBaseName+".yaml", "path to the repository")
	cmd.Flags().String(FlagBlobCacheDirectory, filepath.Join(".ocm", "cache"), "path to the blob cache directory")
	enum.Var(cmd.Flags(), FlagComponentVersionConflictPolicy, ComponentVersionOverridePolicies(), "policy to apply when a component version already exists in the repository")
	cmd.Flags().Bool(FlagSkipReferenceDigestProcessing, false, "skip digest processing for resources and sources. Any resource referenced via access type will not have their digest updated.")

	return cmd
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
		return fmt.Errorf("getting component-version-override-policy flag failed: %w", err)
	}

	repoSpec, err := GetRepositorySpec(cmd)
	if err != nil {
		return fmt.Errorf("getting repository spec failed: %w", err)
	}

	cacheDir, err := cmd.Flags().GetString(FlagBlobCacheDirectory)
	if err != nil {
		return fmt.Errorf("getting blob cache directory flag failed: %w", err)
	}

	constructorSpec, err := GetComponentConstructor(cmd)
	if err != nil {
		return fmt.Errorf("getting component constructor failed: %w", err)
	}

	instance := &constructorProvider{
		cache:          cacheDir,
		targetRepoSpec: repoSpec,
		PluginManager:  pluginManager,
		Graph:          credentialGraph,
	}

	opts := constructor.Options{
		TargetRepositoryProvider:       instance,
		ResourceRepositoryProvider:     instance,
		SourceInputMethodProvider:      instance,
		ResourceInputMethodProvider:    instance,
		CredentialProvider:             instance,
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

	_, err = constructor.ConstructDefault(cmd.Context(), constructorSpec, opts)
	if stop != nil {
		stop()
	}

	time.Sleep(10 * time.Millisecond)
	return err
}

func GetRepositorySpec(cmd *cobra.Command) (runtime.Typed, error) {
	repoRef, err := file.Get(cmd.Flags(), FlagRepositoryRef)
	if err != nil {
		return nil, fmt.Errorf("getting repository reference flag failed: %w", err)
	}
	var accessMode ctfv1.AccessMode = ctfv1.AccessModeReadWrite
	if !repoRef.Exists() {
		accessMode += "|" + ctfv1.AccessModeCreate
	}
	repoSpec := ctfv1.Repository{
		Path:       repoRef.String(),
		AccessMode: accessMode,
	}
	return &repoSpec, nil
}

func GetComponentConstructor(cmd *cobra.Command) (*constructorruntime.ComponentConstructor, error) {
	constructorFlag, err := file.Get(cmd.Flags(), FlagComponentConstructorPath)
	if err != nil {
		return nil, fmt.Errorf("getting component constructor path flag failed: %w", err)
	}
	if !constructorFlag.Exists() {
		return nil, fmt.Errorf("component constructor %q does not exist", constructorFlag.String())
	} else if constructorFlag.IsDir() {
		return nil, fmt.Errorf("path %q is a directory but must point to a component constructor", constructorFlag.String())
	}
	constructorStream, err := constructorFlag.Open()
	if err != nil {
		return nil, fmt.Errorf("opening component constructor %q failed: %w", constructorFlag.String(), err)
	}
	defer func() {
		if err := constructorStream.Close(); err != nil {
			slog.WarnContext(cmd.Context(), "error closing component constructor file data stream", "error", err)
		}
	}()
	constructorData, err := io.ReadAll(constructorStream)
	if err != nil {
		return nil, fmt.Errorf("reading component constructor %q failed: %w", constructorFlag.String(), err)
	}
	data := constructorv1.ComponentConstructor{}
	if err := yaml.Unmarshal(constructorData, &data); err != nil {
		return nil, fmt.Errorf("unmarshalling component constructor %q failed: %w", constructorFlag.String(), err)
	}

	converted := constructorruntime.ConvertToRuntimeConstructor(&data)

	return converted, nil
}

var _ constructor.TargetRepositoryProvider = (*constructorProvider)(nil)

type constructorProvider struct {
	cache          string
	targetRepoSpec runtime.Typed
	*manager.PluginManager
	*credentials.Graph
}

func (prov *constructorProvider) GetDigestProcessor(ctx context.Context, resource *descriptor.Resource) (constructor.ResourceDigestProcessor, error) {
	return prov.PluginManager.DigestProcessorRegistry.GetPlugin(ctx, resource.Access)
}

func (prov *constructorProvider) GetResourceInputMethod(ctx context.Context, resource *constructorruntime.Resource) (constructor.ResourceInputMethod, error) {
	return prov.PluginManager.InputRegistry.GetResourceInputPlugin(ctx, resource.Input)
}

func (prov *constructorProvider) GetSourceInputMethod(ctx context.Context, src *constructorruntime.Source) (constructor.SourceInputMethod, error) {
	return prov.PluginManager.InputRegistry.GetSourceInputPlugin(ctx, src.Input)
}

func (prov *constructorProvider) GetResourceRepository(ctx context.Context, resource *constructorruntime.Resource) (constructor.ResourceRepository, error) {
	return prov.PluginManager.ResourcePluginRegistry.GetResourcePlugin(ctx, resource.Access)
}

func (prov *constructorProvider) GetTargetRepository(ctx context.Context, _ *constructorruntime.Component) (constructor.TargetRepository, error) {
	plugin, err := prov.PluginManager.ComponentVersionRepositoryRegistry.GetPlugin(ctx, prov.targetRepoSpec)
	if err != nil {
		return nil, fmt.Errorf("getting plugin for repository %q failed: %w", prov.targetRepoSpec, err)
	}
	var creds map[string]string
	identity, err := plugin.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, prov.targetRepoSpec)
	if err == nil {
		if creds, err = prov.Graph.Resolve(ctx, identity); err != nil {
			return nil, fmt.Errorf("getting credentials for repository %q failed: %w", prov.targetRepoSpec, err)
		}
	}
	repo, err := plugin.GetComponentVersionRepository(ctx, prov.targetRepoSpec, creds)
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
		trackers := map[string]*progress.Tracker{}
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
			trackers[key] = tracker
			pw.AppendTracker(tracker)
			return nil
		}
		options.OnEndComponentConstruct = func(_ context.Context, descriptor *descriptor.Descriptor, err error) error {
			if err != nil {
				return nil
			}
			key := descriptor.Component.Name + "/" + descriptor.Component.Version
			tracker := trackers[key]
			tracker.UpdateMessage(tracker.Message + " constructed")
			tracker.Increment(1)
			tracker.MarkAsDone()
			return nil
		}
		// TODO Add Resource and Source tracking in more detail
		go func() {
			for _, tracker := range trackers {
				tracker.Start()
			}
			pw.Render()
		}()
		return options, pw.Stop, nil
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
		return options, nil, nil
	}

	return opts, nil, fmt.Errorf("unknown log format to track component construction: %q", format)
}
