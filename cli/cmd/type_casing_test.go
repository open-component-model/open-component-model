package cmd_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/ctf"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/cli/cmd/internal/test"
)

func Test_Add_Input_Type_Casing(t *testing.T) {
	tests := []struct {
		name           string
		aliases        []string
		resourceType   string
		resourceName   string
		expectedAccess string
		setupInput     func(t *testing.T, tmp string) (yamlFragment string)
		verifyContent  func(t *testing.T, content []byte)
	}{
		{
			name:           "file",
			aliases:        []string{"file/v1", "File/v1"},
			resourceType:   "blob",
			resourceName:   "my-file",
			expectedAccess: "LocalBlob/v1",
			setupInput: func(t *testing.T, tmp string) string {
				filePath := filepath.Join(tmp, "test-file.txt")
				require.NoError(t, os.WriteFile(filePath, []byte("file-content"), 0o600))
				return fmt.Sprintf("path: %s", filePath)
			},
			verifyContent: func(t *testing.T, content []byte) {
				require.Equal(t, "file-content", string(content))
			},
		},
		{
			name:           "dir",
			aliases:        []string{"dir/v1", "Dir/v1"},
			resourceType:   "blob",
			resourceName:   "my-dir",
			expectedAccess: "LocalBlob/v1",
			setupInput: func(t *testing.T, tmp string) string {
				dirPath := filepath.Join(tmp, "test-dir")
				require.NoError(t, os.MkdirAll(dirPath, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dirPath, "data.txt"), []byte("dir-content"), 0o600))
				return fmt.Sprintf("path: %s", dirPath)
			},
			verifyContent: func(t *testing.T, content []byte) {
				require.NotEmpty(t, content)
			},
		},
		{
			name:           "utf8",
			aliases:        []string{"utf8/v1", "UTF8/v1"},
			resourceType:   "blob",
			resourceName:   "my-text",
			expectedAccess: "LocalBlob/v1",
			setupInput: func(t *testing.T, _ string) string {
				return `text: "hello utf8"`
			},
			verifyContent: func(t *testing.T, content []byte) {
				require.Equal(t, "hello utf8", string(content))
			},
		},
		{
			name:           "helm",
			aliases:        []string{"helm/v1", "Helm/v1"},
			resourceType:   "helmChart",
			resourceName:   "my-chart",
			expectedAccess: "LocalBlob/v1",
			setupInput: func(t *testing.T, tmp string) string {
				chartDir := filepath.Join(tmp, "mychart")
				require.NoError(t, os.MkdirAll(filepath.Join(chartDir, "templates"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(chartDir, "Chart.yaml"),
					[]byte("name: mychart\nversion: 0.1.0\n"), 0o600))
				require.NoError(t, os.WriteFile(filepath.Join(chartDir, "templates", "pod.yaml"),
					[]byte("apiVersion: v1\nkind: Pod\nmetadata:\n  name: test\nspec:\n  containers:\n  - image: busybox\n    name: test\n"), 0o600))
				return fmt.Sprintf("path: %s", chartDir)
			},
			verifyContent: func(t *testing.T, content []byte) {
				require.NotEmpty(t, content)
			},
		},
	}

	for _, tc := range tests {
		for _, alias := range tc.aliases {
			t.Run(alias, func(t *testing.T) {
				r := require.New(t)
				tmp := t.TempDir()

				inputFragment := tc.setupInput(t, tmp)
				constructorYAML := fmt.Sprintf(`
name: ocm.software/test-type-casing
version: 1.0.0
provider:
  name: ocm.software
resources:
  - name: %s
    type: %s
    input:
      type: %s
      %s
`, tc.resourceName, tc.resourceType, alias, inputFragment)

				constructorPath := filepath.Join(tmp, "constructor.yaml")
				r.NoError(os.WriteFile(constructorPath, []byte(constructorYAML), 0o600))

				archive := filepath.Join(tmp, "archive")
				_, err := test.OCM(t, test.WithArgs("add", "cv",
					"--constructor", constructorPath,
					"--repository", archive,
				), test.WithErrorOutput(test.NewJSONLogReader()))
				r.NoError(err)

				fs, err := filesystem.NewFS(archive, os.O_RDONLY)
				r.NoError(err)
				repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))))
				r.NoError(err)

				desc, err := repo.GetComponentVersion(t.Context(), "ocm.software/test-type-casing", "1.0.0")
				r.NoError(err)
				r.Len(desc.Component.Resources, 1)
				r.Equal(tc.resourceName, desc.Component.Resources[0].Name)
				r.Equal(tc.resourceType, desc.Component.Resources[0].Type)
				r.Equal(tc.expectedAccess, desc.Component.Resources[0].Access.GetType().String())

				b, _, err := repo.GetLocalResource(t.Context(), "ocm.software/test-type-casing", "1.0.0", desc.Component.Resources[0].ToIdentity())
				r.NoError(err)
				var buf bytes.Buffer
				r.NoError(blob.Copy(&buf, b))
				tc.verifyContent(t, buf.Bytes())
			})
		}
	}
}
