package component_version

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/cli/cmd/configuration"
	ocmctx "ocm.software/open-component-model/bindings/go/cli/internal/context"
	"ocm.software/open-component-model/bindings/go/cli/internal/flags/enum"
	"ocm.software/open-component-model/bindings/go/cli/internal/render"
	"ocm.software/open-component-model/bindings/go/cli/internal/render/progress"
	"ocm.software/open-component-model/bindings/go/cli/internal/render/progress/bar"
	"ocm.software/open-component-model/bindings/go/cli/internal/repository/ocm"
	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	versioningspec "ocm.software/open-component-model/bindings/go/configuration/versioning/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	graphPkg "ocm.software/open-component-model/bindings/go/transform/graph"
	graphRuntime "ocm.software/open-component-model/bindings/go/transform/graph/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

const (
	FlagDryRun        = "dry-run"
	FlagOutput        = "output"
	FlagRecursive     = "recursive"
	FlagCopyResources = "copy-resources"
	FlagUploadAs      = "upload-as"
	FlagTransferSpec  = "transfer-spec"
	FlagConstraint    = "constraint"
	FlagLatest        = "latest"
	FlagConcurrency   = "concurrency-limit"

	// Each node emits 2 events (Running + Completed/Failed) and since the tracker consumes
	// them faster than the transfer produces, 16 is enough to avoid blocking with room to grow.
	eventBufferSize = 16
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:        "component-version {reference} {target}",
		Aliases:    []string{"cv", "component-versions", "cvs", "componentversion", "componentversions", "component", "components", "comp", "comps", "c"},
		SuggestFor: []string{"version", "versions"},
		Short:      "Transfer one or more component versions between OCM repositories",
		Long: `Transfer component version(s) from a source repository to
a target repository using an internally generated transformation graph.

When a version is included in the source reference, exactly that version is transferred.
When the version is omitted, all versions of the component are discovered and transferred.
When the source is a repository reference (without a component), every component version
the repository contains is transferred (component listing is currently CTF-only).
Use --constraint to restrict which versions are selected, and --latest to transfer
only the newest matching version. Both apply per component.

OCI, CTF, and Helm repositories are supported as transfer sources.
OCI and CTF repositories are supported as transfer targets, while Helm repositories are not supported.

By default, only the component version itself is transferred. Use --copy-resources to also
copy (and, when needed, transform) the resources it references. --upload-as controls whether
those resources land as OCI artifacts or as local blobs in the target. --recursive walks the
component's references and transfers them too.

Driving defaults from the OCM configuration:
  A transfer.config.ocm.software/v1alpha1 entry inside the central OCM configuration
  (passed via --config) sets defaults for --recursive, --copy-resources, and --upload-as.
  Explicit command-line flags always override the values from the configuration.

Two-step workflow (generate, review, replay):
  --dry-run builds and validates the graph without executing it, and with -o yaml|json prints
  the resulting TransformationGraphDefinition. --transfer-spec then replays a saved definition
  from a file (or stdin with "-"):
    1. Generate the spec:  transfer cv --dry-run -o yaml --copy-resources -r {reference} {target} > spec.yaml
    2. Review/edit spec.yaml, then execute: transfer cv --transfer-spec spec.yaml
  All graph-shaping flags (--recursive, --copy-resources, --upload-as) and any transfer
  configuration entry are baked into the spec during step 1 and are therefore ignored in
  step 2 - the spec is the full graph definition. Only --dry-run, --output, and
  --concurrency-limit remain meaningful when replaying a spec.

How the graph is built:
  Internally the command assembles a TransformationGraphDefinition from these node types,
  selected based on the source/target references:
    1. CTFGetComponentVersion -> OCIGetComponentVersion
    2. CTFAddComponentVersion -> OCIAddComponentVersion
    3. GetOCIArtifact -> OCIAddLocalResource / AddOCIArtifact
    4. GetHelmChart -> ConvertHelmToOCI -> OCIAddLocalResource / AddOCIArtifact`,
		Example: strings.TrimSpace(`
# Transfer a component version from a CTF archive to an OCI registry
transfer component-version ctf::./my-archive//ocm.software/mycomponent:1.0.0 ghcr.io/my-org/ocm

# Transfer from one OCI registry to another
transfer component-version ghcr.io/source-org/ocm//ocm.software/mycomponent:1.0.0 ghcr.io/target-org/ocm

# Transfer all versions of a component (omit version from reference)
transfer component-version ctf::./my-archive//ocm.software/mycomponent ghcr.io/my-org/ocm

# Transfer every component version contained in a CTF archive (repository reference as source)
transfer component-version ./my-archive ghcr.io/my-org/ocm
transfer component-version ctf::./my-archive ghcr.io/my-org/ocm

# Transfer all versions matching a version constraint
transfer component-version ctf::./my-archive//ocm.software/mycomponent ghcr.io/my-org/ocm --constraint ">= 1.0.0, < 2.0.0"

# Transfer only the latest version
transfer component-version ctf::./my-archive//ocm.software/mycomponent ghcr.io/my-org/ocm --latest

# Transfer from one OCI to another using localBlobs (default)
transfer component-version ghcr.io/source-org/ocm//ocm.software/mycomponent:1.0.0 ghcr.io/target-org/ocm --copy-resources --upload-as localBlob

# Transfer from one OCI to another using OCI artifacts
transfer component-version ghcr.io/source-org/ocm//ocm.software/mycomponent:1.0.0 ghcr.io/target-org/ocm --copy-resources --upload-as ociArtifact

# Transfer a component version containing Helm charts (access-type: helm/v1) as an OCI artifact
transfer component-version ghcr.io/source-org/ocm//ocm.software/mycomponent:1.0.0 ghcr.io/target-org/ocm --copy-resources --upload-as ociArtifact

# Transfer including all resources (e.g. OCI artifacts)
transfer component-version ctf::./my-archive//ocm.software/mycomponent:1.0.0 ghcr.io/my-org/ocm --copy-resources

# Recursively transfer a component version and all its references
transfer component-version ghcr.io/source-org/ocm//ocm.software/mycomponent:1.0.0 ghcr.io/target-org/ocm -r --copy-resources

# Drive defaults from the OCM configuration. With --config ./ocmconfig.yaml containing:
#   type: generic.config.ocm.software/v1
#   configurations:
#   - type: transfer.config.ocm.software/v1alpha1
#     recursive: -1
#     copyMode: allResources
#     uploadType: ociArtifact
# the following invocation transfers recursively with all resources copied as OCI artifacts.
# Any explicit flag still overrides the corresponding configuration value.
transfer component-version --config ./ocmconfig.yaml ghcr.io/source-org/ocm//ocm.software/mycomponent:1.0.0 ghcr.io/target-org/ocm

# Two-step transfer: generate a spec with all desired flags, then review and execute
transfer component-version --dry-run -o yaml --copy-resources -r ghcr.io/source-org/ocm//ocm.software/mycomponent:1.0.0 ghcr.io/target-org/ocm > spec.yaml
# (review/edit spec.yaml as needed, e.g. change the target registry)
transfer component-version --transfer-spec spec.yaml
`),
		Args:              transferArgs,
		RunE:              TransferComponentVersion,
		DisableAutoGenTag: true,
	}

	enum.VarP(cmd.Flags(), FlagOutput, "o", []string{render.OutputFormatYAML.String(), render.OutputFormatJSON.String(), render.OutputFormatNDJSON.String()}, "output format of the component descriptors")
	cmd.Flags().Bool(FlagDryRun, false, "build and validate the graph but do not execute")
	cmd.Flags().BoolP(FlagRecursive, "r", false, "recursively discover and transfer component versions")
	cmd.Flags().Bool(FlagCopyResources, false, "copy all resources in the component version")
	uploadAsValues := make([]string, len(transferv1alpha1.AllUploadTypes))
	for i, t := range transferv1alpha1.AllUploadTypes {
		uploadAsValues[i] = string(t)
	}
	enum.VarP(cmd.Flags(), FlagUploadAs, "u", uploadAsValues,
		"Define whether copied resources should be uploaded as OCI artifacts (instead of local blob resources). This option is only relevant if --copy-resources is set.")
	cmd.Flags().String(FlagTransferSpec, "", "path to a transfer specification file (use \"-\" for stdin). The input must hold exactly one transfer spec document; with \"-\", OCM configuration documents in stdin are applied as configuration")
	cmd.Flags().String(FlagConstraint, "", "version constraint evaluated by each version's configured scheme; versions with no applicable scheme are retained (e.g. \">= 1.0.0, < 2.0.0\"); only used when no version is specified in the reference")
	cmd.Flags().Bool(FlagLatest, false, "if set, only the latest version of the component is transferred; only used when no version is specified in the reference")
	cmd.Flags().Int(FlagConcurrency, 4, "maximum number of transformation nodes processed in parallel; independent nodes run concurrently while dependency ordering is preserved. Increase it to speed up large graphs, decrease it to reduce load on the registry")

	return cmd
}

func transferArgs(cmd *cobra.Command, args []string) error {
	specPath, err := cmd.Flags().GetString(FlagTransferSpec)
	if err != nil {
		return fmt.Errorf("getting transfer-spec flag failed: %w", err)
	}

	if specPath != "" {
		if len(args) > 0 {
			return fmt.Errorf("positional arguments are not allowed when --%s is set", FlagTransferSpec)
		}
		ignoredFlags := []string{FlagRecursive, FlagCopyResources, FlagUploadAs}
		for _, name := range ignoredFlags {
			if cmd.Flags().Changed(name) {
				slog.Warn(fmt.Sprintf("--%s has no effect when --%s is set", name, FlagTransferSpec))
			}
		}
		return nil
	}
	return cobra.ExactArgs(2)(cmd, args)
}

func TransferComponentVersion(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	dryRun, err := cmd.Flags().GetBool(FlagDryRun)
	if err != nil {
		return fmt.Errorf("getting dry-run flag failed: %w", err)
	}

	output, err := enum.Get(cmd.Flags(), FlagOutput)
	if err != nil {
		return fmt.Errorf("getting output flag failed: %w", err)
	}

	concurrency, err := cmd.Flags().GetInt(FlagConcurrency)
	if err != nil {
		return fmt.Errorf("getting concurrency-limit flag failed: %w", err)
	}

	octx := ocmctx.FromContext(ctx)

	pm := octx.PluginManager()
	if pm == nil {
		return fmt.Errorf("plugin manager missing in context")
	}

	credGraph := octx.CredentialGraph()
	if credGraph == nil {
		return fmt.Errorf("credentials graph not found in context")
	}

	httpConfig, err := httpv1alpha1.ResolveHTTPConfig(octx.Configuration())
	if err != nil {
		return fmt.Errorf("could not get http configuration: %w", err)
	}

	specPath, err := cmd.Flags().GetString(FlagTransferSpec)
	if err != nil {
		return fmt.Errorf("getting transfer-spec flag failed: %w", err)
	}

	// Progress goes to stderr so dry-run spec output on stdout remains clean for piping.
	tracker := progress.NewTracker(ctx, cmd.ErrOrStderr(), bar.NewVisualizer[*graphPkg.Transformation])
	defer tracker.Stop()

	var tgd *transformv1alpha1.TransformationGraphDefinition

	if specPath != "" {
		op := tracker.StartOperation("Loading transfer spec")
		tgd, err = loadTransferSpec(specPath, cmd.InOrStdin())
		op.Finish(err)
		if err != nil {
			return err
		}
	} else {
		opName := "Resolving component versions"
		if dryRun {
			opName += " (dry run)"
		}
		// Graph construction discovers component references recursively, so the number
		// of resolutions is not known up front: track them as an indeterminate log
		// instead of a progress bar.
		resolutionEvents := make(chan resolutionEvent, eventBufferSize)
		op := tracker.StartOperation(opName,
			progress.WithEvents(resolutionEvents, mapResolutionEvent, progress.IndeterminateTotal))
		tgd, err = buildGraphDefinitionFromArgs(cmd, args, octx, pm, credGraph, resolutionEvents)
		close(resolutionEvents)
		op.Finish(err)
		if err != nil {
			return err
		}
	}

	// Build transformation graph
	b := transfer.NewDefaultBuilder(
		pm.ComponentVersionRepositoryRegistry,
		pm.ResourcePluginRegistry,
		credGraph,
		transfer.WithHTTPConfig(httpConfig),
	)
	// BuildAndCheck wires and checks every transformation node; the node count
	// is known from the TGD, so track it as a determinate operation.
	buildEvents := make(chan graphRuntime.ProgressEvent, eventBufferSize)
	buildOp := tracker.StartOperation("Building transformation graph",
		progress.WithEvents(buildEvents, mapEvent, len(tgd.Transformations)),
		progress.WithErrorFormatter(formatError))
	graph, err := b.
		WithConcurrency(concurrency).
		WithEvents(make(chan graphRuntime.ProgressEvent, eventBufferSize)).
		WithBuildEvents(buildEvents).
		BuildAndCheck(tgd)
	buildOp.Finish(err)
	if err != nil {
		reader, rerr := renderTGD(tgd, output)
		if rerr != nil {
			return errors.Join(err, rerr)
		}
		defer func() {
			if reader != nil {
				_ = reader.Close()
			}
		}()
		raw, readErr := io.ReadAll(reader)
		if readErr != nil {
			return errors.Join(err, readErr)
		}
		if len(raw) == 0 {
			return err
		}
		return errors.Join(err, fmt.Errorf("%s", raw))
	}

	if dryRun {
		reader, err := renderTGD(tgd, output)
		if err != nil {
			return fmt.Errorf("rendering transformation graph failed: %w", err)
		}
		defer func() {
			if err := reader.Close(); err != nil {
				slog.WarnContext(ctx, "closing transformation graph reader failed", "error", err)
			}
		}()
		if _, err := io.Copy(cmd.OutOrStdout(), reader); err != nil {
			return fmt.Errorf("writing transformation graph failed: %w", err)
		}
		return nil
	}

	// Execute graph with progress tracking
	op := tracker.StartOperation("Transferring component versions",
		progress.WithEvents(graph.Events(), mapEvent, graph.NodeCount()),
		progress.WithConcurrency[*graphPkg.Transformation](concurrency),
		progress.WithErrorFormatter(formatError))

	if err := graph.Process(ctx); err != nil {
		op.Finish(err)
		return fmt.Errorf("graph execution failed: %w", err)
	}
	op.Finish(nil)

	tracker.Stop() // Restore slog before the log below; defer is the safety net for error paths.
	slog.DebugContext(ctx, "transfer completed successfully")
	return nil
}

// loadTransferSpec reads a TransformationGraphDefinition from a file path or stdin (when path is "-").
func loadTransferSpec(path string, stdin io.Reader) (*transformv1alpha1.TransformationGraphDefinition, error) {
	var data []byte
	var err error

	if path == "-" {
		data, err = io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("reading transfer spec from stdin: %w", err)
		}
	} else {
		data, err = os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading transfer spec file %q: %w", path, err)
		}
	}

	spec, err := transferSpecDocument(data)
	if err != nil {
		return nil, fmt.Errorf("parsing transfer spec: %w", err)
	}

	tgd := &transformv1alpha1.TransformationGraphDefinition{}
	if err := yaml.Unmarshal(spec, tgd); err != nil {
		return nil, fmt.Errorf("parsing transfer spec: %w", err)
	}

	return tgd, nil
}

// transferSpecDocument returns the only document of a transfer spec. OCM configuration
// piped with --transfer-spec - is taken out of stdin before the command runs, so any
// configuration left here came from a spec file, where it would not be applied.
// A plain yaml.Unmarshal would silently take the first document and run an empty graph.
func transferSpecDocument(data []byte) ([]byte, error) {
	configs, specs, err := configuration.SplitConfigStream(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if len(configs) > 0 {
		return nil, errors.New("OCM configuration is not allowed in a transfer spec file, pass it with --config or on stdin")
	}
	switch len(specs) {
	case 0:
		return nil, errors.New("no transfer spec document found")
	case 1:
		return specs[0], nil
	default:
		return nil, fmt.Errorf("expected exactly one transfer spec document, found %d", len(specs))
	}
}

func buildGraphDefinitionFromArgs(
	cmd *cobra.Command,
	args []string,
	octx *ocmctx.Context,
	pm *manager.PluginManager,
	credGraph credentials.Resolver,
	resolutionEvents chan<- resolutionEvent,
) (*transformv1alpha1.TransformationGraphDefinition, error) {
	ctx := cmd.Context()
	cfg := octx.Configuration()

	if len(args) != 2 {
		return nil, fmt.Errorf("source component reference and target repository spec are required as positional arguments")
	}

	fromSpec, repoProvider, sourceComponents, err := resolveSource(ctx, args[0], pm, credGraph, cfg, resolutionEvents)
	if err != nil {
		return nil, err
	}

	toSpec, err := compref.ParseRepository(args[1],
		compref.WithCTFAccessMode(ctfv1.AccessModeReadWrite+"|"+ctfv1.AccessModeCreate),
	)
	if err != nil {
		return nil, fmt.Errorf("invalid target repository spec: %w", err)
	}

	transferCfg, err := transferv1alpha1.LookupConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("looking up transfer config failed: %w", err)
	}
	if transferCfg == nil {
		// LookupConfig returns nil when the central config has no transfer entry; start
		// from a zero value so the override branches below can write unconditionally.
		transferCfg = &transferv1alpha1.Config{}
	}

	uploaderCfgs, err := transferv1alpha1.LookupUploaderConfigs(cfg)
	if err != nil {
		return nil, fmt.Errorf("looking up uploader configs failed: %w", err)
	}

	if cmd.Flags().Changed(FlagRecursive) {
		recursive, err := cmd.Flags().GetBool(FlagRecursive)
		if err != nil {
			return nil, fmt.Errorf("getting recursive flag failed: %w", err)
		}
		if recursive {
			transferCfg.Recursive = transferv1alpha1.RecursiveInfinite
		} else {
			transferCfg.Recursive = transferv1alpha1.RecursiveNone
		}
	}
	if cmd.Flags().Changed(FlagCopyResources) {
		copyResources, err := cmd.Flags().GetBool(FlagCopyResources)
		if err != nil {
			return nil, fmt.Errorf("getting copy-resources flag failed: %w", err)
		}
		if copyResources {
			transferCfg.CopyMode = transferv1alpha1.CopyModeAllResources
		} else {
			transferCfg.CopyMode = transferv1alpha1.CopyModeLocalBlobResources
		}
	}
	if cmd.Flags().Changed(FlagUploadAs) {
		uploadAs, err := enum.Get(cmd.Flags(), FlagUploadAs)
		if err != nil {
			return nil, fmt.Errorf("getting upload-as flag failed: %w", err)
		}
		transferCfg.UploadType = transferv1alpha1.UploadType(uploadAs)
	}

	componentIDs, err := collectComponentIDs(ctx, cmd, cfg, repoProvider, fromSpec, sourceComponents)
	if err != nil {
		return nil, err
	}

	tgd, err := transfer.BuildGraphDefinition(ctx, transferCfg, uploaderCfgs,
		transfer.Mapping{
			Components: componentIDs,
			Target:     toSpec,
			Resolver:   repoProvider,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("building graph definition failed: %w", err)
	}

	return tgd, nil
}

// resolveSource parses the source as a component reference or, as a fallback, a
// repository reference. A repository reference transfers every component version it
// contains. The returned sourceComponents is nil for a component reference and holds
// the discovered component names otherwise.
func resolveSource(
	ctx context.Context,
	source string,
	pm *manager.PluginManager,
	credGraph credentials.Resolver,
	cfg *genericv1.Config,
	resolutionEvents chan<- resolutionEvent,
) (fromSpec *compref.Ref, repoProvider resolvers.ComponentVersionRepositoryResolver, sourceComponents []string, err error) {
	fromSpec, compErr := compref.Parse(source, compref.IgnoreSemverCompatibility())

	var sourceRepository runtime.Typed
	if compErr != nil {
		repo, repoErr := compref.ParseRepository(source)
		if repoErr != nil {
			return nil, nil, nil, fmt.Errorf("invalid source reference: must be either a component reference or a repository reference: %w", errors.Join(compErr, repoErr))
		}
		sourceRepository = repo
	}

	resolverOpts := []ocm.RepositoryResolverOption{ocm.WithConfig(cfg)}
	if sourceRepository != nil {
		// Component names must be discovered before the resolver is built so they can
		// be registered as high-priority patterns for the source repository.
		if sourceComponents, err = listComponentsFromRepository(ctx, pm, sourceRepository); err != nil {
			return nil, nil, nil, fmt.Errorf("could not list components in source repository: %w", err)
		}
		if len(sourceComponents) == 0 {
			return nil, nil, nil, fmt.Errorf("no components found in source repository")
		}
		resolverOpts = append(resolverOpts, ocm.WithRepository(sourceRepository), ocm.WithComponentPatterns(sourceComponents))
	} else {
		resolverOpts = append(resolverOpts, ocm.WithComponentRef(fromSpec))
	}

	resolver, err := ocm.NewComponentRepositoryResolver(ctx, pm.ComponentVersionRepositoryRegistry, credGraph, resolverOpts...)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("could not initialize ocm repositoryProvider: %w", err)
	}

	return fromSpec, &resolutionProgressResolver{ComponentVersionRepositoryResolver: resolver, events: resolutionEvents}, sourceComponents, nil
}

// collectComponentIDs determines which component versions to transfer. For a
// component reference with an explicit version, exactly that version is used.
// Otherwise versions are listed and filtered (by --constraint and --latest) for the
// single referenced component, or for every component of a repository reference.
func collectComponentIDs(
	ctx context.Context,
	cmd *cobra.Command,
	cfg *genericv1.Config,
	repoProvider resolvers.ComponentVersionRepositoryResolver,
	fromSpec *compref.Ref,
	sourceComponents []string,
) ([]transfer.ComponentID, error) {
	constraint, err := cmd.Flags().GetString(FlagConstraint)
	if err != nil {
		return nil, fmt.Errorf("getting constraint flag failed: %w", err)
	}
	latestOnly, err := cmd.Flags().GetBool(FlagLatest)
	if err != nil {
		return nil, fmt.Errorf("getting latest flag failed: %w", err)
	}

	repositorySource := sourceComponents != nil

	if !repositorySource && fromSpec.Version != "" {
		if cmd.Flags().Changed(FlagConstraint) {
			slog.WarnContext(ctx, fmt.Sprintf("--%s has no effect when a version is already specified in the reference", FlagConstraint))
		}
		if cmd.Flags().Changed(FlagLatest) {
			slog.WarnContext(ctx, fmt.Sprintf("--%s has no effect when a version is already specified in the reference", FlagLatest))
		}
		return []transfer.ComponentID{{Component: fromSpec.Component, Version: fromSpec.Version}}, nil
	}

	registry, err := versioningspec.RegistryFromConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("could not build versioning registry: %w", err)
	}

	components := sourceComponents
	if !repositorySource {
		components = []string{fromSpec.Component}
	}

	var componentIDs []transfer.ComponentID
	for _, component := range components {
		repo, err := repoProvider.GetComponentVersionRepositoryForComponent(ctx, component, "")
		if err != nil {
			return nil, fmt.Errorf("could not access ocm repository: %w", err)
		}
		versions, err := ocm.VersionsWithFiltering(ctx, component, repo, ocm.VersionOptions{
			SemverConstraint: constraint,
			LatestOnly:       latestOnly,
			Registry:         registry,
		})
		if err != nil {
			return nil, fmt.Errorf("listing and filtering component versions failed: %w", err)
		}
		for _, v := range versions {
			componentIDs = append(componentIDs, transfer.ComponentID{Component: component, Version: v})
		}
	}
	if len(componentIDs) == 0 {
		msg := "no versions found"
		if !repositorySource {
			msg = fmt.Sprintf("no versions found for component %q", fromSpec.Component)
		}
		if constraint != "" {
			msg += fmt.Sprintf(" matching constraint %q", constraint)
		}
		if latestOnly {
			msg += " (latest only)"
		}
		return nil, errors.New(msg)
	}

	return componentIDs, nil
}

// listComponentsFromRepository lists the component names contained in a repository.
// Component listing currently supports CTF repositories only, which covers the common
// "transfer a whole transport archive" case.
func listComponentsFromRepository(ctx context.Context, pm *manager.PluginManager, repository runtime.Typed) ([]string, error) {
	if _, ok := repository.(*ctfv1.Repository); !ok {
		return nil, fmt.Errorf("component listing in repositories of type %T is not supported; specify a component in the source reference", repository)
	}

	lister, err := pm.ComponentListerRegistry.GetComponentLister(ctx, repository, nil)
	if err != nil {
		return nil, fmt.Errorf("could not get component lister: %w", err)
	}

	var componentNames []string
	if err := lister.ListComponents(ctx, "", func(names []string) error {
		componentNames = append(componentNames, names...)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("could not list components: %w", err)
	}

	return componentNames, nil
}

func renderTGD(tgd *transformv1alpha1.TransformationGraphDefinition, format string) (io.ReadCloser, error) {
	switch format {
	case render.OutputFormatJSON.String():
		read, write := io.Pipe()
		encoder := json.NewEncoder(write)
		encoder.SetIndent("", "  ")
		go func() {
			err := encoder.Encode(tgd)
			_ = write.CloseWithError(err)
		}()
		return read, nil
	case render.OutputFormatNDJSON.String():
		read, write := io.Pipe()
		encoder := json.NewEncoder(write)
		go func() {
			err := encoder.Encode(tgd)
			_ = write.CloseWithError(err)
		}()
		return read, nil
	case render.OutputFormatYAML.String():
		data, err := yaml.Marshal(tgd)
		if err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(data)), nil
	default:
		return nil, fmt.Errorf("invalid output format %q", format)
	}
}
