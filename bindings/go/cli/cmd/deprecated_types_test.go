package cmd_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/cli/cmd/internal/test"
)

// The expectation mirrors the "deprecated: true" entries in each type's JSON
// schema, which is what the warning is read from.
func TestAddComponentVersionWarnsAboutDeprecatedTypes(t *testing.T) {
	const commit = "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	gitHub := "repoUrl: https://github.com/open-component-model/ocm\n      commit: " + commit
	git := "repository: https://github.com/open-component-model/ocm-spec.git\n      commit: " + commit
	oci := "imageReference: ghcr.io/open-component-model/ocm:latest"
	wget := "url: https://example.com/archive.tgz"
	helm := "helmRepository: https://charts.example.com\n      helmChart: demo:1.0.0"

	tests := []struct {
		kind     string // "access" or "input"
		spelling string
		fields   string
		// want is the text after "is deprecated, "; empty means no warning.
		want string
	}{
		// The GitHub type as a whole gives way to Git/v1, so even the canonical
		// spelling warns, and the schema carries the hint for every spelling.
		{"access", "GitHub/v1", gitHub, "use Git/v1 instead"},
		{"access", "GitHub", gitHub, "use Git/v1 instead"},
		{"access", "github/v1", gitHub, "use Git/v1 instead"},
		{"access", "github", gitHub, "use Git/v1 instead"},
		{"access", "gitHub/v1", gitHub, "use Git/v1 instead"},
		{"access", "gitHub", gitHub, "use Git/v1 instead"},

		{"access", "Git/v1", git, ""},
		{"access", "Git", git, ""},
		{"access", "git", git, "use Git/v1 instead"},
		{"access", "git/v1alpha1", git, "use Git/v1 instead"},
		{"access", "Git/v1alpha1", git, "use Git/v1 instead"},

		{"access", "OCIImage/v1", oci, ""},
		{"access", "ociArtifact/v1", oci, ""},
		{"access", "ociArtifact", oci, "use OCIImage/v1 instead"},
		{"access", "ociImage", oci, "use OCIImage/v1 instead"},

		{"access", "Wget/v1", wget, ""},
		{"access", "http/v1", wget, ""},
		{"access", "wget", wget, "use Wget/v1 instead"},
		{"access", "http", wget, "use Wget/v1 instead"},

		{"access", "Helm/v1", helm, ""},
		{"access", "helm", helm, "use Helm/v1 instead"},

		// Inputs that need no network: utf8 carries its content inline and
		// file reads a local file.
		{"input", "utf8/v1", "text: hello", ""},
		{"input", "UTF8/v1", "text: hello", ""},
		{"input", "utf8", "text: hello", "use UTF8/v1 instead"},
		{"input", "UTF8", "text: hello", "use UTF8/v1 instead"},

		{"input", "file/v1", "path: payload.txt", ""},
		{"input", "file", "path: payload.txt", "use File/v1 instead"},
		{"input", "File", "path: payload.txt", "use File/v1 instead"},
	}

	for _, tc := range tests {
		t.Run(tc.kind+" "+tc.spelling, func(t *testing.T) {
			r := require.New(t)

			// An access type goes on a source: a source is a plain reference, so
			// add cv never contacts the origin and the test needs no network.
			element := "resources"
			if tc.kind == "access" {
				element = "sources"
			}
			notice := addComponentVersionNotice(t, fmt.Sprintf(`
name: ocm.software/deprecated-types
version: 1.0.0
provider:
  name: ocm.software
%s:
  - name: probe
    type: blob
    %s:
      type: %s
      %s
`, element, tc.kind, tc.spelling, tc.fields))

			if tc.want != "" {
				r.Contains(notice, "\nWARNING: "+tc.kind+" type "+tc.spelling+" is deprecated, "+tc.want+"\n")
			} else {
				r.NotContains(notice, "WARNING", "current spelling must not warn")
			}
		})
	}
}

// addComponentVersionNotice runs add cv on constructorYAML against a fresh CTF
// and returns what the command wrote to stderr besides JSON log lines, with
// color escapes stripped: that is the deprecation notice, if any. A payload.txt
// sits next to the constructor for file inputs.
func addComponentVersionNotice(t *testing.T, constructorYAML string) string {
	t.Helper()
	r := require.New(t)
	tmp := t.TempDir()
	r.NoError(os.WriteFile(filepath.Join(tmp, "payload.txt"), []byte("hello"), 0o600))
	constructorPath := filepath.Join(tmp, "component-constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructorYAML), 0o600))

	logs := test.NewJSONLogReader()
	_, err := test.OCM(t, test.WithArgs("add", "cv",
		"--constructor", constructorPath,
		"--repository", filepath.Join(tmp, "archive"),
	), test.WithErrorOutput(logs))
	r.NoError(err)

	// List splits stderr into JSON log entries and the leftover text.
	_, err = logs.List()
	r.NoError(err)
	return text.StripEscape(logs.GetDiscarded())
}
