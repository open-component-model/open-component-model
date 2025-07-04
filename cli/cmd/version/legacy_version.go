package version

import (
	_ "embed"
	"fmt"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/Masterminds/semver/v3"
)

type LegacyVersionInfo struct {
	Major      string `json:"major"`
	Minor      string `json:"minor"`
	Patch      string `json:"patch"`
	PreRelease string `json:"prerelease,omitempty"`
	Meta       string `json:"meta,omitempty"`
	GitVersion string `json:"gitVersion"`
	GitCommit  string `json:"gitCommit,omitempty"`
	BuildDate  string `json:"buildDate,omitempty"`
	GoVersion  string `json:"goVersion"`
	Compiler   string `json:"compiler"`
	Platform   string `json:"platform"`
}

// GetLegacyFormat returns the build information in a legacy format compatible with OCM v1 CLI specifications.
func GetLegacyFormat(bi *debug.BuildInfo) (LegacyVersionInfo, error) {
	base := LegacyVersionInfo{
		GoVersion: runtime.Version(),
		Compiler:  runtime.Compiler,
		Platform:  fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}

	v, err := semver.NewVersion(bi.Main.Version)
	if err != nil {
		// If the version is not a valid semantic version, we still want to return it as a regular string.
		base.GitVersion = bi.Main.Version
		base.Major, base.Minor, base.Patch = "0", "0", "0"
	} else {
		// If the version is a valid semantic version, we extract the components.
		base.GitVersion = v.String()
		if v.Metadata() != "" {
			base.Meta = strings.TrimPrefix(v.Metadata(), "+")
		}
		if v.Prerelease() != "" {
			base.PreRelease = v.Prerelease()
			if base.PreRelease != "" {
				base.BuildDate, base.GitCommit, _ = strings.Cut(base.PreRelease, "-")
			}
		}
		base.Major = strconv.FormatUint(v.Major(), 10)
		base.Minor = strconv.FormatUint(v.Minor(), 10)
		base.Patch = strconv.FormatUint(v.Patch(), 10)
	}

	return base, nil
}
