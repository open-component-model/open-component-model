package componentversion

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/spf13/cobra"
	"ocm.software/open-component-model/cli/internal/render/descs"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/enum"
	"ocm.software/open-component-model/cli/internal/reference/compref"
	"ocm.software/open-component-model/cli/internal/render"
	"ocm.software/open-component-model/cli/internal/repository/ocm"
)

const (
	FlagSemverConstraint = "semver-constraint"
	FlagOutput           = "output"
	FlagDisplayMode      = "display-mode"
	FlagConcurrencyLimit = "concurrency-limit"
	FlagLatest           = "latest"
	FlagRecursive        = "recursive"
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:        "component-version {reference}",
		Aliases:    []string{"cv", "component-versions", "cvs", "componentversion", "componentversions", "component", "components", "comp", "comps", "c"},
		SuggestFor: []string{"version", "versions"},
		Short:      "Get component version(s) from an OCM repository",
		Args:       cobra.MatchAll(cobra.ExactArgs(1), ComponentReferenceAsFirstPositional),
		Long: fmt.Sprintf(`Get component version(s) from an OCM repository.

The format of a component reference is:
	[type::]{repository}/[valid-prefix]/{component}[:version]

For valid prefixes {%[1]s|none} are available. If <none> is used, it defaults to %[1]q. This is because by default,
OCM components are stored within a specific sub-repository.

For known types, currently only {%[2]s} are supported, which can be shortened to {%[3]s} respectively for convenience.

If no type is given, the repository path is interpreted based on introspection and heuristics.
`,
			compref.DefaultPrefix,
			strings.Join([]string{ociv1.Type, ctfv1.Type}, "|"),
			strings.Join([]string{ociv1.ShortType, ociv1.ShortType2, ctfv1.ShortType, ctfv1.ShortType2}, "|"),
		),
		Example: strings.TrimSpace(`
Getting a single component version:

get component-version ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0
get cv ./path/to/ctf//ocm.software/ocmcli:0.23.0
get cv ./path/to/ctf/component-descriptors/ocm.software/ocmcli:0.23.0

Listing many component versions:

get component-versions ghcr.io/open-component-model/ocm//ocm.software/ocmcli
get cvs ghcr.io/open-component-model/ocm//ocm.software/ocmcli --output json
get cvs ghcr.io/open-component-model/ocm//ocm.software/ocmcli -oyaml

Specifying types and schemes:

get cv ctf::github.com/locally-checked-out-repo//ocm.software/ocmcli:0.23.0
get cvs oci::http://localhost:8080//ocm.software/ocmcli
`),
		RunE:              GetComponentVersion,
		DisableAutoGenTag: true,
	}

	enum.VarP(cmd.Flags(), FlagOutput, "o", []string{render.OutputFormatTable.String(), render.OutputFormatYAML.String(), render.OutputFormatJSON.String(), render.OutputFormatNDJSON.String(), render.OutputFormatTree.String()}, "output format of the component descriptors")
	enum.VarP(cmd.Flags(), FlagDisplayMode, "", []string{render.StaticRenderMode, render.LiveRenderMode}, `display mode can be used in combination with --recursive
  static: print the output once the complete component graph is discovered
  live (experimental): continuously updates the output to represent the current discovery state of the component graph`)
	cmd.Flags().String(FlagSemverConstraint, "> 0.0.0-0", "semantic version constraint restricting which versions to output")
	// TODO(fabianburth): add concurrency limit to the dag discovery (https://github.com/open-component-model/ocm-project/issues/705)
	// cmd.Flags().Int(FlagConcurrencyLimit, 4, "maximum amount of parallel requests to the repository for resolving component versions")
	cmd.Flags().Bool(FlagLatest, false, "if set, only the latest version of the component is returned")
	cmd.Flags().Int(FlagRecursive, 0, "depth of recursion for resolving referenced component versions (0=none, -1=unlimited, >0=levels (not implemented yet))")
	cmd.Flags().Lookup(FlagRecursive).NoOptDefVal = "-1"

	return cmd
}

func ComponentReferenceAsFirstPositional(_ *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing component reference as first positional argument")
	}
	if _, err := compref.Parse(args[0]); err != nil {
		return fmt.Errorf("parsing component reference from first position argument %q failed: %w", args[0], err)
	}
	return nil
}

func GetComponentVersion(cmd *cobra.Command, args []string) error {
	pluginManager := ocmctx.FromContext(cmd.Context()).PluginManager()
	if pluginManager == nil {
		return fmt.Errorf("could not retrieve plugin manager from context")
	}

	credentialGraph := ocmctx.FromContext(cmd.Context()).CredentialGraph()
	if credentialGraph == nil {
		return fmt.Errorf("could not retrieve credential graph from context")
	}

	output, err := enum.Get(cmd.Flags(), FlagOutput)
	if err != nil {
		return fmt.Errorf("getting output flag failed: %w", err)
	}
	displayMode, err := enum.Get(cmd.Flags(), FlagDisplayMode)
	if err != nil {
		return fmt.Errorf("getting display-mode flag failed: %w", err)
	}
	constraint, err := cmd.Flags().GetString(FlagSemverConstraint)
	if err != nil {
		return fmt.Errorf("getting semver-constraint flag failed: %w", err)
	}
	// TODO(fabianburth): add concurrency limit to the dag discovery (https://github.com/open-component-model/ocm-project/issues/705)
	// concurrencyLimit, err := cmd.Flags().GetInt(FlagConcurrencyLimit)
	// if err != nil {
	//	 return fmt.Errorf("getting concurrency-limit flag failed: %w", err)
	// }
	latestOnly, err := cmd.Flags().GetBool(FlagLatest)
	if err != nil {
		return fmt.Errorf("getting latest flag failed: %w", err)
	}
	recursive, err := cmd.Flags().GetInt(FlagRecursive)
	if err != nil {
		return fmt.Errorf("getting recursive flag failed: %w", err)
	}

	reference := args[0]
	config := ocmctx.FromContext(cmd.Context()).Configuration()

	// we have a reference and parse it
	ref, err := compref.Parse(reference)
	if err != nil {
		return fmt.Errorf("parsing component reference %q failed: %w", reference, err)
	}
	slog.DebugContext(cmd.Context(), "parsed component reference", "reference", reference, "parsed", ref)

	repoProvider, err := ocm.NewComponentVersionRepositoryForComponentProvider(cmd.Context(), pluginManager.ComponentVersionRepositoryRegistry, credentialGraph, config, ref)
	if err != nil {
		return fmt.Errorf("could not initialize ocm repositoryProvider: %w", err)
	}

	repo, err := repoProvider.GetComponentVersionRepositoryForComponent(cmd.Context(), ref.Component, ref.Version)
	if err != nil {
		return fmt.Errorf("could not access ocm repository: %w", err)
	}

	descriptors, err := ocm.GetComponentVersions(cmd.Context(), ocm.GetComponentVersionsOptions{
		VersionOptions: ocm.VersionOptions{
			SemverConstraint: constraint,
			LatestOnly:       latestOnly,
		},
	}, ref.Component, ref.Version, repo)
	if err != nil {
		return fmt.Errorf("getting component reference and versions failed: %w", err)
	}

	roots := make([]string, 0, len(descriptors))
	for _, desc := range descriptors {
		identity := runtime.Identity{
			descruntime.IdentityAttributeName:    desc.Component.Name,
			descruntime.IdentityAttributeVersion: desc.Component.Version,
		}.String()
		roots = append(roots, identity)
	}

	if err := descs.RenderComponents(cmd, repoProvider, roots, output, displayMode, recursive); err != nil {
		return fmt.Errorf("failed to render components recursively: %w", err)
	}
	return nil
}
