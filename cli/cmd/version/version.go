package version

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"

	"ocm.software/open-component-model/cli/internal/version"
)

const (
	FlagFormat            = "format"
	FlagFormatShortHand   = "f"
	FlagFormatOCMv1       = "ocmv1"
	FlagFormatGoBuildInfo = "gobuildinfo"
)

var BuildVersion = "n/a"

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Retrieve the version of the OCM CLI",
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := cmd.Flags().GetString(FlagFormat)
			if err != nil {
				return err
			}
			ver, ok := debug.ReadBuildInfo()
			if !ok {
				return fmt.Errorf("no build info available")
			}
			if BuildVersion != "n/a" {
				// Override the version if specified
				ver.Main.Version = BuildVersion
			}
			switch format {
			case FlagFormatOCMv1:
				ver, err := version.GetLegacyFormat(ver)
				if err != nil {
					return err
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(ver)
			case FlagFormatGoBuildInfo:
				str := ver.String()
				_, err = io.Copy(cmd.OutOrStdout(), strings.NewReader(str))
				return err
			default:
				return cmd.Help()
			}
		},
		DisableAutoGenTag: true,
		SilenceUsage:      true,
	}

	cmd.Flags().StringP(FlagFormat, FlagFormatShortHand, FlagFormatOCMv1, "Format of the generated documentation (default: ocmv1)")
	return cmd
}
