package version

import (
	"fmt"
	"io"
	"runtime/debug"
	"text/tabwriter"
	"time"
)

// writeHumanReadable prints a friendly, human-oriented summary of the build
// information. The first line unambiguously identifies the binary as the OCM v2
// CLI, differentiating it from the legacy OCM v1 CLI at a glance.
func writeHumanReadable(w io.Writer, bi *debug.BuildInfo) error {
	info, err := GetLegacyFormat(bi)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(w, "OCM CLI (Open Component Model %s)\n", Generation); err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	row := func(label, value string) {
		if value == "" {
			return
		}
		fmt.Fprintf(tw, "  %s:\t%s\n", label, value)
	}
	row("Version", info.GitVersion)
	row("GitCommit", info.GitCommit)
	row("BuildDate", humanBuildDate(info.BuildDate))
	return tw.Flush()
}

// humanBuildDate turns the compact Go pseudo-version build timestamp
// (YYYYMMDDHHMMSS, UTC) into a human-readable RFC3339 UTC timestamp. Any value
// that is not in that format (e.g. an empty or custom build date) is returned
// unchanged.
func humanBuildDate(raw string) string {
	t, err := time.ParseInLocation("20060102150405", raw, time.UTC)
	if err != nil {
		return raw
	}
	return t.Format(time.RFC3339)
}
