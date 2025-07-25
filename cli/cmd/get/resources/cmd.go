package resources

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/enum"
	"ocm.software/open-component-model/cli/internal/reference/compref"
	"ocm.software/open-component-model/cli/internal/repository/ocm"
)

const (
	FlagOutput = "output"
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "resources {reference}",
		Aliases: []string{"resource", "r", "res"},
		Short:   "Get resource description(s) from an OCM component version",
		Args:    cobra.MatchAll(cobra.ExactArgs(1), ComponentReferenceAsFirstPositional),
		Long:    fmt.Sprintf(`Get resource description(s) from an OCM component version.`),
		Example: strings.TrimSpace(`
Getting a single component version:

get resources ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0
`),
		RunE:              GetResources,
		DisableAutoGenTag: true,
	}

	enum.VarP(cmd.Flags(), FlagOutput, "o", []string{"tree", "treewide"}, "output format of the resource descriptions")

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

func GetResources(cmd *cobra.Command, args []string) error {
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

	reference := args[0]
	repo, err := ocm.NewFromRef(cmd.Context(), pluginManager, credentialGraph, reference)
	if err != nil {
		return fmt.Errorf("could not initialize ocm repository: %w", err)
	}

	desc, err := repo.GetComponentVersion(cmd.Context())
	if err != nil {
		return fmt.Errorf("getting component reference and versions failed: %w", err)
	}

	data, size, err := encodeResources(output, desc)
	if err != nil {
		return fmt.Errorf("generating output failed: %w", err)
	}

	if _, err := io.CopyN(cmd.OutOrStdout(), data, size); err != nil {
		return fmt.Errorf("writing component version descriptor failed: %w", err)
	}

	return nil
}
