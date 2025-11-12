package get

import (
	"github.com/spf13/cobra"
)

const (
	FlagRegistry = "registry"
	FlagOutput   = "output"
	FlagVersion  = "version"
)

// pluginDirectoryDefault contains all plugins for ocm.
// TODO: Check if const would be better suited as this is used in cmd/download/plugin/cmd.go as well.
// var pluginDirectoryDefault = filepath.Join(os.Getenv("HOME"), ".config", "ocm", "plugins")

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "get <plugin-component-version>",
		Short:             "Download specified plugin binary from a registry.",
		Args:              cobra.ExactArgs(1),
		Long:              ``,
		Example:           `  # Download a plugin binary from a registry.`,
		RunE:              GetPlugin,
		DisableAutoGenTag: true,
	}

	return cmd
}

func GetPlugin(cmd *cobra.Command, args []string) error {
	return nil
}
