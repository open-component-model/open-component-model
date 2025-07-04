package version

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
)

const (
	FlagFormat                = "format"
	FlagFormatShortHand       = "f"
	FlagFormatOCMv1           = "ocmv1"
	FlagFormatGoBuildInfo     = "gobuildinfo"
	FlagFormatGoBuildInfoJSON = "gobuildinfojson"
)

// BuildVersion is an external variable that can be set at build time to override the version.
// It is set to "n/a" by default, indicating that no version has been specified.
// The variable can be adjusted at build time with
//
//	-ldflags "-X ocm.software/open-component-model/cli/cmd/version.BuildVersion=1.2.3"
//
// The build version accepted is interpreted differently depending on the format:
//   - For `ocmv1`, it is expected to be a semantic version (e.g., "1.2.3") and will be split
//     for a json like output
//   - For `gobuildinfo`, it can be any version string, including a full semantic version.
//     If set, it will override the detected module build version from the Go build info.
var BuildVersion = "n/a"

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Retrieve the build version of the OCM CLI",
		Long: `The version command retrieves the build version of the OCM CLI.

The build version can be formatted in different ways depending on the specified format flag.
The default format is 'ocmv1', which outputs the version in a format compatible with OCM v1 specifications,
with slight modifications:

- "gitTreeState" is removed in favor of "meta" field, which contains the git tree state.
- "buildDate" and "gitCommit" are derived from the input version string, and are parsed according to the go module version specification.

When the format is set to 'gobuildinfo', it outputs the Go build information as a string. The format is standardized
and unified across all golang applications.

When the format is set to 'gobuildinfojson', it outputs the Go build information in JSON format.
This is equivalent to gobuildinfo, but in a structured JSON format.

The build info by default is drawn from the go module build information, which is set at build time of the CLI.
When officially built, it is possibly overwritten with the released version of the OCM CLI.

`,
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
				ver, err := GetLegacyFormat(ver)
				if err != nil {
					return err
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(ver)
			case FlagFormatGoBuildInfo:
				str := ver.String()
				_, err = io.Copy(cmd.OutOrStdout(), strings.NewReader(str))
				return err
			case FlagFormatGoBuildInfoJSON:
				return json.NewEncoder(cmd.OutOrStdout()).Encode(ver)
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
