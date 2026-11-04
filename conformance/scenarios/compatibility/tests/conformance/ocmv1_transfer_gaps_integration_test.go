package conformance

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Known gaps in how OCM v2 transfers resources that OCM v1 created.
// Every case is linked to its issue with knownGap and asserts the
// behaviour v2 shows today, so the fix turns it into a regular case.

const (
	ocmv1S3VersionedComponentName = "ocm.software/ocmv1-compat-s3-versioned"
	ocmv1HelmComponentName        = "ocm.software/ocmv1-compat-helm-repository"
	ocmv1HelmChartName            = "sample"
	ocmv1HelmChartVersion         = "0.1.0"
)

// Test_Integration_OCMv1VersionedS3Access covers the versioned s3/v1 spelling.
// The default local blob uploader does not select it, so --copy-resources
// leaves the resource by reference and an air-gapped target cannot resolve it.
func Test_Integration_OCMv1VersionedS3Access(t *testing.T) {
	r := require.New(t)
	knownGap(t, 1404)

	v1Config := newCompatibilityConfig(t)
	repository := filepath.Join(t.TempDir(), "ctf")
	// The object is deliberately not served, so digest generation is skipped.
	addOCMv1ComponentVersion(t, v1Config, repository, fmt.Sprintf(`name: %s
version: %s
provider:
  name: ocm.software
resources:
  - name: s3-versioned
    version: %s
    type: blob
    access:
      type: s3/v1
      region: eu-central-1
      bucket: sample-bucket
      key: sample/object.txt
`, ocmv1S3VersionedComponentName, ocmv1Version, ocmv1Version), "--skip-digest-generation")

	configPath := newCompatibilityConfig(t)
	reference := "ctf::" + repository + "//" + ocmv1S3VersionedComponentName + ":" + ocmv1Version
	targetPath := filepath.Join(t.TempDir(), "transferred")
	runOCMv2Command(t, configPath, "transfer", "cv", "--copy-resources", reference, "ctf::"+targetPath)

	target := readCompatDescriptor(t, configPath, "ctf::"+targetPath+"//"+ocmv1S3VersionedComponentName+":"+ocmv1Version)
	r.Len(target.Component.Resources, 1)
	r.Equal("s3/v1", target.Component.Resources[0].Access["type"],
		"the resource stays by reference instead of being copied as a local blob")
}

// Test_Integration_OCMv1HelmRepositoryCopy covers a v1-signed Helm resource
// from an HTTP chart repository. --copy-resources converts the chart to an OCI
// artifact and replaces the signed genericBlobDigest/v1 with an
// ociArtifactDigest/v1, so the target can no longer be verified.
func Test_Integration_OCMv1HelmRepositoryCopy(t *testing.T) {
	r := require.New(t)
	knownGap(t, 1368)

	v1Config := newCompatibilityConfig(t)
	repositoryURL, chartDigest := newHelmChartRepository(t)
	privateKey, publicKey := newRSAKeyPair(t)

	repository := filepath.Join(t.TempDir(), "ctf")
	addOCMv1ComponentVersion(t, v1Config, repository, fmt.Sprintf(`name: %s
version: %s
provider:
  name: ocm.software
resources:
  - name: chart
    version: %s
    type: helmChart
    access:
      type: helm
      helmRepository: %s
      helmChart: %s:%s
`, ocmv1HelmComponentName, ocmv1Version, ocmv1Version, repositoryURL, ocmv1HelmChartName, ocmv1HelmChartVersion))

	v1Binary := os.Getenv("OCM_V1_BIN")
	v1Reference := repository + "//" + ocmv1HelmComponentName + ":" + ocmv1Version
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "sign", "cv", "--signature", ocmv1SignatureName, "--private-key", privateKey, v1Reference)

	configPath := newVerificationConfig(t, ocmv1SignatureName, publicKey)
	reference := "ctf::" + v1Reference
	source := readCompatDescriptor(t, configPath, reference)
	r.Len(source.Component.Resources, 1)
	r.Equal("genericBlobDigest/v1", source.Component.Resources[0].Digest["normalisationAlgorithm"])
	r.Equal(chartDigest, source.Component.Resources[0].Digest["value"], "v1 digests the chart archive as served")
	runOCMv2Command(t, configPath, "verify", "cv", reference)

	targetPath := filepath.Join(t.TempDir(), "transferred")
	runOCMv2Command(t, configPath, "transfer", "cv", "--copy-resources", reference, "ctf::"+targetPath)

	targetReference := "ctf::" + targetPath + "//" + ocmv1HelmComponentName + ":" + ocmv1Version
	target := readCompatDescriptor(t, configPath, targetReference)
	r.Len(target.Component.Resources, 1)
	r.Equal("ociArtifactDigest/v1", target.Component.Resources[0].Digest["normalisationAlgorithm"],
		"the transfer rewrites the signed digest")
	r.NotEqual(source.Component.Resources[0].Digest, target.Component.Resources[0].Digest)

	output := runFailingOCMv2Command(t, configPath, "verify", "cv", targetReference)
	r.Contains(output, "digest mismatch")
}

// newHelmChartRepository serves a minimal chart repository over HTTP and
// returns its base URL and the SHA-256 of the chart archive.
func newHelmChartRepository(t *testing.T) (string, string) {
	t.Helper()
	r := require.New(t)

	directory := t.TempDir()
	archiveName := fmt.Sprintf("%s-%s.tgz", ocmv1HelmChartName, ocmv1HelmChartVersion)
	files := []struct {
		name    string
		content string
	}{
		{
			name: ocmv1HelmChartName + "/Chart.yaml",
			content: fmt.Sprintf("apiVersion: v2\nname: %s\nversion: %s\ntype: application\n",
				ocmv1HelmChartName, ocmv1HelmChartVersion),
		},
		{name: ocmv1HelmChartName + "/values.yaml", content: "replicaCount: 1\n"},
		{
			name:    ocmv1HelmChartName + "/templates/configmap.yaml",
			content: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: sample\ndata:\n  key: value\n",
		},
	}

	archive := &bytes.Buffer{}
	compressor := gzip.NewWriter(archive)
	writer := tar.NewWriter(compressor)
	for _, file := range files {
		r.NoError(writer.WriteHeader(&tar.Header{Name: file.name, Mode: 0o600, Size: int64(len(file.content))}))
		_, err := io.WriteString(writer, file.content)
		r.NoError(err)
	}
	r.NoError(writer.Close())
	r.NoError(compressor.Close())
	r.NoError(os.WriteFile(filepath.Join(directory, archiveName), archive.Bytes(), 0o600))

	server := httptest.NewServer(http.FileServer(http.Dir(directory)))
	t.Cleanup(server.Close)

	digest := sha256.Sum256(archive.Bytes())
	chartDigest := hex.EncodeToString(digest[:])
	index := fmt.Sprintf(`apiVersion: v1
generated: "2024-01-01T00:00:00Z"
entries:
  %s:
  - apiVersion: v2
    name: %s
    version: %s
    type: application
    created: "2024-01-01T00:00:00Z"
    digest: %s
    urls:
    - %s/%s
`, ocmv1HelmChartName, ocmv1HelmChartName, ocmv1HelmChartVersion, chartDigest, server.URL, archiveName)
	r.NoError(os.WriteFile(filepath.Join(directory, "index.yaml"), []byte(index), 0o600))

	return server.URL, chartDigest
}
