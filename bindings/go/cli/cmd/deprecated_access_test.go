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

// Every spelling is checked on a source: a source is a plain reference, so add
// cv never contacts the origin and the test needs no network. The expectation
// mirrors the "deprecated: true" entries in each type's JSON schema, which is
// what the warning is read from.
func TestAddComponentVersionWarnsAboutDeprecatedAccessTypes(t *testing.T) {
	const commit = "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	gitHub := "repoUrl: https://github.com/open-component-model/ocm\n      commit: " + commit
	git := "repository: https://github.com/open-component-model/ocm-spec.git\n      commit: " + commit
	oci := "imageReference: ghcr.io/open-component-model/ocm:latest"
	wget := "url: https://example.com/archive.tgz"
	helm := "helmRepository: https://charts.example.com\n      helmChart: demo:1.0.0"

	// Every line carries the hint from the schema.
	hinted := func(canonical string) string {
		return "use " + canonical + " instead"
	}

	tests := []struct {
		spelling string
		fields   string
		// want is the text after "is deprecated, "; empty means no warning.
		want string
	}{
		// The GitHub type as a whole gives way to Git/v1, so even the canonical
		// spelling warns, and the schema carries the hint for every spelling.
		{"GitHub/v1", gitHub, hinted("Git/v1")},
		{"GitHub", gitHub, hinted("Git/v1")},
		{"github/v1", gitHub, hinted("Git/v1")},
		{"github", gitHub, hinted("Git/v1")},
		{"gitHub/v1", gitHub, hinted("Git/v1")},
		{"gitHub", gitHub, hinted("Git/v1")},

		{"Git/v1", git, ""},
		{"Git", git, ""},
		{"git", git, hinted("Git/v1")},
		{"git/v1alpha1", git, hinted("Git/v1")},
		{"Git/v1alpha1", git, hinted("Git/v1")},

		{"OCIImage/v1", oci, ""},
		{"ociArtifact/v1", oci, ""},
		{"ociArtifact", oci, hinted("OCIImage/v1")},
		{"ociImage", oci, hinted("OCIImage/v1")},

		{"Wget/v1", wget, ""},
		{"http/v1", wget, ""},
		{"wget", wget, hinted("Wget/v1")},
		{"http", wget, hinted("Wget/v1")},

		{"Helm/v1", helm, ""},
		{"helm", helm, hinted("Helm/v1")},
	}

	for _, tc := range tests {
		t.Run(tc.spelling, func(t *testing.T) {
			r := require.New(t)
			tmp := t.TempDir()

			constructorYAML := fmt.Sprintf(`
name: ocm.software/deprecated-access
version: 1.0.0
provider:
  name: ocm.software
sources:
  - name: probe
    type: blob
    access:
      type: %s
      %s
`, tc.spelling, tc.fields)
			constructorPath := filepath.Join(tmp, "component-constructor.yaml")
			r.NoError(os.WriteFile(constructorPath, []byte(constructorYAML), 0o600))

			logs := test.NewJSONLogReader()
			_, err := test.OCM(t, test.WithArgs("add", "cv",
				"--constructor", constructorPath,
				"--repository", filepath.Join(tmp, "archive"),
			), test.WithErrorOutput(logs))
			r.NoError(err)

			entries, err := logs.List()
			r.NoError(err)
			for _, entry := range entries {
				r.Nil(entry.Extras["access_type"], "the notice is printed, not logged")
			}

			// Everything on stderr that is not a JSON log line is the notice.
			notice := text.StripEscape(logs.Discarded.String())
			if tc.want != "" {
				r.Contains(notice, "\nWARNING: access type "+tc.spelling+" is deprecated, "+tc.want+"\n")
			} else {
				r.NotContains(notice, "WARNING", "current spelling must not warn")
			}
		})
	}
}
