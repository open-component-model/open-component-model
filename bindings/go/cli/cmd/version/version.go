package version

import (
	"encoding/json"
	"fmt"
	"io"
	"runtime/debug"
	"strings"

	_ "embed"

	"github.com/spf13/cobra"

	"ocm.software/open-component-model/bindings/go/cli/cmd/configuration"
	"ocm.software/open-component-model/bindings/go/cli/internal/flags/enum"
)

const (
	FlagOutput          = "output"
	FlagOutputShortHand = "o"

	OutputText            = "text"
	OutputOCMv1           = "legacyjson"
	OutputGoBuildInfo     = "gobuildinfo"
	OutputGoBuildInfoJSON = "gobuildinfojson"
)

// Generation identifies the OCM specification generation this CLI implements.
// It is what distinguishes this binary from the legacy OCM v1 CLI in the
// human-readable output.
const Generation = "v2"

// BuildVersion is an external variable that can be set at build time to override the version.
// It is set to "n/a" by default, indicating that no version has been specified.
// The variable can be adjusted at build time with
//
//	-ldflags "-X ocm.software/open-component-model/bindings/go/cli/cmd/version.BuildVersion=1.2.3"
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

		// Never uses configuration, so piped stdin must not be read.
		Annotations: map[string]string{configuration.SkipStdinConfigAnnotation: ""},

		Long: fmt.Sprintf(`The version command retrieves the build version of the OCM CLI.

The build version can be formatted in different ways depending on the specified %[1]s flag.
The default format is %[2]q, which prints a human-readable summary that clearly identifies
this binary as the OCM %[6]s CLI, together with the version, commit and build date.

When the format is set to %[3]q, it outputs the version in a format compatible with OCM v1
specifications, with slight modifications:

- "gitTreeState" is removed in favor of "meta" field, which contains the git tree state.
- "buildDate" and "gitCommit" are derived from the input version string, and are parsed according to the go module version specification.

When the format is set to %[4]q, it outputs the Go build information as a string. The format is standardized
and unified across all golang applications.

When the format is set to %[5]q, it outputs the Go build information in JSON format.
This is equivalent to %[4]q, but in a structured JSON format.

The build info by default is drawn from the go module build information, which is set at build time of the CLI.
When officially built, it is possibly overwritten with the released version of the OCM CLI.`, FlagOutput, OutputText, OutputOCMv1, OutputGoBuildInfo, OutputGoBuildInfoJSON, Generation),
		Example: fmt.Sprintf(`ocm version --%s %s`, FlagOutput, OutputText),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := enum.Get(cmd.Flags(), FlagOutput)
			if err != nil {
				return err
			}
			return Write(cmd.OutOrStdout(), format)
		},
		DisableAutoGenTag: true,
		SilenceUsage:      true,
	}

	enum.VarP(cmd.Flags(), FlagOutput, FlagOutputShortHand, []string{OutputText, OutputOCMv1, OutputGoBuildInfo, OutputGoBuildInfoJSON}, "output format of the version information")
	return cmd
}

// Write renders the CLI build information to w using the given format.
// Supported formats are OutputText (human-readable, default), OutputOCMv1,
// OutputGoBuildInfo and OutputGoBuildInfoJSON. An unknown format returns an error.
func Write(w io.Writer, format string) error {
	ver, ok := debug.ReadBuildInfo()
	if !ok {
		return fmt.Errorf("no build info available")
	}
	if BuildVersion != "n/a" {
		// Override the version if specified.
		ver.Main.Version = BuildVersion
	}
	switch format {
	case OutputText:
		return writeHumanReadable(w, ver)
	case OutputOCMv1:
		legacy, err := GetLegacyFormat(ver)
		if err != nil {
			return err
		}
		return json.NewEncoder(w).Encode(legacy)
	case OutputGoBuildInfo:
		_, err := io.Copy(w, strings.NewReader(ver.String()))
		return err
	case OutputGoBuildInfoJSON:
		return json.NewEncoder(w).Encode(ver)
	default:
		return fmt.Errorf("unknown version format %q", format)
	}
}
