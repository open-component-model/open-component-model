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

var (
	gitVersion   = "0.0.0-dev"
	gitCommit    string
	gitTreeState string
	buildDate    = "1970-01-01T00:00:00Z"
)

type Info struct {
	Major      string `json:"major"`
	Minor      string `json:"minor"`
	Patch      string `json:"patch"`
	PreRelease string `json:"prerelease"`
	Meta       string `json:"meta"`
	GitVersion string `json:"gitVersion"`
	GitCommit  string `json:"gitCommit"`
	BuildDate  string `json:"buildDate"`
	GoVersion  string `json:"goVersion"`
	Compiler   string `json:"compiler"`
	Platform   string `json:"platform"`
}

// Get returns the overall codebase version. It's for detecting
// what code a binary was built from.
// These variables typically come from -ldflags settings and in
// their absence fallback to the settings in pkg/version/base.go.
func Get() (Info, error) {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return Info{}, fmt.Errorf("could not read build info")
	}
	v, err := semver.NewVersion(bi.Main.Version)
	if err != nil {
		return Info{}, fmt.Errorf("could not parse version %q: %w", bi.Main.Version, err)
	}

	var gitCommit string
	var buildDate string
	prerelease := v.Prerelease()
	if prerelease != "" {
		buildDate, gitCommit, _ = strings.Cut(prerelease, "-")
	}

	return Info{
		Major:      strconv.FormatUint(v.Major(), 10),
		Minor:      strconv.FormatUint(v.Minor(), 10),
		Patch:      strconv.FormatUint(v.Patch(), 10),
		PreRelease: v.Prerelease(),
		Meta:       strings.TrimPrefix(v.Metadata(), "+"),
		GitVersion: v.String(),
		GitCommit:  gitCommit,
		BuildDate:  buildDate,
		GoVersion:  bi.GoVersion,
		Compiler:   runtime.Compiler,
		Platform:   fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}, nil
}
