package component_version

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/transfer"
	graphRuntime "ocm.software/open-component-model/bindings/go/transform/graph/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/enum"
	"ocm.software/open-component-model/cli/internal/render"
	"ocm.software/open-component-model/cli/internal/repository/ocm"
)

const (
	FlagDryRun        = "dry-run"
	FlagOutput        = "output"
	FlagRecursive     = "recursive"
	FlagCopyResources = "copy-resources"
	FlagUploadAs      = "upload-as"
	FlagTransferSpec  = "transfer-spec"

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
  1. CTFGetComponentVersion -> OCIGetComponentVersion
  2. CTFAddComponentVersion -> OCIAddComponentVersion
  3. GetOCIArtifact -> OCIAddLocalResource / AddOCIArtifact
  4. GetHelmChart -> ConvertHelmToOCI -> OCIAddLocalResource / AddOCIArtifact

We support OCI and CTF as well as Helm repositories as transfer sources.
OCI and CTF repositories are supported as transfer targets, while Helm repositories are not supported.
The graph is built accordingly based on the provided references. 
By default, only the component version itself is transferred, but with --copy-resources, all resources are also copied and transformed if necessary.

The graph is validated, and then executed unless --dry-run is set.

Alternatively, --transfer-spec can be used to provide a previously saved TransformationGraphDefinition
from a file (or stdin with "-"), enabling a two-step workflow:
  1. Generate and review the spec: transfer cv --dry-run -o yaml {reference} {target} > spec.yaml
  2. Edit the spec as needed, then execute: transfer cv --transfer-spec spec.yaml

When --transfer-spec is used, positional arguments and graph-building flags
(--recursive, --copy-resources, --upload-as) are not needed.`,
		Example: strings.TrimSpace(`
# Transfer a component version from a CTF archive to an OCI registry
transfer component-version ctf::./my-archive//ocm.software/mycomponent:1.0.0 ghcr.io/my-org/ocm

# Transfer from one OCI registry to another
transfer component-version ghcr.io/source-org/ocm//ocm.software/mycomponent:1.0.0 ghcr.io/target-org/ocm

# Transfer from one OCI to another using localBlobs
transfer component-version ghcr.io/source-org/ocm//ocm.software/mycomponent:1.0.0 ghcr.io/target-org/ocm --copy-resources --upload-as localBlob

# Transfer from one OCI to another using OCI artifacts (default)
transfer component-version ghcr.io/source-org/ocm//ocm.software/mycomponent:1.0.0 ghcr.io/target-org/ocm --copy-resources --upload-as ociArtifact

# Transfer a component version containing Helm charts (access-type: helm/v1) as an OCI artifact
transfer component-version ghcr.io/source-org/ocm//ocm.software/mycomponent:1.0.0 ghcr.io/target-org/ocm --copy-resources --upload-as ociArtifact

# Transfer including all resources (e.g. OCI artifacts)
transfer component-version ctf::./my-archive//ocm.software/mycomponent:1.0.0 ghcr.io/my-org/ocm --copy-resources

# Recursively transfer a component version and all its references
transfer component-version ghcr.io/source-org/ocm//ocm.software/mycomponent:1.0.0 ghcr.io/target-org/ocm -r --copy-resources

# Two-step transfer: first generate a spec, edit it, then execute it
transfer component-version --dry-run -o yaml ghcr.io/source-org/ocm//ocm.software/mycomponent:1.0.0 ghcr.io/target-org/ocm > spec.yaml
# (edit spec.yaml as needed, e.g. change the target registry)
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
	enum.VarP(cmd.Flags(), FlagUploadAs, "u", []string{UploadAsDefault.String(), UploadAsLocalBlob.String(), UploadAsOciArtifact.String()}, "Define whether copied resources should be uploaded as OCI artifacts (instead of local blob resources). This option is only relevant if --copy-resources is set.")
	cmd.Flags().String(FlagTransferSpec, "", "path to a transfer specification file (use \"-\" for stdin)")

	return cmd
}

// transferArgs validates positional arguments based on whether --transfer-spec is set.
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

	octx := ocmctx.FromContext(ctx)

	pm := octx.PluginManager()
	if pm == nil {
		return fmt.Errorf("plugin manager missing in context")
	}

	credGraph := octx.CredentialGraph()
	if credGraph == nil {
		return fmt.Errorf("credentials graph not found in context")
	}

	specPath, err := cmd.Flags().GetString(FlagTransferSpec)
	if err != nil {
		return fmt.Errorf("getting transfer-spec flag failed: %w", err)
	}

	var tgd *transformv1alpha1.TransformationGraphDefinition

	if specPath != "" {
		tgd, err = loadTransferSpec(specPath, cmd.InOrStdin())
		if err != nil {
			return err
		}
	} else {
		tgd, err = buildGraphDefinitionFromArgs(cmd, args, octx, pm, credGraph)
		if err != nil {
			return err
		}
	}

	// Build transformer builder
	b := transfer.NewDefaultBuilder(pm.ComponentVersionRepositoryRegistry, pm.ResourcePluginRegistry, credGraph)
	graph, err := b.
		WithEvents(make(chan graphRuntime.ProgressEvent, eventBufferSize)).
		BuildAndCheck(tgd)
	if err != nil {
		reader, rerr := renderTGD(tgd, output)
		defer func() {
			if reader != nil {
				_ = reader.Close()
			}
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

	tgd := &transformv1alpha1.TransformationGraphDefinition{}
	if err := yaml.Unmarshal(data, tgd); err != nil {
		return nil, fmt.Errorf("parsing transfer spec: %w", err)
	}

	return tgd, nil
}

func buildGraphDefinitionFromArgs(
	cmd *cobra.Command,
	args []string,
	octx *ocmctx.Context,
	pm *manager.PluginManager,
	credGraph credentials.Resolver,
) (*transformv1alpha1.TransformationGraphDefinition, error) {
	ctx := cmd.Context()
	cfg := octx.Configuration()

	if len(args) != 2 {
		return nil, fmt.Errorf("source component reference and target repository spec are required as positional arguments")
	}

	fromSpec, compErr := compref.Parse(args[0])
	if compErr != nil {
		// TODO add support for reference without component version
		return nil, fmt.Errorf("invalid source component reference: %w", compErr)
	}

	repoProvider, err := ocm.NewComponentRepositoryResolver(
		ctx, pm.ComponentVersionRepositoryRegistry, credGraph, ocm.WithConfig(cfg), ocm.WithComponentRef(fromSpec),
	)
	if err != nil {
		return nil, fmt.Errorf("could not initialize ocm repositoryProvider: %w", err)
	}

	toSpec, err := compref.ParseRepository(args[1],
		compref.WithCTFAccessMode(ctfv1.AccessModeReadWrite+"|"+ctfv1.AccessModeCreate),
	)
	if err != nil {
		return nil, fmt.Errorf("invalid target repository spec: %w", err)
	}

	recursive, err := cmd.Flags().GetBool(FlagRecursive)
	if err != nil {
		return nil, fmt.Errorf("getting recursive flag failed: %w", err)
	}

	copyResources, err := cmd.Flags().GetBool(FlagCopyResources)
	if err != nil {
		return nil, fmt.Errorf("getting copy-resources flag failed: %w", err)
	}

	copyMode := transfer.CopyModeLocalBlobResources
	if copyResources {
		copyMode = transfer.CopyModeAllResources
	}

	uploadType, err := enum.Get(cmd.Flags(), FlagUploadAs)
	if err != nil {
		return nil, fmt.Errorf("getting upload-as flag failed: %w", err)
	}

	upTyp := transfer.UploadAsDefault
	switch uploadType {
	case UploadAsLocalBlob.String():
		upTyp = transfer.UploadAsLocalBlob
	case UploadAsOciArtifact.String():
		upTyp = transfer.UploadAsOciArtifact
	}

	// Build TransformationGraphDefinition
	tgd, err := transfer.BuildGraphDefinition(
		ctx,
		transfer.WithTransfer(
			transfer.Component(fromSpec.Component, fromSpec.Version),
			transfer.ToRepositorySpec(toSpec),
			transfer.FromResolver(repoProvider),
		),
		transfer.WithRecursive(recursive),
		transfer.WithCopyMode(copyMode),
		transfer.WithUploadType(upTyp),
	)
	if err != nil {
		return nil, fmt.Errorf("building graph definition failed: %w", err)
	}

	return tgd, nil
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
