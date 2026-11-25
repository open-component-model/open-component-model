package integration

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/registry"

	"ocm.software/open-component-model/bindings/go/cli/cmd"
	"ocm.software/open-component-model/bindings/go/cli/integration/internal"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

const (
	helmDigestChartFile   = "mychart-0.1.0.tgz"
	helmDigestResourceRef = "name=mychart,version=0.1.0"
)

// Test_Integration_Transfer_HelmChart_SignedCopyKeepsDigest copies a signed Helm chart from an
// HTTP chart repository into a CTF. The signature covers the genericBlobDigest/v1 digest of the
// chart archive, so the copy must keep that digest and store the archive as it is.
func Test_Integration_Transfer_HelmChart_SignedCopyKeepsDigest(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	chartDir := filepath.Join(getRepoRootBasedOnGit(t), "bindings/go/helm/testdata/provenance")
	chartBytes, err := os.ReadFile(filepath.Join(chartDir, helmDigestChartFile))
	r.NoError(err, "test helm chart should exist")
	srv := newIndexedHelmChartRepoServer(t, chartDir)
	cfgPath := writeSigningConfig(t, nil)

	name, version := "ocm.software/helm-digest-signed", "v1.0.0"
	tempDir := t.TempDir()
	constructorPath := filepath.Join(tempDir, "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(fmt.Sprintf(`components:
- name: %s
  version: %s
  provider:
    name: ocm.software
  resources:
%s`, name, version, helmDigestChartResource(srv.URL))), 0o600))

	srcCTF := filepath.Join(tempDir, "source-ctf")
	requireOCM(t, "creating the source component version should succeed",
		"add", "component-version",
		"--repository", "ctf::"+srcCTF,
		"--constructor", constructorPath,
		"--config", cfgPath,
	)

	src := getCTFComponentVersion(t, srcCTF, name, version)
	r.Len(src.Component.Resources, 1)
	r.NotNil(src.Component.Resources[0].Digest, "the source resource should carry a digest")
	r.Equal("genericBlobDigest/v1", src.Component.Resources[0].Digest.NormalisationAlgorithm)
	r.Equal("SHA-256", src.Component.Resources[0].Digest.HashAlgorithm)

	srcRef := fmt.Sprintf("ctf::%s//%s:%s", srcCTF, name, version)
	requireOCM(t, "signing the source component version should succeed", "sign", "cv", srcRef, "--config", cfgPath)
	requireOCM(t, "verifying the source component version should succeed", "verify", "cv", srcRef, "--config", cfgPath)

	targetCTF := filepath.Join(tempDir, "target-ctf")
	requireOCM(t, "copying the signed chart should succeed",
		"transfer", "component-version", srcRef, "ctf::"+targetCTF,
		"--config", cfgPath,
		"--copy-resources",
	)

	targetRef := fmt.Sprintf("ctf::%s//%s:%s", targetCTF, name, version)
	requireOCM(t, "verifying the copied component version should succeed", "verify", "cv", targetRef, "--config", cfgPath)

	tgt := getCTFComponentVersion(t, targetCTF, name, version)
	r.Len(tgt.Component.Resources, 1)
	r.Equal(src.Component.Resources[0].Digest, tgt.Component.Resources[0].Digest, "the copy should keep the signed digest")
	r.Equal(src.Component.Resources[0].Type, tgt.Component.Resources[0].Type, "the copy should keep the resource type")
	requireHelmChartLocalBlob(t, tgt.Component.Resources[0].Access)
	r.Equal(chartBytes, downloadRawResource(t, targetRef, cfgPath), "the copy should store the chart archive unchanged")
}

// Test_Integration_Transfer_HelmChart_RecursiveCopyKeepsReferenceDigest copies an unsigned parent
// and its child with a Helm chart twice. The parent records the digest of the child descriptor,
// so a copy that changes the chart digest breaks the parent, and the second transfer, which
// checks that digest, fails.
func Test_Integration_Transfer_HelmChart_RecursiveCopyKeepsReferenceDigest(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	chartDir := filepath.Join(getRepoRootBasedOnGit(t), "bindings/go/helm/testdata/provenance")
	srv := newIndexedHelmChartRepoServer(t, chartDir)
	cfgPath := writeSigningConfig(t, nil)

	parent, child, version := "ocm.software/helm-digest-parent", "ocm.software/helm-digest-child", "v1.0.0"
	tempDir := t.TempDir()
	constructorPath := filepath.Join(tempDir, "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(fmt.Sprintf(`components:
- name: %[1]s
  version: %[3]s
  provider:
    name: ocm.software
  resources:
%[4]s
- name: %[2]s
  version: %[3]s
  provider:
    name: ocm.software
  componentReferences:
  - name: child
    componentName: %[1]s
    version: %[3]s
`, child, parent, version, helmDigestChartResource(srv.URL))), 0o600))

	srcCTF := filepath.Join(tempDir, "source-ctf")
	requireOCM(t, "creating the source component versions should succeed",
		"add", "component-version",
		"--repository", "ctf::"+srcCTF,
		"--constructor", constructorPath,
		"--config", cfgPath,
	)

	srcParent := getCTFComponentVersion(t, srcCTF, parent, version)
	srcChild := getCTFComponentVersion(t, srcCTF, child, version)
	r.Len(srcParent.Component.References, 1)
	r.Len(srcChild.Component.Resources, 1)
	r.NotEmpty(srcParent.Component.References[0].Digest.Value, "the parent should record the digest of the child")

	hop1CTF := filepath.Join(tempDir, "hop1-ctf")
	requireOCM(t, "the first recursive copy should succeed",
		"transfer", "component-version", fmt.Sprintf("ctf::%s//%s:%s", srcCTF, parent, version), "ctf::"+hop1CTF,
		"--config", cfgPath,
		"--recursive",
		"--copy-resources",
	)

	hop2CTF := filepath.Join(tempDir, "hop2-ctf")
	requireOCM(t, "the second recursive copy should succeed",
		"transfer", "component-version", fmt.Sprintf("ctf::%s//%s:%s", hop1CTF, parent, version), "ctf::"+hop2CTF,
		"--config", cfgPath,
		"--recursive",
		"--copy-resources",
	)

	parent2 := getCTFComponentVersion(t, hop2CTF, parent, version)
	child2 := getCTFComponentVersion(t, hop2CTF, child, version)
	r.Len(parent2.Component.References, 1)
	r.Len(child2.Component.Resources, 1)
	r.Equal(srcParent.Component.References[0].Digest, parent2.Component.References[0].Digest, "the copies should keep the reference digest")
	r.Equal(srcChild.Component.Resources[0].Digest, child2.Component.Resources[0].Digest, "the copies should keep the chart digest")
}

// helmDigestChartResource returns the constructor entry of the mychart resource, indented as an
// item of a resources list. Its type is not the one the conversion to OCI writes, so a copy
// that takes the type from the conversion shows.
func helmDigestChartResource(repoURL string) string {
	return fmt.Sprintf(`  - name: mychart
    version: 0.1.0
    type: helm
    access:
      type: helm/v1
      helmRepository: %s
      helmChart: "mychart:0.1.0"`, repoURL)
}

// requireOCM runs one ocm command and requires it to succeed.
func requireOCM(t *testing.T, msg string, args ...string) {
	t.Helper()
	ocm := cmd.New()
	ocm.SetArgs(args)
	require.NoError(t, ocm.ExecuteContext(t.Context()), msg)
}

// newIndexedHelmChartRepoServer serves the files of chartDir as a Helm chart repository. The
// digest processor of "ocm add cv" reads the chart digest from index.yaml, so the server
// generates one for mychart 0.1.0 from the archive in chartDir.
func newIndexedHelmChartRepoServer(t *testing.T, chartDir string) *httptest.Server {
	t.Helper()
	r := require.New(t)

	chart, err := os.ReadFile(filepath.Join(chartDir, helmDigestChartFile))
	r.NoError(err, "test helm chart should exist")
	index := fmt.Sprintf(`apiVersion: v1
entries:
  mychart:
    - urls:
        - %s
      name: mychart
      version: 0.1.0
      digest: "sha256:%x"
      apiVersion: v2
generated: "2024-01-01T00:00:00.000000000Z"
`, helmDigestChartFile, sha256.Sum256(chart))

	files := http.FileServer(http.Dir(chartDir))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/index.yaml" {
			w.Header().Set("Content-Type", "application/x-yaml")
			_, _ = w.Write([]byte(index))
			return
		}
		files.ServeHTTP(w, req)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// writeSigningConfig writes an OCM config with a fresh RSA signing key and plain HTTP, which the
// local chart server needs. With a registry it also holds the credentials for it.
func writeSigningConfig(t *testing.T, reg *internal.OCIRegistry) string {
	t.Helper()

	k := mustRSAKey(t)
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&k.PublicKey)})

	registryConsumer := ""
	if reg != nil {
		registryConsumer = fmt.Sprintf(`
  - identity:
      type: OCIRegistry
      hostname: %[1]q
      port: %[2]q
      scheme: http
    credentials:
    - type: Credentials/v1
      properties:
        username: %[3]q
        password: %[4]q`, reg.Host, reg.Port, reg.User, reg.Password)
	}

	cfg := fmt.Sprintf(`type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: RSA/v1alpha1
      algorithm: RSASSA-PSS
      signature: default
    credentials:
    - type: Credentials/v1
      properties:
        public_key_pem: %[1]q
        private_key_pem: %[2]q%[3]s
- type: http.config.ocm.software/v1alpha1
  plainHTTP: true
`, pubPEM, privPEM, registryConsumer)

	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0o600))
	return cfgPath
}

// getCTFComponentVersion reads a component version straight from a CTF directory, so the test
// sees the stored descriptor rather than the output of a CLI command.
func getCTFComponentVersion(t *testing.T, ctfPath, name, version string) *descriptor.Descriptor {
	t.Helper()
	r := require.New(t)

	archive, err := ctf.OpenCTFFromOSPath(ctfPath, os.O_RDONLY)
	r.NoError(err)
	repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(archive)))
	r.NoError(err)
	desc, err := repo.GetComponentVersion(t.Context(), name, version)
	r.NoError(err)
	return desc
}

// downloadRawResource downloads the mychart resource without extraction and returns its bytes.
func downloadRawResource(t *testing.T, componentRef, cfgPath string) []byte {
	t.Helper()

	output := filepath.Join(t.TempDir(), "resource")
	requireOCM(t, "downloading the resource should succeed",
		"download", "resource", componentRef,
		"--identity", helmDigestResourceRef,
		"--extraction-policy", "disable",
		"--output", output,
		"--config", cfgPath,
	)
	data, err := os.ReadFile(output)
	require.NoError(t, err)
	return data
}

// requireHelmChartLocalBlob requires a local blob access that holds the chart as its .tgz archive.
func requireHelmChartLocalBlob(t *testing.T, access ocmruntime.Typed) v2.LocalBlob {
	t.Helper()
	r := require.New(t)

	r.NotNil(access, "the resource should have an access")
	var localBlob v2.LocalBlob
	r.NoError(v2.Scheme.Convert(access, &localBlob), "the resource access should be a local blob")
	r.Equal(registry.ChartLayerMediaType, localBlob.MediaType, "the chart should be stored as its .tgz archive")
	return localBlob
}
