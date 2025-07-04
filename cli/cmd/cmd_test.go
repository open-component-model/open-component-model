package cmd_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/cli/cmd/internal/test"
	"ocm.software/open-component-model/cli/internal/reference/compref"
)

// setupTestRepository creates a test repository with the given component versions
func setupTestRepository(t *testing.T, versions ...*descriptor.Descriptor) (string, error) {
	r := require.New(t)
	archivePath := t.TempDir()
	fs, err := filesystem.NewFS(archivePath, os.O_RDWR)
	r.NoError(err, "could not create test filesystem")
	archive := ctf.NewFileSystemCTF(fs)
	helperRepo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(archive)))
	r.NoError(err, "could not create helper test repository")

	ctx := t.Context()
	for _, desc := range versions {
		r.NoError(helperRepo.AddComponentVersion(ctx, desc), "could not add component version to test repository")
	}

	return archivePath, nil
}

// createTestDescriptor creates a test component descriptor with the given name and version
func createTestDescriptor(name, version string) *descriptor.Descriptor {
	return &descriptor.Descriptor{
		Meta: descriptor.Meta{
			Version: "v2",
		},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    name,
					Version: version,
				},
			},
			Provider: descriptor.Provider{
				Name: "ocm.software",
			},
		},
	}
}

// Test_Get_Component_Version_Formats tests the different output formats for the get cv command
func Test_Get_Component_Version_Formats(t *testing.T) {
	// Setup test repository with a single component version
	desc := createTestDescriptor("ocm.software/test-component", "0.0.1")
	archivePath, err := setupTestRepository(t, desc)
	require.NoError(t, err)

	ref := compref.Ref{
		Repository: &ctfv1.Repository{
			Path: archivePath,
		},
		Component: desc.Component.Name,
		Version:   desc.Component.Version,
	}
	path := ref.String()

	tests := []struct {
		name           string
		args           []string
		expectedOutput string
		expectedError  bool
	}{
		{
			name: "Default Options (Table)",
			args: []string{"get", "cv", path},
			expectedOutput: `
COMPONENT                   │ VERSION │ PROVIDER     
─────────────────────────────┼─────────┼──────────────
 ocm.software/test-component │ 0.0.1   │ ocm.software
`,
			expectedError: false,
		},
		{
			name: "YAML output",
			args: []string{"get", "cv", path, "--output=yaml"},
			expectedOutput: `
component:
  componentReferences: null
  name: ocm.software/test-component
  provider: ocm.software
  repositoryContexts: null
  resources: null
  sources: null
  version: 0.0.1
meta:
  schemaVersion: v2
`,
			expectedError: false,
		},
		{
			name:           "JSON output",
			args:           []string{"get", "cv", path, "--output=json"},
			expectedOutput: "", // JSON output is handled differently
			expectedError:  false,
		},
		{
			name:           "Invalid output format",
			args:           []string{"get", "cv", path, "--output=invalid"},
			expectedOutput: "",
			expectedError:  true,
		},
		{
			name:           "Non-existent component",
			args:           []string{"get", "cv", "non-existent"},
			expectedOutput: "",
			expectedError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			logs := test.NewJSONLogReader()
			_, err = test.OCM(t, test.WithArgs(tt.args...), test.WithOutput(logs))

			if tt.expectedError {
				r.Error(err, "expected error but got none")
				return
			}

			r.NoError(err, "failed to run command")
			entries, err := logs.List()
			r.NoError(err, "failed to list log entries")

			if tt.args[len(tt.args)-1] == "--output=json" {
				// Handle JSON output separately
				r.EqualValues(map[string]any{
					"component": map[string]any{
						"componentReferences": nil,
						"name":                "ocm.software/test-component",
						"provider":            "ocm.software",
						"repositoryContexts":  nil,
						"resources":           nil,
						"sources":             nil,
						"version":             "0.0.1",
					},
					"meta": map[string]any{
						"schemaVersion": "v2",
					},
				}, entries[len(entries)-1].Extras)
			} else {
				discarded := logs.GetDiscarded()
				r.NotEmpty(discarded, "expected non json logs to contain output")
				r.EqualValues(strings.TrimSpace(tt.expectedOutput), strings.TrimSpace(discarded), "expected output")
			}
		})
	}
}

// Test_List_Component_Version_Variations tests different variations of listing component versions
func Test_List_Component_Version_Variations(t *testing.T) {
	// Setup test repository with multiple component versions
	desc1 := createTestDescriptor("ocm.software/test-component", "0.0.1")
	desc2 := createTestDescriptor("ocm.software/test-component", "0.0.2")
	archivePath, err := setupTestRepository(t, desc1, desc2)
	require.NoError(t, err)

	ref := compref.Ref{
		Repository: &ctfv1.Repository{
			Path: archivePath,
		},
		Component: desc1.Component.Name,
	}

	path := ref.String()

	tests := []struct {
		name           string
		args           []string
		expectedOutput string
		expectedError  bool
	}{
		{
			name: "Default Options (Table) - all versions",
			args: []string{"get", "cv", path},
			expectedOutput: `
COMPONENT                   │ VERSION │ PROVIDER     
─────────────────────────────┼─────────┼──────────────
 ocm.software/test-component │ 0.0.2   │ ocm.software 
                             │ 0.0.1   │     
`,
			expectedError: false,
		},
		{
			name: "Latest version only",
			args: []string{"get", "cv", path, "--latest"},
			expectedOutput: `
COMPONENT                   │ VERSION │ PROVIDER     
─────────────────────────────┼─────────┼──────────────
 ocm.software/test-component │ 0.0.2   │ ocm.software
`,
			expectedError: false,
		},
		{
			name: "Semver constraint",
			args: []string{"get", "cv", path, "--semver-constraint", "< 0.0.2"},
			expectedOutput: `
COMPONENT                   │ VERSION │ PROVIDER     
─────────────────────────────┼─────────┼──────────────
 ocm.software/test-component │ 0.0.1   │ ocm.software
`,
			expectedError: false,
		},
		{
			name:           "Invalid semver constraint",
			args:           []string{"get", "cv", path, "--semver-constraint", "invalid"},
			expectedOutput: "",
			expectedError:  true,
		},
		{
			name:           "Non-existent component",
			args:           []string{"get", "cv", "non-existent"},
			expectedOutput: "",
			expectedError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			logs := test.NewJSONLogReader()
			_, err = test.OCM(t, test.WithArgs(tt.args...), test.WithOutput(logs))

			if tt.expectedError {
				r.Error(err, "expected error but got none")
				return
			}

			r.NoError(err, "failed to run command")
			_, err := logs.List()
			r.NoError(err, "failed to list log entries")

			discarded := logs.GetDiscarded()
			r.NotEmpty(discarded, "expected non json logs to contain table")
			r.EqualValues(strings.TrimSpace(tt.expectedOutput), strings.TrimSpace(discarded), "expected table output")
		})
	}
}

func Test_Version(t *testing.T) {
	r := require.New(t)
	logs := test.NewJSONLogReader()
	_, err := test.OCM(t, test.WithArgs("version"), test.WithOutput(logs))
	r.NoError(err, "failed to run version command")

	entries, err := logs.List()
	r.NoError(err, "failed to list log entries")

	r.NotEmpty(entries, "expected log entries for version command")

	found := false
	for _, entry := range entries {
		ver, ok := entry.Extras["gitVersion"]
		if ok {
			found = true
			r.Equal(ver, "(devel)")
			break
		}
	}
	r.True(found, "expected to find gitVersion in log entries")
}
