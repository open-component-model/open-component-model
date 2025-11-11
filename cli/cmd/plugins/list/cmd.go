package list

import (
	"github.com/spf13/cobra"
)

const (
	FlagRegistry = "registry"
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "list",
		Short:             "List available plugin binaries from a registry.",
		Args:              cobra.ExactArgs(0),
		Long:              ``,
		Example:           `  # List available plugin binaries from a registry.`,
		RunE:              ListPlugins,
		DisableAutoGenTag: true,
	}

	return cmd
}

func ListPlugins(cmd *cobra.Command, _ []string) error {
	return nil
}
