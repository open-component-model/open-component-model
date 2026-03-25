package cmd_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/ctf"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/cli/cmd/internal/test"
)

func Test_Add_And_Transfer_Input_Type_Casing(t *testing.T) {
	tests := []struct {
		name             string
		inputType        string
		resourceType     string
		resourceName     string
		expectedAccess   string
		setupConstructor func(t *testing.T, tmp string) string
		verifyContent    func(t *testing.T, content []byte)
		skipBlobCompare  bool
	}{
		{
			name:           "file/v1 lowercase",
			inputType:      "file/v1",
			resourceType:   "blob",
			resourceName:   "my-file",
			expectedAccess: "localBlob/v1",
			setupConstructor: func(t *testing.T, tmp string) string {
				filePath := filepath.Join(tmp, "test-file.txt")
				require.NoError(t, os.WriteFile(filePath, []byte("file-content"), 0o600))
				return fmt.Sprintf(`
name: ocm.software/test-type-casing
version: 1.0.0
provider:
  name: ocm.software
resources:
  - name: my-file
    type: blob
    input:
      type: file/v1
      path: %s
`, filePath)
			},
			verifyContent: func(t *testing.T, content []byte) {
				require.Equal(t, "file-content", string(content))
			},
		},
		{
			name:           "File/v1 upper camel case",
			inputType:      "File/v1",
			resourceType:   "blob",
			resourceName:   "my-file",
			expectedAccess: "localBlob/v1",
			setupConstructor: func(t *testing.T, tmp string) string {
				filePath := filepath.Join(tmp, "test-file.txt")
				require.NoError(t, os.WriteFile(filePath, []byte("file-content-upper"), 0o600))
				return fmt.Sprintf(`
name: ocm.software/test-type-casing
version: 1.0.0
provider:
  name: ocm.software
resources:
  - name: my-file
    type: blob
    input:
      type: File/v1
      path: %s
`, filePath)
			},
			verifyContent: func(t *testing.T, content []byte) {
				require.Equal(t, "file-content-upper", string(content))
			},
		},
		{
			name:           "dir/v1 lowercase",
			inputType:      "dir/v1",
			resourceType:   "blob",
			resourceName:   "my-dir",
			expectedAccess: "localBlob/v1",
			setupConstructor: func(t *testing.T, tmp string) string {
				dirPath := filepath.Join(tmp, "test-dir")
				require.NoError(t, os.MkdirAll(dirPath, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dirPath, "data.txt"), []byte("dir-content"), 0o600))
				return fmt.Sprintf(`
name: ocm.software/test-type-casing
version: 1.0.0
provider:
  name: ocm.software
resources:
  - name: my-dir
    type: blob
    input:
      type: dir/v1
      path: %s
`, dirPath)
			},
			verifyContent: func(t *testing.T, content []byte) {
				require.NotEmpty(t, content, "dir content should not be empty (tar archive)")
			},
		},
		{
			name:           "Dir/v1 upper camel case",
			inputType:      "Dir/v1",
			resourceType:   "blob",
			resourceName:   "my-dir",
			expectedAccess: "localBlob/v1",
			setupConstructor: func(t *testing.T, tmp string) string {
				dirPath := filepath.Join(tmp, "test-dir")
				require.NoError(t, os.MkdirAll(dirPath, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dirPath, "data.txt"), []byte("dir-content-upper"), 0o600))
				return fmt.Sprintf(`
name: ocm.software/test-type-casing
version: 1.0.0
provider:
  name: ocm.software
resources:
  - name: my-dir
    type: blob
    input:
      type: Dir/v1
      path: %s
`, dirPath)
			},
			verifyContent: func(t *testing.T, content []byte) {
				require.NotEmpty(t, content, "dir content should not be empty (tar archive)")
			},
		},
		{
			name:           "utf8/v1 lowercase",
			inputType:      "utf8/v1",
			resourceType:   "blob",
			resourceName:   "my-text",
			expectedAccess: "localBlob/v1",
			setupConstructor: func(t *testing.T, _ string) string {
				return `
name: ocm.software/test-type-casing
version: 1.0.0
provider:
  name: ocm.software
resources:
  - name: my-text
    type: blob
    input:
      type: utf8/v1
      text: "hello from utf8 lowercase"
`
			},
			verifyContent: func(t *testing.T, content []byte) {
				require.Equal(t, "hello from utf8 lowercase", string(content))
			},
		},
		{
			name:           "UTF8/v1 upper camel case",
			inputType:      "UTF8/v1",
			resourceType:   "blob",
			resourceName:   "my-text",
			expectedAccess: "localBlob/v1",
			setupConstructor: func(t *testing.T, _ string) string {
				return `
name: ocm.software/test-type-casing
version: 1.0.0
provider:
  name: ocm.software
resources:
  - name: my-text
    type: blob
    input:
      type: UTF8/v1
      text: "hello from UTF8 upper"
`
			},
			verifyContent: func(t *testing.T, content []byte) {
				require.Equal(t, "hello from UTF8 upper", string(content))
			},
		},
		{
			name:            "helm/v1 input lowercase",
			inputType:       "helm/v1",
			resourceType:    "helmChart",
			resourceName:    "my-chart",
			expectedAccess:  "localBlob/v1",
			skipBlobCompare: true,
			setupConstructor: func(t *testing.T, _ string) string {
				chartDir, err := filepath.Abs(filepath.Join("../../bindings/go/helm/testdata/mychart"))
				require.NoError(t, err)
				return fmt.Sprintf(`
name: ocm.software/test-type-casing
version: 1.0.0
provider:
  name: ocm.software
resources:
  - name: my-chart
    type: helmChart
    input:
      type: helm/v1
      path: %s
`, chartDir)
			},
			verifyContent: func(t *testing.T, content []byte) {
				require.NotEmpty(t, content, "helm chart content should not be empty")
			},
		},
		{
			name:            "Helm/v1 input upper camel case",
			inputType:       "Helm/v1",
			resourceType:    "helmChart",
			resourceName:    "my-chart",
			expectedAccess:  "localBlob/v1",
			skipBlobCompare: true,
			setupConstructor: func(t *testing.T, _ string) string {
				chartDir, err := filepath.Abs(filepath.Join("../../bindings/go/helm/testdata/mychart"))
				require.NoError(t, err)
				return fmt.Sprintf(`
name: ocm.software/test-type-casing
version: 1.0.0
provider:
  name: ocm.software
resources:
  - name: my-chart
    type: helmChart
    input:
      type: Helm/v1
      path: %s
`, chartDir)
			},
			verifyContent: func(t *testing.T, content []byte) {
				require.NotEmpty(t, content, "helm chart content should not be empty")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			tmp := t.TempDir()

			constructorYAML := tc.setupConstructor(t, tmp)
			constructorPath := filepath.Join(tmp, "constructor.yaml")
			r.NoError(os.WriteFile(constructorPath, []byte(constructorYAML), 0o600))

			// Step 1: add cv
			sourceArchive := filepath.Join(tmp, "source-archive")
			logs := test.NewJSONLogReader()
			_, err := test.OCM(t, test.WithArgs("add", "cv",
				"--constructor", constructorPath,
				"--repository", sourceArchive,
			), test.WithErrorOutput(logs))
			r.NoError(err, "add cv failed for input type %s", tc.inputType)

			// Verify source CTF
			sourceFS, err := filesystem.NewFS(sourceArchive, os.O_RDONLY)
			r.NoError(err)
			sourceRepo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(sourceFS))))
			r.NoError(err)

			desc, err := sourceRepo.GetComponentVersion(t.Context(), "ocm.software/test-type-casing", "1.0.0")
			r.NoError(err, "could not get component version from source")
			r.Len(desc.Component.Resources, 1)
			r.Equal(tc.resourceName, desc.Component.Resources[0].Name)
			r.Equal(tc.resourceType, desc.Component.Resources[0].Type)
			r.Equal(tc.expectedAccess, desc.Component.Resources[0].Access.GetType().String())

			// Verify content from source
			sourceBlob, _, err := sourceRepo.GetLocalResource(t.Context(), "ocm.software/test-type-casing", "1.0.0", desc.Component.Resources[0].ToIdentity())
			r.NoError(err, "could not get local resource from source")
			var sourceBuf bytes.Buffer
			r.NoError(blob.Copy(&sourceBuf, sourceBlob))
			tc.verifyContent(t, sourceBuf.Bytes())

			// Step 2: transfer cv
			targetArchive := filepath.Join(tmp, "target-archive")
			sourceRef := fmt.Sprintf("ctf::%s//ocm.software/test-type-casing:1.0.0", sourceArchive)
			targetRef := fmt.Sprintf("ctf::%s", targetArchive)

			transferLogs := test.NewJSONLogReader()
			_, err = test.OCM(t, test.WithArgs("transfer", "component-version", sourceRef, targetRef),
				test.WithErrorOutput(transferLogs))
			r.NoError(err, "transfer cv failed for input type %s", tc.inputType)

			// Verify transfer logs contain success
			transferEntries, err := transferLogs.List()
			r.NoError(err)
			found := false
			for _, entry := range transferEntries {
				if strings.Contains(fmt.Sprint(entry), "transfer completed successfully") {
					found = true
					break
				}
			}
			r.True(found, "expected transfer success log for input type %s", tc.inputType)

			// Verify target CTF
			targetFS, err := filesystem.NewFS(targetArchive, os.O_RDONLY)
			r.NoError(err)
			targetRepo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(targetFS))))
			r.NoError(err)

			targetDesc, err := targetRepo.GetComponentVersion(t.Context(), "ocm.software/test-type-casing", "1.0.0")
			r.NoError(err, "could not get component version from target")
			r.Len(targetDesc.Component.Resources, 1)
			r.Equal(tc.resourceName, targetDesc.Component.Resources[0].Name)
			r.Equal(tc.resourceType, targetDesc.Component.Resources[0].Type)
			r.Equal(tc.expectedAccess, targetDesc.Component.Resources[0].Access.GetType().String())

			// Verify content from target matches source
			targetBlob, _, err := targetRepo.GetLocalResource(t.Context(), "ocm.software/test-type-casing", "1.0.0", targetDesc.Component.Resources[0].ToIdentity())
			r.NoError(err, "could not get local resource from target")
			var targetBuf bytes.Buffer
			r.NoError(blob.Copy(&targetBuf, targetBlob))
			if tc.skipBlobCompare {
				r.NotEmpty(targetBuf.Bytes(), "target blob should not be empty for type %s", tc.inputType)
			} else {
				r.Equal(sourceBuf.Bytes(), targetBuf.Bytes(), "transferred content should match source for type %s", tc.inputType)
			}
		})
	}
}
