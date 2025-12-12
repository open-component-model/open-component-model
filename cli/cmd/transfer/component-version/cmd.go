package component_version

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/credentials"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/oci/transformer"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transform/graph/builder"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/enum"
	"ocm.software/open-component-model/cli/internal/reference/compref"
	"ocm.software/open-component-model/cli/internal/render"
)

const (
	FlagDryRun = "dry-run"
	FlagOutput = "output"
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

	toSpec, err := compref.ParseRepository(args[1],
		compref.WithCTFAccessMode(ctfv1.AccessModeReadWrite+"|"+ctfv1.AccessModeCreate),
	)
	if err != nil {
		return fmt.Errorf("invalid target repository spec: %w", err)
	}

	// Build TransformationGraphDefinition
	tgd := buildGraphDefinition(fromSpec.Repository, toSpec, fromSpec.Component, fromSpec.Version)

	// Build transformer builder
	b := graphBuilder(pm, credGraph)

	graph, err := b.BuildAndCheck(tgd)
	if err != nil {
		return fmt.Errorf("graph validation failed: %w", err)
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

	// Execute graph
	if err := graph.Process(ctx); err != nil {
		return fmt.Errorf("graph execution failed: %w", err)
	}

	slog.InfoContext(ctx, "transfer completed successfully", "component", fromSpec.String())
	return nil
}

func buildGraphDefinition(
	from runtime.Typed,
	to runtime.Typed,
	component, version string,
) *transformv1alpha1.TransformationGraphDefinition {
	return &transformv1alpha1.TransformationGraphDefinition{
		Environment: &runtime.Unstructured{
			Data: map[string]interface{}{
				"from": asUnstructured(from).Data,
				"to":   asUnstructured(to).Data,
			},
		},
		Transformations: []transformv1alpha1.GenericTransformation{
			{
				TransformationMeta: meta.TransformationMeta{
					Type: chooseGetType(from),
					ID:   "download",
				},
				Spec: &runtime.Unstructured{Data: map[string]interface{}{
					"repository": asUnstructured(from).Data,
					"component":  component,
					"version":    version,
				}},
			},
			{
				TransformationMeta: meta.TransformationMeta{
					Type: chooseAddType(from),
					ID:   "upload",
				},
				Spec: &runtime.Unstructured{Data: map[string]interface{}{
					"repository": asUnstructured(to).Data,
					"descriptor": "${download.output.descriptor}",
				}},
			},
		},
	}
}

func chooseGetType(repo runtime.Typed) runtime.Type {
	switch repo.(type) {
	case *oci.Repository:
		return ociv1alpha1.OCIGetComponentVersionV1alpha1
	case *ctfv1.Repository:
		return ociv1alpha1.CTFGetComponentVersionV1alpha1
	default:
		panic(fmt.Sprintf("unknown repository type %T", repo))
	}
}

func chooseAddType(repo runtime.Typed) runtime.Type {
	switch repo.(type) {
	case *oci.Repository:
		return ociv1alpha1.OCIAddComponentVersionV1alpha1
	case *ctfv1.Repository:
		return ociv1alpha1.CTFAddComponentVersionV1alpha1
	default:
		panic(fmt.Sprintf("unknown repository type %T", repo))
	}
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

	return builder.NewBuilder(transformerScheme).
		WithTransformer(&ociv1alpha1.OCIGetComponentVersion{}, ociGet).
		WithTransformer(&ociv1alpha1.OCIAddComponentVersion{}, ociAdd).
		WithTransformer(&ociv1alpha1.CTFGetComponentVersion{}, ociGet).
		WithTransformer(&ociv1alpha1.CTFAddComponentVersion{}, ociAdd)
}

func asUnstructured(typed runtime.Typed) *runtime.Unstructured {
	var raw runtime.Raw
	if err := runtime.NewScheme(runtime.WithAllowUnknown()).Convert(typed, &raw); err != nil {
		panic(fmt.Sprintf("cannot convert to raw: %v", err))
	}
	var unstructured runtime.Unstructured
	if err := runtime.NewScheme(runtime.WithAllowUnknown()).Convert(&raw, &unstructured); err != nil {
		panic(fmt.Sprintf("cannot convert to unstructured: %v", err))
	}
	return &unstructured
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
