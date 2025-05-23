package componentversion

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/constructor"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/ocmrepository/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/file"
)

const (
	FlagConcurrencyLimit         = "concurrency-limit"
	FlagRepositoryRef            = "repository"
	FlagComponentConstructorPath = "constructor"
	FlagCopyResources            = "copy-resources"
	FlagBlobCacheDirectory       = "blob-cache-directory"

	DefaultComponentConstructorBaseName = "component-constructor"
	LegacyDefaultArchiveName            = "transport-archive"
)

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
	cmd.Flags().Bool(FlagCopyResources, false, "copy external resources by-value to the archive")
	cmd.Flags().String(FlagBlobCacheDirectory, "blobs", "path to the blob cache directory")

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

	copyResources, err := cmd.Flags().GetBool(FlagCopyResources)
	if err != nil {
		return fmt.Errorf("getting copy-resources flag failed: %w", err)
	}

	repoSpec, err := GetRepositorySpec(cmd)
	if err != nil {
		return fmt.Errorf("getting repository spec failed: %w", err)
	}

	cacheDir, err := cmd.Flags().GetString(FlagBlobCacheDirectory)

	constructorSpec, err := GetComponentConstructor(cmd)
	if err != nil {
		return fmt.Errorf("getting component constructor failed: %w", err)
	}

	instance := constructorProvider{
		cache:          cacheDir,
		targetRepoSpec: repoSpec,
		PluginManager:  pluginManager,
	}

	_, err = constructor.Construct(cmd.Context(), constructorSpec, constructor.Options{
		TargetRepositoryProvider:        instance,
		ResourceRepositoryProvider:      instance,
		ResourceInputMethodProvider:     instance,
		ResourceDigestProcessorProvider: instance,
		ProcessByValue: func(resource *constructorv1.Resource) bool {
			if resource.Relation == constructorv1.ExternalRelation {
				return copyResources
			}
			return true
		},
		ConcurrencyLimit: concurrencyLimit,
	})

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

func GetComponentConstructor(cmd *cobra.Command) (*constructorv1.ComponentConstructor, error) {
	constructorFlag, err := file.Get(cmd.Flags(), FlagComponentConstructorPath)
	if err != nil {
		return nil, fmt.Errorf("getting component constructor path flag failed: %w", err)
	}
	if constructorFlag.IsDir() {
		return nil, fmt.Errorf("path %q is a directory but must point to a component constructor", constructorFlag.String())
	} else if !constructorFlag.Exists() {
		return nil, fmt.Errorf("component constructor %q does not exist", constructorFlag.String())
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
	return &data, nil
}

var _ constructor.TargetRepositoryProvider = (*constructorProvider)(nil)
var _ constructor.ResourceInputMethodProvider = (*constructorProvider)(nil)

type constructorProvider struct {
	cache          string
	targetRepoSpec runtime.Typed
	*manager.PluginManager
	*credentials.Graph
}

func (c constructorProvider) GetDigestProcessor(ctx context.Context, resource *descriptor.Resource) (constructor.ResourceDigestProcessor, error) {
	// TODO implement digest processor registry in plugin manager
	panic("implement me")
}

func (c constructorProvider) GetResourceInputMethod(ctx context.Context, resource *constructorv1.Resource) (constructor.ResourceInputMethod, error) {
	// TODO implement input method registry in plugin manager
	panic("implement me")
}

func (c constructorProvider) GetResourceRepository(ctx context.Context, resource *constructorv1.Resource) (constructor.ResourceRepository, error) {
	// TODO implement resource repository registry in plugin manager
	panic("implement me")
}

func (c constructorProvider) GetTargetRepository(ctx context.Context, _ *constructorv1.Component) (constructor.TargetRepository, error) {
	plugin, err := c.PluginManager.ComponentVersionRepositoryRegistry.GetPlugin(ctx, c.targetRepoSpec)
	if err != nil {
		return nil, fmt.Errorf("getting plugin for repository %q failed: %w", c.targetRepoSpec, err)
	}
	var creds map[string]string
	identity, err := plugin.GetIdentity(ctx, v1.GetIdentityRequest[runtime.Typed]{Typ: c.targetRepoSpec})
	if err == nil {
		if creds, err = c.Graph.Resolve(ctx, identity); err != nil {
			return nil, fmt.Errorf("getting credentials for repository %q failed: %w", c.targetRepoSpec, err)
		}
	}
	return &targetRepo{c.cache, c.targetRepoSpec, creds, plugin}, nil
}

type targetRepo struct {
	cache       string
	spec        runtime.Typed
	credentials map[string]string
	v1.ReadWriteOCMRepositoryPluginContract[runtime.Typed]
}

func (t targetRepo) AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (newRes *descriptor.Resource, err error) {
	cacheFileName := filepath.Join(t.cache, strconv.FormatUint(res.ToIdentity().CanonicalHashV1(), 10))
	cacheFileName, err = filepath.Abs(cacheFileName)
	if err != nil {
		return nil, fmt.Errorf("getting absolute path for cache file %q failed: %w", cacheFileName, err)
	}

	if err := filesystem.CopyBlobToOSPath(content, cacheFileName); err != nil {
		return nil, fmt.Errorf("copying blob to cache file %q failed: %w", cacheFileName, err)
	}
	defer func() {
		_ = os.Remove(cacheFileName)
	}()

	v2res, err := descriptor.ConvertToV2Resources(runtime.NewScheme(runtime.WithAllowUnknown()), []descriptor.Resource{*res})
	if err != nil {
		return nil, fmt.Errorf("converting resource to v2 failed: %w", err)
	}

	return t.ReadWriteOCMRepositoryPluginContract.AddLocalResource(ctx, v1.PostLocalResourceRequest[runtime.Typed]{
		Repository: t.spec,
		Name:       component,
		Version:    version,
		ResourceLocation: types.Location{
			LocationType: types.LocationTypeLocalFile,
			Value:        cacheFileName,
		},
		Resource: &v2res[0],
	}, t.credentials)
}

func (t targetRepo) AddComponentVersion(ctx context.Context, desc *descriptor.Descriptor) error {
	v2desc, err := descriptor.ConvertToV2(runtime.NewScheme(runtime.WithAllowUnknown()), desc)
	if err != nil {
		return fmt.Errorf("converting descriptor to v2 failed: %w", err)
	}
	return t.ReadWriteOCMRepositoryPluginContract.AddComponentVersion(ctx, v1.PostComponentVersionRequest[runtime.Typed]{
		Repository: t.spec,
		Descriptor: v2desc,
	}, t.credentials)
}
