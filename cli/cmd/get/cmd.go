package get

import (
	"github.com/spf13/cobra"

	"ocm.software/open-component-model/cli/cmd/describe/types"
	componentversion "ocm.software/open-component-model/cli/cmd/get/component-version"
	config "ocm.software/open-component-model/cli/cmd/get/config"
)

// New represents any command that is related to retrieving ( "get"ting ) objects
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get {component-version|component-versions|cv|cvs|config|cfg}",
		Short: "Get anything from OCM",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(types.New())
	cmd.AddCommand(componentversion.New())
	cmd.AddCommand(config.New())
	return cmd
}
