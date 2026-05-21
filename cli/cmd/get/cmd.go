package get

import (
	"github.com/spf13/cobra"

	"ocm.software/open-component-model/cli/cmd/describe/types"
	componentversion "ocm.software/open-component-model/cli/cmd/get/component-version"
	"ocm.software/open-component-model/cli/cmd/get/owner"
)

// New represents any command that is related to retrieving ( "get"ting ) objects
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get {component-version|component-versions|cv|cvs|owner}",
		Short: "Get anything from OCM",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(types.New())
	cmd.AddCommand(componentversion.New())
	cmd.AddCommand(owner.New())
	return cmd
}
