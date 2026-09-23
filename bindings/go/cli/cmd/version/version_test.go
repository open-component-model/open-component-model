package version

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func runVersion(t *testing.T, args ...string) string {
	t.Helper()
	cmd := New()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	require.NoError(t, cmd.Execute())
	return out.String()
}

func TestVersion_DefaultIsHumanReadableAndIdentifiesV2(t *testing.T) {
	r := require.New(t)

	orig := BuildVersion
	t.Cleanup(func() { BuildVersion = orig })
	BuildVersion = "1.2.3"

	out := runVersion(t)

	// The default output must be the human-readable text format and must
	// unambiguously identify the binary as the OCM v2 CLI, differentiating it
	// from the legacy OCM v1 CLI.
	r.Contains(out, "Open Component Model "+Generation)
	r.Contains(out, "1.2.3")
	// It must not be raw JSON.
	r.False(json.Valid([]byte(strings.TrimSpace(out))), "default output should not be JSON")
}

func TestVersion_TextSplitsCommitAndDate(t *testing.T) {
	r := require.New(t)

	orig := BuildVersion
	t.Cleanup(func() { BuildVersion = orig })
	BuildVersion = "0.15.0-20260101000000-abcdef123456"

	out := runVersion(t, "--"+FlagOutput, OutputText)

	r.Contains(out, "GitCommit:")
	r.Contains(out, "abcdef123456")
	r.Contains(out, "BuildDate:")
	r.Contains(out, "2026-01-01T00:00:00Z")
}

func TestHumanBuildDate(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"plain timestamp", "20260101153045", "2026-01-01T15:30:45Z"},
		{"pseudo-version 0. form", "0.20260101153045", "2026-01-01T15:30:45Z"},
		{"empty", "", ""},
		{"non-timestamp", "not-a-date", "not-a-date"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, humanBuildDate(tc.in))
		})
	}
}

func TestVersion_LegacyJSONFormatUnchanged(t *testing.T) {
	r := require.New(t)

	orig := BuildVersion
	t.Cleanup(func() { BuildVersion = orig })
	BuildVersion = "1.2.3"

	out := runVersion(t, "--"+FlagOutput, OutputOCMv1)

	var info LegacyVersionInfo
	r.NoError(json.Unmarshal([]byte(out), &info))
	r.Equal("1", info.Major)
	r.Equal("2", info.Minor)
	r.Equal("3", info.Patch)
	r.Equal("1.2.3", info.GitVersion)
}
