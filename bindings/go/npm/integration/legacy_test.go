package integration_test

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/npm/repository"
	accessv1 "ocm.software/open-component-model/bindings/go/npm/spec/access/v1"
)

// Test_Integration_NPM_V1PackV2Access exercises the actual legacy producer, not a
// handwritten approximation of its output. The legacy CLI packs a CTF and hashes
// each external npm resource; v2 consumes the resulting descriptor unchanged.
func Test_Integration_NPM_V1PackV2Access(t *testing.T) {
	binary := os.Getenv("OCM_V1_BINARY")
	if binary == "" {
		t.Skip("set OCM_V1_BINARY to the legacy OCM v0.50.0 executable (see README.md)")
	}
	binary, err := filepath.Abs(binary)
	require.NoError(t, err)
	home := t.TempDir()
	run := func(t *testing.T, dir string, args ...string) []byte {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "HOME="+home)
		output, err := cmd.Output()
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Logf("legacy stderr: %s", exitErr.Stderr)
		}
		require.NoError(t, err, "legacy CLI %v: %s", args, output)
		return output
	}
	require.Contains(t, string(run(t, home, "version")), "0.50.0", "use the pinned legacy release, not the v2 CLI")

	for _, tc := range []struct {
		name, typ, pkg, selector string
		file                     bool
	}{
		{name: "unversioned", typ: "npm", pkg: "public-lib", selector: version},
		{name: "scoped", typ: "npm/v1", pkg: "@ocm/scoped-lib", selector: version},
		{name: "tag", typ: "NPM", pkg: "JSONStream", selector: "latest"},
		{name: "noncanonical", typ: "NPM/v1", pkg: "_legacy+package", selector: "v1.2.3"},
		{name: "file", typ: "npm", pkg: "@ocm/file-lib", selector: "latest", file: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			work := t.TempDir()
			tarball := tgz(t, tc.pkg)
			integrity := sha512.Sum512(tarball)
			metadata := func(tarballURL string) []byte {
				body, err := json.Marshal(map[string]any{
					"name": tc.pkg, "version": version,
					"dist": map[string]string{
						"tarball":   tarballURL,
						"integrity": "sha512-" + base64.StdEncoding.EncodeToString(integrity[:]),
					},
				})
				require.NoError(t, err)
				return body
			}
			var registryURL string
			if tc.file {
				root := filepath.Join(work, "registry")
				require.NoError(t, os.MkdirAll(filepath.Join(root, tc.pkg), 0o700))
				tarballPath := filepath.Join(root, "package.tgz")
				require.NoError(t, os.WriteFile(tarballPath, tarball, 0o600))
				require.NoError(t, os.WriteFile(filepath.Join(root, tc.pkg, tc.selector), metadata("file://"+tarballPath), 0o600))
				registryURL = "file://" + root
			} else {
				mux := http.NewServeMux()
				srv := httptest.NewServer(mux)
				t.Cleanup(srv.Close)
				registryURL = srv.URL
				body := metadata(srv.URL + "/package.tgz")
				mux.HandleFunc("/"+tc.pkg+"/"+tc.selector, func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(body)
				})
				mux.HandleFunc("/package.tgz", func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write(tarball)
				})
			}

			constructor := fmt.Sprintf(`components:
- name: example.com/npm-compat
  version: "1.0.0"
  provider:
    name: example.com
  resources:
  - name: package
    version: %q
    type: npmPackage
    relation: external
    access:
      type: %q
      registry: %q
      package: %q
      version: %q
`, version, tc.typ, registryURL, tc.pkg, tc.selector)
			constructorPath := filepath.Join(work, "constructor.yaml")
			require.NoError(t, os.WriteFile(constructorPath, []byte(constructor), 0o600))
			archive := filepath.Join(work, "ctf")
			run(t, work, "add", "componentversions", "--create", "--file", archive, "--type", "directory", "--templater", "none", constructorPath)
			output := run(t, work, "get", "componentversions", "--repo", archive, "example.com/npm-compat:1.0.0", "--output", "json")

			var exported struct {
				Items []descriptorv2.Descriptor `json:"items"`
			}
			require.NoError(t, json.Unmarshal(output, &exported))
			require.Len(t, exported.Items, 1)
			cd, err := descriptor.ConvertFromV2(&exported.Items[0])
			require.NoError(t, err)
			require.Len(t, cd.Component.Resources, 1)
			res := &cd.Component.Resources[0]
			require.NotNil(t, res.Digest, "v1 must hash the resource during packing")
			require.Equal(t, "SHA-256", res.Digest.HashAlgorithm)
			require.Equal(t, "genericBlobDigest/v1", res.Digest.NormalisationAlgorithm)
			sum := sha256.Sum256(tarball)
			require.Equal(t, hex.EncodeToString(sum[:]), res.Digest.Value)

			tempDir := t.TempDir()
			repo := repository.NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &tempDir})
			var access accessv1.NPM
			require.NoError(t, repo.GetResourceRepositoryScheme().Convert(res.Access, &access))
			require.Equal(t, registryURL, access.Registry)
			require.Equal(t, tc.pkg, access.Package)
			require.Equal(t, tc.selector, access.Version, "packing must preserve the legacy selector")

			b, err := repo.DownloadResource(t.Context(), res, nil)
			require.NoError(t, err)
			rc, err := b.ReadCloser()
			require.NoError(t, err)
			got, err := io.ReadAll(rc)
			require.NoError(t, err)
			require.NoError(t, rc.Close())
			require.Equal(t, tarball, got)
			mediaType, ok := b.(blob.MediaTypeAware).MediaType()
			require.True(t, ok)
			require.Equal(t, "application/x-tgz", mediaType)

			verified, err := repo.ProcessResourceDigest(t.Context(), res, nil)
			require.NoError(t, err, "v2 must verify the digest generated by v1")
			require.Equal(t, res.Digest, verified.Digest)
		})
	}
}
