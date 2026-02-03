package component_version

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/spf13/cobra"
	"ocm.software/open-component-model/bindings/go/oci/transformer"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/credentials"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/transform/graph/builder"
	graphRuntime "ocm.software/open-component-model/bindings/go/transform/graph/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/cli/cmd/transfer/component-version/internal"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/enum"
	"ocm.software/open-component-model/cli/internal/reference/compref"
	"ocm.software/open-component-model/cli/internal/render"
	"ocm.software/open-component-model/cli/internal/repository/ocm"
)

const (
	FlagDryRun    = "dry-run"
	FlagOutput    = "output"
	FlagRecursive = "recursive"

	// Each node emits 2 events (Running + Completed/Failed) and since the renderer consumes
	// them faster than the transfer produces, 16 is enough to avoid blocking with room to grow.
	eventBufferSize = 16
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:        "component-version {reference} {target}",
		Aliases:    []string{"cv", "component-versions", "cvs", "componentversion", "componentversions", "component", "components", "comp", "comps", "c"},
		SuggestFor: []string{"version", "versions"},
		Short:      "Transfer a component version between OCM repositories",
		Long: `Transfer a single component version from a source repository to
a target repository using an internally generated transformation graph.

This command constructs a TransformationGraphDefinition consisting of:
  1. CTFGetComponentVersion / OCIGetComponentVersion
  2. CTFAddComponentVersion / OCIAddComponentVersion

The graph is validated, and then executed unless --dry-run is set.`,
		Args:              cobra.ExactArgs(2),
		RunE:              TransferComponentVersion,
		DisableAutoGenTag: true,
	}

	enum.VarP(cmd.Flags(), FlagOutput, "o", []string{render.OutputFormatYAML.String(), render.OutputFormatJSON.String(), render.OutputFormatNDJSON.String()}, "output format of the component descriptors")
	cmd.Flags().Bool(FlagDryRun, false, "build and validate the graph but do not execute")
	cmd.Flags().BoolP(FlagRecursive, "r", false, "recursively discover and transfer component versions")

	return cmd
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

	octx := ocmctx.FromContext(ctx)

	cfg := octx.Configuration()

	pm := octx.PluginManager()
	if pm == nil {
		return fmt.Errorf("plugin manager missing in context")
	}

	credGraph := octx.CredentialGraph()
	if credGraph == nil {
		return fmt.Errorf("credentials graph not found in context")
	}

	if len(args) != 2 {
		return fmt.Errorf("source component reference and target repository spec are required as positional arguments")
	}

	// We have a reference, check if it is a component reference.
	fromSpec, compErr := compref.Parse(args[0])
	if compErr != nil {
		// TODO add support for reference without component version
		return fmt.Errorf("invalid source component reference: %w", compErr)
	}

	repoProvider, err := ocm.NewComponentRepositoryResolver(
		ctx, pm.ComponentVersionRepositoryRegistry, credGraph, ocm.WithConfig(cfg), ocm.WithComponentRef(fromSpec),
	)
	if err != nil {
		return fmt.Errorf("could not initialize ocm repositoryProvider: %w", err)
	}

	toSpec, err := compref.ParseRepository(args[1],
		compref.WithCTFAccessMode(ctfv1.AccessModeReadWrite+"|"+ctfv1.AccessModeCreate),
	)
	if err != nil {
		return fmt.Errorf("invalid target repository spec: %w", err)
	}

	recursive, err := cmd.Flags().GetBool(FlagRecursive)
	if err != nil {
		return fmt.Errorf("getting recursive flag failed: %w", err)
	}

	// Build TransformationGraphDefinition
	tgd, err := internal.BuildGraphDefinition(ctx, fromSpec, toSpec, repoProvider, recursive)
	if err != nil {
		return fmt.Errorf("building graph definition failed: %w", err)
	}

	// Build transformer builder
	b := graphBuilder(pm, credGraph)

	graph, err := b.
		WithEvents(make(chan graphRuntime.ProgressEvent, eventBufferSize)).
		BuildAndCheck(tgd)
	if err != nil {
		reader, rerr := renderTGD(tgd, output)
		defer func() {
			_ = reader.Close()
		}()
		raw, _ := io.ReadAll(reader)
		return errors.Join(
			err,
			rerr,
			fmt.Errorf("%s", raw),
		)
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

	// Create event channel and tracker
	tracker := newProgressTracker(graph, cmd.OutOrStdout())
	go tracker.Start(ctx) // Start event processing in a separate goroutine

	// Execute graph
	if err := graph.Process(ctx); err != nil {
		tracker.Summary(err)
		return fmt.Errorf("graph execution failed")
	}
	tracker.Summary(nil)

	slog.DebugContext(ctx, "transfer completed successfully", "component", fromSpec.String())
	return nil
}

// TODO: make this a plugin manager integration.
func graphBuilder(pm *manager.PluginManager, credentialProvider credentials.Resolver) *builder.Builder {
	transformerScheme := ociv1alpha1.Scheme

	ociGet := &transformer.GetComponentVersion{
		Scheme:             transformerScheme,
		RepoProvider:       pm.ComponentVersionRepositoryRegistry,
		CredentialProvider: credentialProvider,
	}
	ociAdd := &transformer.AddComponentVersion{
		Scheme:             transformerScheme,
		RepoProvider:       pm.ComponentVersionRepositoryRegistry,
		CredentialProvider: credentialProvider,
	}

	// Resource transformers
	ociGetResource := &transformer.GetLocalResource{
		Scheme:             transformerScheme,
		RepoProvider:       pm.ComponentVersionRepositoryRegistry,
		CredentialProvider: credentialProvider,
	}
	ociAddResource := &transformer.AddLocalResource{
		Scheme:             transformerScheme,
		RepoProvider:       pm.ComponentVersionRepositoryRegistry,
		CredentialProvider: credentialProvider,
	}

	return builder.NewBuilder(transformerScheme).
		WithTransformer(&ociv1alpha1.OCIGetComponentVersion{}, ociGet).
		WithTransformer(&ociv1alpha1.OCIAddComponentVersion{}, ociAdd).
		WithTransformer(&ociv1alpha1.CTFGetComponentVersion{}, ociGet).
		WithTransformer(&ociv1alpha1.CTFAddComponentVersion{}, ociAdd).
		WithTransformer(&ociv1alpha1.OCIGetLocalResource{}, ociGetResource).
		WithTransformer(&ociv1alpha1.OCIAddLocalResource{}, ociAddResource).
		WithTransformer(&ociv1alpha1.CTFGetLocalResource{}, ociGetResource).
		WithTransformer(&ociv1alpha1.CTFAddLocalResource{}, ociAddResource)
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
