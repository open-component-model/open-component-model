package version

import (
	"encoding/json"

	"github.com/spf13/cobra"

	"ocm.software/open-component-model/cli/cmd/configuration"
	"ocm.software/open-component-model/cli/cmd/generate"
	"ocm.software/open-component-model/cli/cmd/get"
	"ocm.software/open-component-model/cli/internal/flags/log"
	"ocm.software/open-component-model/cli/internal/version"
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Retrieve the version of the OCM CLI",
		RunE: func(cmd *cobra.Command, args []string) error {
			ver, err := version.Get()
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(ver)
		},
		DisableAutoGenTag: true,
		SilenceUsage:      true,
	}

	configuration.RegisterConfigFlag(cmd)
	log.RegisterLoggingFlags(cmd.PersistentFlags())
	cmd.AddCommand(generate.New())
	cmd.AddCommand(get.New())
	return cmd
}
