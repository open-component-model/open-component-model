package cmd_test

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/runtime"
	ocmctx "ocm.software/open-component-model/cli/internal/context"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	componentversion "ocm.software/open-component-model/cli/cmd/add/component-version"
	"ocm.software/open-component-model/cli/cmd/internal/test"
	"ocm.software/open-component-model/cli/internal/reference/compref"
)

// setupTestRepositoryWithDescriptorLibrary creates a test repository with the given component versions
func setupTestRepositoryWithDescriptorLibrary(t *testing.T, versions ...*descriptor.Descriptor) (string, error) {
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
	archivePath, err := setupTestRepositoryWithDescriptorLibrary(t, desc)
	require.NoError(t, err)

	ref := compref.Ref{
		Repository: &ctfv1.Repository{
			FilePath: archivePath,
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
- component:
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
			name:           "NDJSON output",
			args:           []string{"get", "cv", path, "--output=ndjson"},
			expectedOutput: "", // JSON output is handled differently
			expectedError:  false,
		},
		{
			name: "tree output",
			args: []string{"get", "cv", path, "--output=tree"},
			expectedOutput: `NESTING  COMPONENT                    VERSION  PROVIDER      IDENTITY                                       
 └─       ocm.software/test-component  0.0.1    ocm.software  name=ocm.software/test-component,version=0.0.1`,
			expectedError: false,
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
			result := new(bytes.Buffer)
			_, err = test.OCM(t, test.WithArgs(tt.args...), test.WithOutput(result), test.WithErrorOutput(logs))

			if tt.expectedError {
				r.Error(err, "expected error but got none")
				return
			}

			r.NoError(err, "failed to run command")

			if tt.args[len(tt.args)-1] == "--output=json" || tt.args[len(tt.args)-1] == "--output=ndjson" {
				// Handle JSON output separately
				var resultJSON any
				decoder := json.NewDecoder(result)
				r.NoError(decoder.Decode(&resultJSON), "failed to decode result JSON")

				component := map[string]any{
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
				}

				switch {
				case slices.Contains(tt.args, "--output=json"):
					r.EqualValues([]any{component}, resultJSON)
				case slices.Contains(tt.args, "--output=ndjson"):
					r.EqualValues(component, resultJSON)
				}
			}

			logEntries, err := logs.List()
			r.NoError(err, "failed to list log entries")
			r.NotEmpty(logEntries, "expected log entries to be present")

			r.EqualValues(strings.TrimSpace(tt.expectedOutput), strings.TrimSpace(result.String()), "expected output")
		})
	}
}

// Test_Get_Component_Version_Formats_Recursive tests the different output formats for the get cv command
func Test_Get_Component_Version_Formats_Recursive(t *testing.T) {
	// Setup test repository
	leafa := createTestDescriptor("ocm.software/leaf-a", "0.0.1")
	leafb := createTestDescriptor("ocm.software/leaf-b", "0.0.1")
	root := createTestDescriptor("ocm.software/root", "0.0.1")
	root.Component.References = []descriptor.Reference{
		{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "leaf-a",
					Version: leafa.Component.Version,
				},
			},
			Component: leafa.Component.Name,
		},
		{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "leaf-b",
					Version: leafb.Component.Version,
				},
			},
			Component: leafb.Component.Name,
		},
	}

	archivePath, err := setupTestRepositoryWithDescriptorLibrary(t, root, leafa, leafb)
	require.NoError(t, err)

	ref := compref.Ref{
		Repository: &ctfv1.Repository{
			FilePath: archivePath,
		},
		Component: root.Component.Name,
		Version:   root.Component.Version,
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
			args: []string{"get", "cv", path, "--recursive=-1"},
			expectedOutput: `
COMPONENT           │ VERSION │ PROVIDER     
─────────────────────┼─────────┼──────────────
 ocm.software/root   │ 0.0.1   │ ocm.software 
 ocm.software/leaf-a │ 0.0.1   │              
 ocm.software/leaf-b │ 0.0.1   │
`,
			expectedError: false,
		},
		{
			name: "YAML output",
			args: []string{"get", "cv", path, "--output=yaml", "--recursive=-1"},
			expectedOutput: `
- component:
    componentReferences:
    - componentName: ocm.software/leaf-a
      digest:
        hashAlgorithm: ""
        normalisationAlgorithm: ""
        value: ""
      name: leaf-a
      version: 0.0.1
    - componentName: ocm.software/leaf-b
      digest:
        hashAlgorithm: ""
        normalisationAlgorithm: ""
        value: ""
      name: leaf-b
      version: 0.0.1
    name: ocm.software/root
    provider: ocm.software
    repositoryContexts: null
    resources: null
    sources: null
    version: 0.0.1
  meta:
    schemaVersion: v2
- component:
    componentReferences: null
    name: ocm.software/leaf-a
    provider: ocm.software
    repositoryContexts: null
    resources: null
    sources: null
    version: 0.0.1
  meta:
    schemaVersion: v2
- component:
    componentReferences: null
    name: ocm.software/leaf-b
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
			args:           []string{"get", "cv", path, "--output=json", "--recursive=-1"},
			expectedOutput: "", // JSON output is handled differently
			expectedError:  false,
		},
		{
			name:           "NDJSON output",
			args:           []string{"get", "cv", path, "--output=ndjson", "--recursive=-1"},
			expectedOutput: "", // JSON output is handled differently
			expectedError:  false,
		},
		{
			name: "tree output",
			args: []string{"get", "cv", path, "--output=tree", "--recursive=-1"},
			expectedOutput: `NESTING  COMPONENT            VERSION  PROVIDER      IDENTITY                               
 └─ ●     ocm.software/root    0.0.1    ocm.software  name=ocm.software/root,version=0.0.1   
    ├─    ocm.software/leaf-a  0.0.1    ocm.software  name=ocm.software/leaf-a,version=0.0.1 
    └─    ocm.software/leaf-b  0.0.1    ocm.software  name=ocm.software/leaf-b,version=0.0.1`,
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			logs := test.NewJSONLogReader()
			result := new(bytes.Buffer)
			_, err = test.OCM(t, test.WithArgs(tt.args...), test.WithOutput(result), test.WithErrorOutput(logs))

			if tt.expectedError {
				r.Error(err, "expected error but got none")
				return
			}
			r.NoError(err, "failed to run command")

			if slices.Contains(tt.args, "--output=json") || slices.Contains(tt.args, "--output=ndjson") {
				// Handle JSON output separately
				expectedRoot := map[string]any{
					"component": map[string]any{
						"componentReferences": []any{
							map[string]any{
								"name":          "leaf-a",
								"version":       "0.0.1",
								"componentName": "ocm.software/leaf-a",
								"digest": map[string]any{
									"hashAlgorithm":          "",
									"normalisationAlgorithm": "",
									"value":                  "",
								},
							},
							map[string]any{
								"name":          "leaf-b",
								"version":       "0.0.1",
								"componentName": "ocm.software/leaf-b",
								"digest": map[string]any{
									"hashAlgorithm":          "",
									"normalisationAlgorithm": "",
									"value":                  "",
								},
							},
						},
						"name":               "ocm.software/root",
						"provider":           "ocm.software",
						"repositoryContexts": nil,
						"resources":          nil,
						"sources":            nil,
						"version":            "0.0.1",
					},
					"meta": map[string]any{
						"schemaVersion": "v2",
					},
				}
				expectedLeafA := map[string]any{
					"component": map[string]any{
						"componentReferences": nil,
						"name":                "ocm.software/leaf-a",
						"provider":            "ocm.software",
						"repositoryContexts":  nil,
						"resources":           nil,
						"sources":             nil,
						"version":             "0.0.1",
					},
					"meta": map[string]any{
						"schemaVersion": "v2",
					},
				}
				expectedLeafB := map[string]any{
					"component": map[string]any{
						"componentReferences": nil,
						"name":                "ocm.software/leaf-b",
						"provider":            "ocm.software",
						"repositoryContexts":  nil,
						"resources":           nil,
						"sources":             nil,
						"version":             "0.0.1",
					},
					"meta": map[string]any{
						"schemaVersion": "v2",
					},
				}

				var resultJSON any
				decoder := json.NewDecoder(result)
				switch {
				case slices.Contains(tt.args, "--output=json"):
					r.NoError(decoder.Decode(&resultJSON), "failed to decode result JSON")
					r.EqualValues([]any{expectedRoot, expectedLeafA, expectedLeafB}, resultJSON)
				case slices.Contains(tt.args, "--output=ndjson"):
					r.NoError(decoder.Decode(&resultJSON))
					r.EqualValues(expectedRoot, resultJSON)
					r.NoError(decoder.Decode(&resultJSON))
					r.EqualValues(expectedLeafA, resultJSON)
					r.NoError(decoder.Decode(&resultJSON))
					r.EqualValues(expectedLeafB, resultJSON)
				}
			}

			logEntries, err := logs.List()
			r.NoError(err, "failed to list log entries")
			r.NotEmpty(logEntries, "expected log entries to be present")

			r.EqualValues(strings.TrimSpace(tt.expectedOutput), strings.TrimSpace(result.String()), "expected output")
		})
	}
}

// Test_List_Component_Version_Variations tests different variations of listing component versions
func Test_List_Component_Version_Variations(t *testing.T) {
	// Setup test repository with multiple component versions
	desc1 := createTestDescriptor("ocm.software/test-component", "0.0.1")
	desc2 := createTestDescriptor("ocm.software/test-component", "0.0.2")
	archivePath, err := setupTestRepositoryWithDescriptorLibrary(t, desc1, desc2)
	require.NoError(t, err)

	ref := compref.Ref{
		Repository: &ctfv1.Repository{
			FilePath: archivePath,
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
			name:          "No versions",
			args:          []string{"get", "cv", "."},
			expectedError: true,
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
			result := new(bytes.Buffer)
			_, err = test.OCM(t, test.WithArgs(tt.args...), test.WithOutput(result), test.WithErrorOutput(logs))

			if tt.expectedError {
				r.Error(err, "expected error but got none")
				return
			}

			r.NoError(err, "failed to run command")

			logEntries, err := logs.List()
			r.NoError(err, "failed to list log entries")
			r.NotEmpty(logEntries, "expected log entries to be present")

			r.EqualValues(strings.TrimSpace(tt.expectedOutput), strings.TrimSpace(result.String()), "expected table output")
		})
	}
}

// Test_List_Component_Version_Variations tests different variations of listing component versions
func Test_List_Component_Version_Variations_Recursive(t *testing.T) {
	// Setup test repository
	leafa := createTestDescriptor("ocm.software/leaf-a", "0.0.1")
	leafb := createTestDescriptor("ocm.software/leaf-b", "0.0.1")
	desc1 := createTestDescriptor("ocm.software/root", "0.0.1")
	desc2 := createTestDescriptor("ocm.software/root", "0.0.2")

	desc1.Component.References = []descriptor.Reference{
		{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "leaf-a",
					Version: leafa.Component.Version,
				},
			},
			Component: leafa.Component.Name,
		},
		{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "leaf-b",
					Version: leafb.Component.Version,
				},
			},
			Component: leafb.Component.Name,
		},
	}
	desc2.Component.References = []descriptor.Reference{
		{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "leaf-a",
					Version: leafa.Component.Version,
				},
			},
			Component: leafa.Component.Name,
		},
	}
	archivePath, err := setupTestRepositoryWithDescriptorLibrary(t, desc1, desc2, leafa, leafb)
	require.NoError(t, err)

	ref := compref.Ref{
		Repository: &ctfv1.Repository{
			FilePath: archivePath,
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
			args: []string{"get", "cv", path, "--recursive=-1"},
			expectedOutput: `
COMPONENT           │ VERSION │ PROVIDER     
─────────────────────┼─────────┼──────────────
 ocm.software/root   │ 0.0.2   │ ocm.software 
 ocm.software/leaf-a │ 0.0.1   │              
 ocm.software/root   │ 0.0.1   │              
 ocm.software/leaf-a │ 0.0.1   │              
 ocm.software/leaf-b │ 0.0.1   │
`,
			expectedError: false,
		},
		{
			name: "tree output - all versions",
			args: []string{"get", "cv", path, "--output=tree", "--recursive=-1"},
			expectedOutput: `NESTING  COMPONENT            VERSION  PROVIDER      IDENTITY                               
 ├─ ●     ocm.software/root    0.0.2    ocm.software  name=ocm.software/root,version=0.0.2   
 │  └─    ocm.software/leaf-a  0.0.1    ocm.software  name=ocm.software/leaf-a,version=0.0.1 
 └─ ●     ocm.software/root    0.0.1    ocm.software  name=ocm.software/root,version=0.0.1   
    ├─    ocm.software/leaf-a  0.0.1    ocm.software  name=ocm.software/leaf-a,version=0.0.1 
    └─    ocm.software/leaf-b  0.0.1    ocm.software  name=ocm.software/leaf-b,version=0.0.1`,
			expectedError: false,
		},
		{
			name: "Latest version only",
			args: []string{"get", "cv", path, "--latest", "--recursive=-1"},
			expectedOutput: `
COMPONENT           │ VERSION │ PROVIDER     
─────────────────────┼─────────┼──────────────
 ocm.software/root   │ 0.0.2   │ ocm.software 
 ocm.software/leaf-a │ 0.0.1   │
`,
			expectedError: false,
		},
		{
			name: "Semver constraint",
			args: []string{"get", "cv", path, "--semver-constraint", "< 0.0.2", "--recursive=-1"},
			expectedOutput: `
COMPONENT           │ VERSION │ PROVIDER     
─────────────────────┼─────────┼──────────────
 ocm.software/root   │ 0.0.1   │ ocm.software 
 ocm.software/leaf-a │ 0.0.1   │              
 ocm.software/leaf-b │ 0.0.1   │
`,
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			logs := test.NewJSONLogReader()
			result := new(bytes.Buffer)
			_, err = test.OCM(t, test.WithArgs(tt.args...), test.WithOutput(result), test.WithErrorOutput(logs))

			if tt.expectedError {
				r.Error(err, "expected error but got none")
				return
			}

			r.NoError(err, "failed to run command")

			logEntries, err := logs.List()
			r.NoError(err, "failed to list log entries")
			r.NotEmpty(logEntries, "expected log entries to be present")

			r.EqualValues(strings.TrimSpace(tt.expectedOutput), strings.TrimSpace(result.String()), "expected table output")
		})
	}
}

// Test_Get_Component_Version_CTF_Dir tests the call to `ocm get cv <ctf_dir>` command.
// The command has to list all component versions contained in the CTF folder.
func Test_Get_Component_Version_CTF_Dir(t *testing.T) {
	roota := createTestDescriptor("ocm.software/root-a", "0.0.1")
	rootb := createTestDescriptor("ocm.software/root-b", "0.0.2")

	archivePath, err := setupTestRepositoryWithDescriptorLibrary(t, roota, rootb)
	require.NoError(t, err)

	tests := []struct {
		name           string
		args           []string
		configYAML     string
		expectedOutput string
		expectedError  bool
	}{
		{
			name: "get cv from CTF dir - default",
			args: []string{"get", "cv", archivePath},
			expectedOutput: `
COMPONENT           │ VERSION │ PROVIDER     
─────────────────────┼─────────┼──────────────
 ocm.software/root-b │ 0.0.2   │ ocm.software 
 ocm.software/root-a │ 0.0.1   │`,
			expectedError: false,
		},
		{
			// The resolver configuration in the configYAML points to a non-existing path.
			// Therefore, the command would fail to find the components when doing rendering,
			// if there were no dynamically added resolvers, that take precedence over the config file.
			name: "get cv from CTF dir - override resolvers from config",
			args: []string{"get", "cv", archivePath},
			configYAML: `
type: generic.config.ocm.software/v1
configurations:
- type: resolvers.config.ocm.software
  resolvers:
  - repository:
      type: CommonTransportFormat/v1
      filePath: /does/not/exist
    componentNamePattern: ocm.software/*`,
			expectedOutput: `
COMPONENT           │ VERSION │ PROVIDER     
─────────────────────┼─────────┼──────────────
 ocm.software/root-b │ 0.0.2   │ ocm.software 
 ocm.software/root-a │ 0.0.1   │`,
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			args := tt.args

			if tt.configYAML != "" {
				tmp := t.TempDir()
				configYAMLFilePath := filepath.Join(tmp, "config-with-resolver.yaml")
				r.NoError(os.WriteFile(configYAMLFilePath, []byte(tt.configYAML), 0o600))
				args = append(args, "--config", configYAMLFilePath)
			}

			logs := test.NewJSONLogReader()
			result := new(bytes.Buffer)
			_, err = test.OCM(t, test.WithArgs(args...), test.WithOutput(result), test.WithErrorOutput(logs))

			if tt.expectedError {
				r.Error(err, "expected error but got none")
				return
			}

			r.NoError(err, "failed to run command")

			logEntries, err := logs.List()
			r.NoError(err, "failed to list log entries")
			r.NotEmpty(logEntries, "expected log entries to be present")

			r.EqualValues(strings.TrimSpace(tt.expectedOutput), strings.TrimSpace(result.String()), "expected table output")
		})
	}
}

func Test_Add_Component_Version(t *testing.T) {
	r := require.New(t)
	logs := test.NewJSONLogReader()
	tmp := t.TempDir()

	// Create a test file to be added to the component version
	testFilePath := filepath.Join(tmp, "test-file.txt")
	r.NoError(os.WriteFile(testFilePath, []byte("foobar"), 0o600), "could not create test file")

	constructorYAML := fmt.Sprintf(`
name: ocm.software/examples-01
version: 1.0.0
provider:
  name: ocm.software
resources:
  - name: my-file
    type: blob
    input:
      type: file/v1
      path: %[1]s
`, testFilePath)

	constructorYAMLFilePath := filepath.Join(tmp, "component-constructor.yaml")
	r.NoError(os.WriteFile(constructorYAMLFilePath, []byte(constructorYAML), 0o600))

	archiveFilePath := filepath.Join(tmp, "transport-archive")

	t.Run("base construction", func(t *testing.T) {
		_, err := test.OCM(t, test.WithArgs("add", "cv",
			"--constructor", constructorYAMLFilePath,
			"--repository", archiveFilePath,
		), test.WithErrorOutput(logs))

		r.NoError(err, "could not construct component version")

		entries, err := logs.List()
		r.NoError(err, "failed to list log entries")
		r.NotEmpty(entries, "expected log entries to be present")

		fs, err := filesystem.NewFS(archiveFilePath, os.O_RDONLY)
		r.NoError(err, "could not create test filesystem")
		archive := ctf.NewFileSystemCTF(fs)
		helperRepo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(archive)))
		r.NoError(err, "could not create helper test repository")

		desc, err := helperRepo.GetComponentVersion(t.Context(), "ocm.software/examples-01", "1.0.0")
		r.NoError(err, "could not retrieve component version from test repository")

		r.Equal("ocm.software/examples-01", desc.Component.Name, "expected component name to match")
		r.Equal("1.0.0", desc.Component.Version, "expected component version to match")
		r.Len(desc.Component.Resources, 1, "expected one resource in component version")
		r.Equal("my-file", desc.Component.Resources[0].Name, "expected resource name to match")
		r.Equal("blob", desc.Component.Resources[0].Type, "expected resource type to match")
		r.NotNil(desc.Component.Resources[0].Access, "expected resource access to be set")
		r.Equal("localBlob/v1", desc.Component.Resources[0].Access.GetType().String(), "expected resource access type to match")

		blb, _, err := helperRepo.GetLocalResource(t.Context(), desc.Component.Name, desc.Component.Version, desc.Component.Resources[0].ToIdentity())
		r.NoError(err, "could not retrieve local resource from test repository")
		var buf bytes.Buffer
		r.NoError(blob.Copy(&buf, blb))
		r.Equal("foobar", buf.String(), "expected resource content to match test file content")

		t.Run("expect failure on existing component version", func(t *testing.T) {
			constructorYAML = fmt.Sprintf(`
name: ocm.software/examples-01
version: invalid_1.0.0
provider:
  name: ocm.software
resources:
  - name: my-file-replaced
    type: blob
    input:
      type: file/v1
      path: %[1]s
`, testFilePath)

			// Create a replacement test file to be added to the component version
			testFilePath := filepath.Join(tmp, "test-file.txt")
			r.NoError(os.WriteFile(testFilePath, []byte("replaced"), 0o600), "could not create test file")

			constructorYAMLFilePath := filepath.Join(tmp, "component-constructor-replace.yaml")
			r.NoError(os.WriteFile(constructorYAMLFilePath, []byte(constructorYAML), 0o600))
			logs := test.NewJSONLogReader()
			result := new(bytes.Buffer)
			_, err := test.OCM(t, test.WithArgs("add", "cv",
				"--constructor", constructorYAMLFilePath,
				"--repository", archiveFilePath,
				"--component-version-conflict-policy", string(componentversion.ComponentVersionConflictPolicyReplace),
			), test.WithErrorOutput(logs), test.WithOutput(result))

			r.Error(err, "expected error on invalid component version semver")
		})

		t.Run("expect failure on invalid semver in constructor", func(t *testing.T) {

			_, err := test.OCM(t, test.WithArgs("add", "cv",
				"--constructor", constructorYAMLFilePath,
				"--repository", archiveFilePath,
			), test.WithErrorOutput(logs))

			r.Error(err, "expected error on adding existing component version")
			r.Contains(err.Error(), "already exists in target repository", "expected error message about existing component version")

			entries, err := logs.List()
			r.NoError(err, "failed to list log entries")
			r.NotEmpty(entries, "expected log entries to be present")
		})

		t.Run("expect success on replace strategy", func(t *testing.T) {
			constructorYAML = fmt.Sprintf(`
name: ocm.software/examples-01
version: 1.0.0
provider:
  name: ocm.software
resources:
  - name: my-file-replaced
    type: blob
    input:
      type: file/v1
      path: %[1]s
`, testFilePath)

			// Create a replacement test file to be added to the component version
			testFilePath := filepath.Join(tmp, "test-file.txt")
			r.NoError(os.WriteFile(testFilePath, []byte("replaced"), 0o600), "could not create test file")

			constructorYAMLFilePath := filepath.Join(tmp, "component-constructor-replace.yaml")
			r.NoError(os.WriteFile(constructorYAMLFilePath, []byte(constructorYAML), 0o600))
			logs := test.NewJSONLogReader()
			result := new(bytes.Buffer)
			_, err := test.OCM(t, test.WithArgs("add", "cv",
				"--constructor", constructorYAMLFilePath,
				"--repository", archiveFilePath,
				"--component-version-conflict-policy", string(componentversion.ComponentVersionConflictPolicyReplace),
			), test.WithErrorOutput(logs), test.WithOutput(result))

			r.NoError(err, "could not construct component version", "replace strategy should allow an existing component version to be replaced")

			entries, err := logs.List()
			r.NoError(err, "failed to list log entries")
			r.NotEmpty(entries, "expected log entries to be present")

			desc, err := helperRepo.GetComponentVersion(t.Context(), "ocm.software/examples-01", "1.0.0")
			r.NoError(err, "could not retrieve component version from test repository")

			r.Equal("ocm.software/examples-01", desc.Component.Name, "expected component name to match")
			r.Equal("1.0.0", desc.Component.Version, "expected component version to match")
			r.Len(desc.Component.Resources, 1, "expected one resource in component version")
			r.Equal("my-file-replaced", desc.Component.Resources[0].Name, "expected resource name to match")

			blb, _, err := helperRepo.GetLocalResource(t.Context(), desc.Component.Name, desc.Component.Version, desc.Component.Resources[0].ToIdentity())
			r.NoError(err, "could not retrieve local resource from test repository")
			var buf bytes.Buffer
			r.NoError(blob.Copy(&buf, blb))
			r.Equal("replaced", buf.String(), "expected resource content to match test file content")
		})

		t.Run("expect failure with working-directory if resources are not in working-directory", func(t *testing.T) {
			workingDir := filepath.Join(tmp, "working-dir")
			r.NoError(os.Mkdir(workingDir, 0o700), "could not create working directory")

			_, err := test.OCM(t, test.WithArgs("add", "cv",
				"--constructor", constructorYAMLFilePath,
				"--repository", archiveFilePath,
				"--working-directory", workingDir,
			), test.WithErrorOutput(logs))

			r.Error(err, "expected error on adding component version with working directory")
		})

		t.Run("base construction with working-directory should not fail if resources are in working-directory", func(t *testing.T) {
			constructorYAML = fmt.Sprintf(`
name: ocm.software/examples-01
version: 1.0.0
provider:
  name: ocm.software
resources:
  - name: my-file-replaced
    type: blob
    input:
      type: file/v1
      path: %[1]s
`, testFilePath)

			// Create a replacement test file to be added to the component version
			testFilePath := filepath.Join(tmp, "test-file-2.txt")
			r.NoError(os.WriteFile(testFilePath, []byte("replaced"), 0o600), "could not create test file")
			constructorYAMLFilePath := filepath.Join(tmp, "component-constructor-replace-wd.yaml")
			r.NoError(os.WriteFile(constructorYAMLFilePath, []byte(constructorYAML), 0o600))

			cmd, err := test.OCM(t, test.WithArgs("add", "cv",
				"--constructor", constructorYAMLFilePath,
				"--repository", archiveFilePath,
				"--working-directory", tmp,
				"--component-version-conflict-policy", string(componentversion.ComponentVersionConflictPolicyReplace),
			), test.WithErrorOutput(logs))

			r.Equal(ocmctx.FromContext(cmd.Context()).FilesystemConfig().WorkingDirectory, tmp, "expected working directory to be set in ocm context automatically")

			r.NoError(err, "could not construct component version with working directory")
		})
	})

	t.Run("construction with references targeting fallback resolvers", func(t *testing.T) {
		tmp := t.TempDir()
		externalConstructorYAML := fmt.Sprintf(`
name: ocm.software/external
version: 1.0.0
provider:
  name: ocm.software
resources:
- name: my-resource
  type: blob
  input:
    type: utf8/v1
    text: "I come from external!"
`)
		externalConstructorYAMLFilePath := filepath.Join(tmp, "component-constructor-external.yaml")
		r.NoError(os.WriteFile(externalConstructorYAMLFilePath, []byte(externalConstructorYAML), 0o600))
		externalArchiveFilePath := filepath.Join(tmp, "transport-archive-external")

		_, err := test.OCM(t, test.WithArgs("add", "cv",
			"--constructor", externalConstructorYAMLFilePath,
			"--repository", externalArchiveFilePath,
			"--working-directory", tmp,
		), test.WithErrorOutput(logs))
		r.NoError(err, "could not construct component version with working directory")

		legacyResolverConfigYAML := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
- type: ocm.config.ocm.software
  resolvers:
  - repository:
      type: CommonTransportFormat/v1
      filePath: %[1]s
`, externalArchiveFilePath)

		legacyResolverConfigYAMLFilePath := filepath.Join(tmp, "config-with-legacy-resolver.yaml")
		r.NoError(os.WriteFile(legacyResolverConfigYAMLFilePath, []byte(legacyResolverConfigYAML), 0o600))

		constructorYAML = fmt.Sprintf(`
components:
- name: ocm.software/a
  version: 1.0.0
  provider:
    name: ocm.software
  resources:
    - name: my-resource
      type: blob
      input:
        type: utf8/v1
        text: "I come from A"
- name: ocm.software/b
  version: 1.0.0
  provider:
    name: ocm.software
  componentReferences:
    - name: b-to-a # internal reference
      version: 1.0.0
      componentName: ocm.software/a
    - name: external
      version: 1.0.0
      componentName: ocm.software/external # from external repository
  resources:
    - name: my-resource
      type: blob
      input:
        type: utf8/v1
        text: "I come from B"
`)

		// Create a replacement test file to be added to the component version
		constructorYAMLFilePath := filepath.Join(tmp, "component-constructor-external-reference.yaml")
		r.NoError(os.WriteFile(constructorYAMLFilePath, []byte(constructorYAML), 0o600))

		cmd, err := test.OCM(t, test.WithArgs("add", "cv",
			"--constructor", constructorYAMLFilePath,
			"--repository", archiveFilePath,
			"--working-directory", tmp,
			"--config", legacyResolverConfigYAMLFilePath,
			"--component-version-conflict-policy", string(componentversion.ComponentVersionConflictPolicyReplace),
			"--external-component-version-copy-policy", string(componentversion.ExternalComponentVersionCopyPolicyCopyOrFail),
		), test.WithErrorOutput(logs))

		r.Equal(ocmctx.FromContext(cmd.Context()).FilesystemConfig().WorkingDirectory, tmp, "expected working directory to be set in ocm context automatically")

		r.NoError(err, "could not construct component version with working directory")

		fs, err := filesystem.NewFS(archiveFilePath, os.O_RDONLY)
		r.NoError(err, "could not create test filesystem")
		archive := ctf.NewFileSystemCTF(fs)
		helperRepo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(archive)))
		r.NoError(err, "could not create helper test repository")

		for _, identity := range []runtime.Identity{{
			descriptor.IdentityAttributeName:    "ocm.software/a",
			descriptor.IdentityAttributeVersion: "1.0.0",
		}, {
			descriptor.IdentityAttributeName:    "ocm.software/b",
			descriptor.IdentityAttributeVersion: "1.0.0",
		}, {
			descriptor.IdentityAttributeName:    "ocm.software/external",
			descriptor.IdentityAttributeVersion: "1.0.0",
		}} {
			t.Run(identity.String(), func(t *testing.T) {
				r := require.New(t)
				_, err := helperRepo.GetComponentVersion(t.Context(),
					identity[descriptor.IdentityAttributeName],
					identity[descriptor.IdentityAttributeVersion],
				)
				r.NoError(err, "could not retrieve component version from test repository")
			})
		}
	})

	t.Run("construction with references targeting resolvers", func(t *testing.T) {
		tmp := t.TempDir()
		externalConstructorYAML := fmt.Sprintf(`
name: ocm.software/external
version: 1.0.0
provider:
  name: ocm.software
resources:
- name: my-resource
  type: blob
  input:
    type: utf8/v1
    text: "I come from external!"
`)
		externalConstructorYAMLFilePath := filepath.Join(tmp, "component-constructor-external.yaml")
		r.NoError(os.WriteFile(externalConstructorYAMLFilePath, []byte(externalConstructorYAML), 0o600))
		externalArchiveFilePath := filepath.Join(tmp, "transport-archive-external")

		_, err := test.OCM(t, test.WithArgs("add", "cv",
			"--constructor", externalConstructorYAMLFilePath,
			"--repository", externalArchiveFilePath,
			"--working-directory", tmp,
		), test.WithErrorOutput(logs))
		r.NoError(err, "could not construct component version with working directory")

		resolverConfigYAML := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
- type: resolvers.config.ocm.software
  resolvers:
  - repository:
      type: CommonTransportFormat/v1
      filePath: %[1]s
    componentNamePattern: ocm.software/*
`, externalArchiveFilePath)

		resolverConfigYAMLFilePath := filepath.Join(tmp, "config-with-resolver.yaml")
		r.NoError(os.WriteFile(resolverConfigYAMLFilePath, []byte(resolverConfigYAML), 0o600))

		constructorYAML = fmt.Sprintf(`
components:
- name: ocm.software/a
  version: 1.0.0
  provider:
    name: ocm.software
  resources:
    - name: my-resource
      type: blob
      input:
        type: utf8/v1
        text: "I come from A"
- name: ocm.software/b
  version: 1.0.0
  provider:
    name: ocm.software
  componentReferences:
    - name: b-to-a # internal reference
      version: 1.0.0
      componentName: ocm.software/a
    - name: external
      version: 1.0.0
      componentName: ocm.software/external # from external repository
  resources:
    - name: my-resource
      type: blob
      input:
        type: utf8/v1
        text: "I come from B"
`)

		// Create a replacement test file to be added to the component version
		constructorYAMLFilePath := filepath.Join(tmp, "component-constructor-external-reference.yaml")
		r.NoError(os.WriteFile(constructorYAMLFilePath, []byte(constructorYAML), 0o600))

		cmd, err := test.OCM(t, test.WithArgs("add", "cv",
			"--constructor", constructorYAMLFilePath,
			"--repository", archiveFilePath,
			"--working-directory", tmp,
			"--config", resolverConfigYAMLFilePath,
			"--component-version-conflict-policy", string(componentversion.ComponentVersionConflictPolicyReplace),
			"--external-component-version-copy-policy", string(componentversion.ExternalComponentVersionCopyPolicyCopyOrFail),
		), test.WithErrorOutput(logs))

		r.Equal(ocmctx.FromContext(cmd.Context()).FilesystemConfig().WorkingDirectory, tmp, "expected working directory to be set in ocm context automatically")

		r.NoError(err, "could not construct component version with working directory")

		fs, err := filesystem.NewFS(archiveFilePath, os.O_RDONLY)
		r.NoError(err, "could not create test filesystem")
		archive := ctf.NewFileSystemCTF(fs)
		helperRepo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(archive)))
		r.NoError(err, "could not create helper test repository")

		for _, identity := range []runtime.Identity{{
			descriptor.IdentityAttributeName:    "ocm.software/a",
			descriptor.IdentityAttributeVersion: "1.0.0",
		}, {
			descriptor.IdentityAttributeName:    "ocm.software/b",
			descriptor.IdentityAttributeVersion: "1.0.0",
		}, {
			descriptor.IdentityAttributeName:    "ocm.software/external",
			descriptor.IdentityAttributeVersion: "1.0.0",
		}} {
			t.Run(identity.String(), func(t *testing.T) {
				r := require.New(t)
				_, err := helperRepo.GetComponentVersion(t.Context(),
					identity[descriptor.IdentityAttributeName],
					identity[descriptor.IdentityAttributeVersion],
				)
				r.NoError(err, "could not retrieve component version from test repository")
			})
		}
	})
}

// Test_Add_Component_Version_Formats tests the different output formats for the add cv command
func Test_Add_Component_Version_Formats(t *testing.T) {
	r := require.New(t)
	tests := []struct {
		name           string
		outputArg      string
		expectedOutput string
		expectedError  bool
	}{
		{
			name: "Default Options (Table)",
			expectedOutput: ` COMPONENT                │ VERSION │ PROVIDER     
──────────────────────────┼─────────┼──────────────
 ocm.software/examples-01 │ 1.0.0   │ ocm.software 
`,
			expectedError: false,
		},
		{
			name:      "YAML output",
			outputArg: "--output=yaml",
			expectedOutput: `
- component:
    componentReferences: null
    name: ocm.software/examples-01
    provider: ocm.software
    repositoryContexts: null
    resources:
    - access:
        localReference: sha256:c3ab8ff13720e8ad9047dd39466b3c8974e592c2fa383d4a3960714caef0c4f2
        mediaType: text/plain; charset=utf-8
        type: localBlob/v1
      digest:
        hashAlgorithm: SHA-256
        normalisationAlgorithm: genericBlobDigest/v1
        value: c3ab8ff13720e8ad9047dd39466b3c8974e592c2fa383d4a3960714caef0c4f2
      name: my-file
      relation: local
      type: blob
      version: 1.0.0
    sources: null
    version: 1.0.0
  meta:
    schemaVersion: v2

`,
			expectedError: false,
		},
		{
			name:           "JSON output",
			outputArg:      "--output=json",
			expectedOutput: "", // JSON output is handled differently
			expectedError:  false,
		},
		{
			name:           "NDJSON output",
			outputArg:      "--output=ndjson",
			expectedOutput: "", // JSON output is handled differently
			expectedError:  false,
		},
		{
			name:      "tree output",
			outputArg: "--output=tree",
			expectedOutput: ` NESTING  COMPONENT                 VERSION  PROVIDER      IDENTITY                                    
 └─       ocm.software/examples-01  1.0.0    ocm.software  name=ocm.software/examples-01,version=1.0.0`,
			expectedError: false,
		},
		{
			name:           "Invalid output format",
			outputArg:      "--output=invalid",
			expectedOutput: "",
			expectedError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()

			// Create a test file to be added to the component version
			testFilePath := filepath.Join(tmp, "test-file.txt")
			r.NoError(os.WriteFile(testFilePath, []byte("foobar"), 0o600), "could not create test file")

			constructorYAML := fmt.Sprintf(`
name: ocm.software/examples-01
version: 1.0.0
provider:
  name: ocm.software
resources:
  - name: my-file
    type: blob
    input:
      type: file/v1
      path: %[1]s
`, testFilePath)

			constructorYAMLFilePath := filepath.Join(tmp, "component-constructor.yaml")
			r.NoError(os.WriteFile(constructorYAMLFilePath, []byte(constructorYAML), 0o600))

			archiveFilePath := filepath.Join(tmp, "transport-archive")
			r := require.New(t)
			logs := test.NewJSONLogReader()
			result := new(bytes.Buffer)
			args := []string{"add", "cv",
				"--constructor", constructorYAMLFilePath,
				"--repository", archiveFilePath}
			if tt.outputArg != "" {
				args = append(args, tt.outputArg)
			}
			_, err := test.OCM(t, test.WithArgs(args...), test.WithErrorOutput(logs), test.WithOutput(result))

			if tt.expectedError {
				r.Error(err, "expected error but got none")
				return
			}

			r.NoError(err, "failed to run command")

			if tt.outputArg == "--output=json" || tt.outputArg == "--output=ndjson" {
				// Handle JSON output separately
				var resultJSON any
				decoder := json.NewDecoder(result)
				r.NoError(decoder.Decode(&resultJSON), "failed to decode result JSON")

				component := map[string]any{
					"component": map[string]any{
						"componentReferences": nil,
						"name":                "ocm.software/examples-01",
						"provider":            "ocm.software",
						"repositoryContexts":  nil,
						"resources": []any{
							map[string]any{
								"name":     "my-file",
								"type":     "blob",
								"version":  "1.0.0",
								"relation": "local",
								"digest": map[string]any{
									"hashAlgorithm":          "SHA-256",
									"normalisationAlgorithm": "genericBlobDigest/v1",
									"value":                  "c3ab8ff13720e8ad9047dd39466b3c8974e592c2fa383d4a3960714caef0c4f2",
								},
								"access": map[string]any{
									"type":           "localBlob/v1",
									"mediaType":      "text/plain; charset=utf-8",
									"localReference": "sha256:c3ab8ff13720e8ad9047dd39466b3c8974e592c2fa383d4a3960714caef0c4f2",
								},
							},
						},
						"sources": nil,
						"version": "1.0.0",
					},
					"meta": map[string]any{
						"schemaVersion": "v2",
					},
				}

				switch {
				case strings.Contains(tt.outputArg, "--output=json"):
					r.EqualValues([]any{component}, resultJSON)
				case strings.Contains(tt.outputArg, "--output=ndjson"):
					r.EqualValues(component, resultJSON)
				}
			}

			logEntries, err := logs.List()
			r.NoError(err, "failed to list log entries")
			r.NotEmpty(logEntries, "expected log entries to be present")

			r.EqualValues(strings.TrimSpace(tt.expectedOutput), strings.TrimSpace(result.String()), "expected output")
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

func Test_Download_Resource(t *testing.T) {
	r := require.New(t)
	tmp := t.TempDir()

	// Create a test file to be added to the component version
	testFilePath := filepath.Join(tmp, "test-file.txt")
	r.NoError(os.WriteFile(testFilePath, []byte("foobar"), 0o600), "could not create test file")

	constructorYAML := fmt.Sprintf(`
name: ocm.software/examples-01
version: 1.0.0
provider:
  name: ocm.software
resources:
  - name: my-file
    type: blob
    input:
      type: file/v1
      path: %[1]s
`, testFilePath)

	constructorYAMLFilePath := filepath.Join(tmp, "component-constructor.yaml")
	r.NoError(os.WriteFile(constructorYAMLFilePath, []byte(constructorYAML), 0o600))

	archiveFilePath := filepath.Join(tmp, "transport-archive")

	_, err := test.OCM(t, test.WithArgs("add", "cv",
		"--constructor", constructorYAMLFilePath,
		"--repository", archiveFilePath,
	))

	r.NoError(err, "could not construct component version")

	logs := test.NewJSONLogReader()

	downloadTarget := filepath.Join(t.TempDir(), "downloaded-resource.txt")

	_, err = test.OCM(t, test.WithArgs("download", "resource",
		archiveFilePath+"//ocm.software/examples-01:1.0.0",
		"--identity", "name=my-file,version=1.0.0",
		"--output", downloadTarget),
		test.WithOutput(logs),
	)
	r.NoError(err, "failed to run download resource command")

	downloaded, err := os.ReadFile(downloadTarget)
	r.NoError(err, "failed to read downloaded resource file")
	r.Equal("foobar", string(downloaded), "expected downloaded resource content to match test file content")
}

func Test_Sign_And_Verify_Component_Version(t *testing.T) {
	r := require.New(t)
	tmp := t.TempDir()

	name, version := "ocm.software/examples-01", "1.0.0"
	// Create a test file to be added to the component version
	testFilePath := filepath.Join(tmp, "test-file.txt")
	r.NoError(os.WriteFile(testFilePath, []byte("foobar"), 0o600), "could not create test file")

	constructorYAML := fmt.Sprintf(`
name: %[1]s
version: %[2]s
provider:
  name: ocm.software
resources:
  - name: my-secure-resource
    type: blob
    input:
      type: utf8/v1
      text: "I want to be signed"
`, name, version, testFilePath)

	constructorYAMLFilePath := filepath.Join(tmp, "component-constructor.yaml")
	r.NoError(os.WriteFile(constructorYAMLFilePath, []byte(constructorYAML), 0o600))

	archiveFilePath := filepath.Join(tmp, "transport-archive")

	_, err := test.OCM(t, test.WithArgs("add", "cv",
		"--constructor", constructorYAMLFilePath,
		"--repository", archiveFilePath,
	))

	r.NoError(err, "could not construct component version")

	logs := test.NewJSONLogReader()

	signatureName := "test-signature"
	aKey := mustKey(t)
	cert := mustSelfSigned(t, "CN=signer", aKey)
	privateKeyPath, publicKeyChainPath := writeKeyAndChain(t, t.TempDir(), aKey, cert)

	ocmConfigYAML := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: RSA/v1alpha1
      algorithm: RSASSA-PSS
      signature: %[1]s
    credentials:
    - type: Credentials/v1
      properties:
        public_key_pem_file: %[2]s
        private_key_pem_file: %[3]s
`, signatureName, publicKeyChainPath, privateKeyPath)

	ocmConfigFilePath := filepath.Join(tmp, "ocm-config.yaml")
	r.NoError(os.WriteFile(ocmConfigFilePath, []byte(ocmConfigYAML), 0o600))

	reference := archiveFilePath + "//" + name + ":" + version

	_, err = test.OCM(t, test.WithArgs("sign", "component-version",
		reference,
		"--signature", signatureName,
		"--config", ocmConfigFilePath),
		test.WithOutput(logs),
	)
	r.NoError(err, "failed to sign component version")

	_, err = test.OCM(t, test.WithArgs("verify", "component-version",
		reference,
		"--signature", signatureName,
		"--config", ocmConfigFilePath),
		test.WithOutput(logs),
	)
	r.NoError(err, "failed to verify component version")
}

// Test_Add_Component_Version_Docker_Credentials tests the use of docker credentials in the add cv command
func Test_Add_Component_Version_Docker_Credentials(t *testing.T) {
	tmp := t.TempDir()

	// Create a test file to be added to the component version
	testFilePath := filepath.Join(tmp, "test-file.txt")
	require.NoError(t, os.WriteFile(testFilePath, []byte("foobar"), 0o600), "could not create test file")

	constructorYAML := fmt.Sprintf(`
name: ocm.software/examples-docker-creds
version: 1.0.0
provider:
  name: ocm.software
resources:
  - name: my-file
    type: blob
    input:
      type: file/v1
      path: %[1]s
`, testFilePath)

	constructorPath := filepath.Join(tmp, "component-constructor.yaml")
	require.NoError(t, os.WriteFile(constructorPath, []byte(constructorYAML), 0o600))

	t.Run("valid credentials", func(t *testing.T) {
		r := require.New(t)
		// Create a dummy docker config file
		dockerConfigContent := `{
		"auths": {
			"localhost": {
				"auth": "dXNlcm5hbWU6cGFzc3dvcmQ="
			}
		}
	}`
		dockerConfigPath := filepath.Join(tmp, "docker-config-valid.json")
		r.NoError(os.WriteFile(dockerConfigPath, []byte(dockerConfigContent), 0o600))

		// Create OCM config file referencing the docker config
		ocmConfigContent := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  repositories:
  - repository:
      type: DockerConfig/v1
      dockerConfigFile: %[1]s
      propagateConsumerIdentity: true
`, dockerConfigPath)
		ocmConfigPath := filepath.Join(tmp, "ocm-config-valid.yaml")
		r.NoError(os.WriteFile(ocmConfigPath, []byte(ocmConfigContent), 0o600))

		// Run command with config
		logs := test.NewJSONLogReader()
		_, err := test.OCM(t, test.WithArgs("add", "cv",
			"--constructor", constructorPath,
			"--repository", "localhost:12345/test-repo",
			"--config", ocmConfigPath,
		), test.WithErrorOutput(logs))

		// We expect an error because localhost:12345 is not reachable, but we should not see the "missing hostname" error
		r.Error(err)
		r.NotContains(err.Error(), "missing \"hostname\" in identity", "should not fail with missing hostname in identity")
	})

	t.Run("missing credentials in docker config", func(t *testing.T) {
		r := require.New(t)
		// Create a dummy docker config file with unrelated auths
		dockerConfigContent := `{
		"auths": {
			"otherhost": {
				"auth": "dXNlcm5hbWU6cGFzc3dvcmQ="
			}
		}
	}`
		dockerConfigPath := filepath.Join(tmp, "docker-config-missing.json")
		r.NoError(os.WriteFile(dockerConfigPath, []byte(dockerConfigContent), 0o600))

		// Create OCM config file referencing the docker config
		ocmConfigContent := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  repositories:
  - repository:
      type: DockerConfig/v1
      dockerConfigFile: %[1]s
      propagateConsumerIdentity: true
`, dockerConfigPath)
		ocmConfigPath := filepath.Join(tmp, "ocm-config-missing.yaml")
		r.NoError(os.WriteFile(ocmConfigPath, []byte(ocmConfigContent), 0o600))

		// Run command with config
		logs := test.NewJSONLogReader()
		_, err := test.OCM(t, test.WithArgs("add", "cv",
			"--constructor", constructorPath,
			"--repository", "localhost:12345/test-repo",
			"--config", ocmConfigPath,
		), test.WithErrorOutput(logs))

		// We expect an error because localhost:12345 is not reachable.
		// ResolveV1DockerConfigCredentials should return nil, nil
		// and the process should continue to try to connect (and fail).
		r.Error(err)
		r.NotContains(err.Error(), "missing \"hostname\" in identity", "should not fail with missing hostname in identity")
		r.NotErrorIs(err, credentials.ErrNoIndirectCredentials, "should not fail with ErrNoIndirectCredentials")
	})

	t.Run("invalid inline docker config", func(t *testing.T) {
		r := require.New(t)
		// Create OCM config file referencing invalid inline docker config
		ocmConfigContent := `
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  repositories:
  - repository:
      type: DockerConfig/v1
      dockerConfig: "{ invalid json"
      propagateConsumerIdentity: true
`
		ocmConfigPath := filepath.Join(tmp, "ocm-config-invalid-inline.yaml")
		r.NoError(os.WriteFile(ocmConfigPath, []byte(ocmConfigContent), 0o600))

		// Run command with config
		logs := test.NewJSONLogReader()
		_, err := test.OCM(t, test.WithArgs("add", "cv",
			"--constructor", constructorPath,
			"--repository", "localhost:12345/test-repo",
			"--config", ocmConfigPath,
		), test.WithErrorOutput(logs))

		// We expect an error due to invalid JSON configuration
		r.Error(err)
		r.NotContains(err.Error(), "missing \"hostname\" in identity", "should not fail with missing hostname in identity")
		r.Contains(err.Error(), "failed to create inline config store", "should fail with inline config creation error")
		r.ErrorIs(err, credentials.ErrUnknown, "should fail with ErrUnknown from inline config creation")
	})
}

func mustKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return k
}

func mustSelfSigned(t *testing.T, cn string, key *rsa.PrivateKey) *x509.Certificate {
	t.Helper()
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          n,
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}

func writeKeyAndChain(t *testing.T, dir string, priv *rsa.PrivateKey, chain ...*x509.Certificate) (privPath, chainPath string) {
	t.Helper()
	privPath = filepath.Join(dir, "key.pem")
	writePEMFile(t, privPath, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(priv))
	chainPath = writeCertsPEM(t, dir, "chain.pem", chain...)
	return
}

func writePEMFile(t *testing.T, path, typ string, der []byte) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), 0o600))
}

func writeCertsPEM(t *testing.T, dir, name string, certs ...*x509.Certificate) string {
	t.Helper()
	var b []byte
	for _, c := range certs {
		b = append(b, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})...)
	}
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, b, 0o600))
	return p
}
