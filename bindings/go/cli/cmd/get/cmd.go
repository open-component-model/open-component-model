package get

import (
	"github.com/spf13/cobra"

	"ocm.software/open-component-model/bindings/go/cli/cmd/describe/types"
	attestation "ocm.software/open-component-model/bindings/go/cli/cmd/get/attestation"
	componentversion "ocm.software/open-component-model/bindings/go/cli/cmd/get/component-version"
	"ocm.software/open-component-model/bindings/go/cli/cmd/get/config"
)

// New represents any command that is related to retrieving ( "get"ting ) objects
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get {component-version|component-versions|cv|cvs|config|cfg|attestation|attestations}",
		Short: "Get anything from OCM",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(types.New())
	cmd.AddCommand(componentversion.New())
	cmd.AddCommand(config.New())
	cmd.AddCommand(attestation.New())
	return cmd
}
