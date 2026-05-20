package setup

import (
	"testing"

	"github.com/spf13/cobra"
)

func Test_isVersionCheckDisabled_devVersions(t *testing.T) {
	tests := []struct {
		version string
		skip    bool
	}{
		{"", true},
		{"n/a", true},
		// pseudo-versions produced by go install from untagged commit
		{"0.0.0-20260520062203-565e352c12c5", true},
		{"v0.0.0-20260520062203-565e352c12c5", true},
		{"0.0.0-dev", true},
		// intentional pre-releases — must NOT skip
		{"0.6.0-rc.1", false},
		{"v0.6.0-rc.1", false},
		{"0.6.0-alpha.1", false},
		{"0.6.0-beta.2", false},
		// stable release
		{"0.6.0", false},
		{"v0.6.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			cmd := &cobra.Command{Use: "root"}
			cmd.SetContext(t.Context())
			got := isVersionCheckDisabled(cmd, tt.version)
			if got != tt.skip {
				t.Errorf("isVersionCheckDisabled(%q) = %v, want %v", tt.version, got, tt.skip)
			}
		})
	}
}
