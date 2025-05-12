package get

import (
	"github.com/spf13/cobra"

	"ocm.software/open-component-model/cli/cmd/get/component-version"
)

// Cmd represents any command that is related to retrieving ( "get"ting ) objects
var Cmd = &cobra.Command{
	Use:   "get {component-version|component-versions|cv|cvs}",
	Short: "Get anything from OCM",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	Cmd.AddCommand(componentversion.Cmd)
}
