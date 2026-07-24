package componentversion

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"ocm.software/open-component-model/bindings/go/oci/compref"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/repository"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/repository/ocm"
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:        "component-version {reference}",
		Aliases:    []string{"cv", "componentversion", "component", "comp", "c"},
		SuggestFor: []string{"version", "versions"},
		Short:      "Delete a component version from an OCM repository",
		Args:       cobra.MatchAll(cobra.ExactArgs(1), componentReferenceAsFirstPositional),
		Long: fmt.Sprintf(`Delete a component version from an OCM repository.

The format of a component reference is:
	[type::]{repository}/[valid-prefix]/{component}:version

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
Deleting a component version:

delete component-version ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0
delete cv ./path/to/ctf//ocm.software/ocmcli:0.23.0
`),
		RunE:              DeleteComponentVersion,
		DisableAutoGenTag: true,
	}

	return cmd
}

func componentReferenceAsFirstPositional(_ *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing component reference as first positional argument")
	}
	var compErr error
	if _, compErr = compref.Parse(args[0]); compErr == nil {
		return nil
	}
	return fmt.Errorf("first position argument %q must be a valid component reference: %w", args[0], compErr)
}

func DeleteComponentVersion(cmd *cobra.Command, args []string) error {
	pluginManager := ocmctx.FromContext(cmd.Context()).PluginManager()
	if pluginManager == nil {
		return fmt.Errorf("could not retrieve plugin manager from context")
	}

	credentialGraph := ocmctx.FromContext(cmd.Context()).CredentialGraph()
	if credentialGraph == nil {
		return fmt.Errorf("could not retrieve credential graph from context")
	}

	config := ocmctx.FromContext(cmd.Context()).Configuration()
	reference := args[0]

	ref, compErr := compref.Parse(reference, []compref.Option{
		compref.IgnoreSemverCompatibility(),
	}...)
	if compErr != nil {
		return fmt.Errorf("failed to parse component reference %q: %w", reference, compErr)
	}

	if ref.Version == "" {
		return fmt.Errorf("component version must be specified for deletion")
	}

	if ctfRepo, ok := ref.Repository.(*ctfv1.Repository); ok {
		ctfRepo.AccessMode = ctfv1.AccessModeReadWrite
	}

	ctx := cmd.Context()
	repoProvider, err := ocm.NewComponentVersionRepositoryForComponentProvider(ctx, pluginManager.ComponentVersionRepositoryRegistry, credentialGraph, config, ref)
	if err != nil {
		return fmt.Errorf("could not initialize ocm repositoryProvider: %w", err)
	}

	repo, err := repoProvider.GetComponentVersionRepositoryForComponent(ctx, ref.Component, ref.Version)
	if err != nil {
		return fmt.Errorf("could not access ocm repository: %w", err)
	}

	deleter, ok := repo.(repository.ComponentVersionDeleter)
	if !ok {
		return fmt.Errorf("repository does not support component version deletion")
	}

	if err := deleter.DeleteComponentVersion(ctx, ref.Component, ref.Version); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return fmt.Errorf("component version %s:%s not found in repository", ref.Component, ref.Version)
		}
		return fmt.Errorf("failed to delete component version: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Successfully deleted component version %s:%s\n", ref.Component, ref.Version)
	return nil
}
