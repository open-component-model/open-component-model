package verify

import (
	"github.com/spf13/cobra"

	componentversion "ocm.software/open-component-model/cli/cmd/verify/component-version"
)

// New represents any command that is related to verifying objects
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify {component-version|component-versions|cv|cvs}",
		Short: "verify digests and signatures of component versions in OCM",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(componentversion.New())
	return cmd
}
