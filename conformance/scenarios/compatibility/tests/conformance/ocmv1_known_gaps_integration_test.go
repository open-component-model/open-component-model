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
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// The cases in this file pin the behaviour OCM v2 shows today for v1 data that
// is known to be mishandled. Each one is tied to an open issue under
// open-component-model/ocm-project#1366 through knownGap: when the fix merges
// the case fails here and is rewritten to assert the supported behaviour.
//
// One bug under #1366 has no case here: #1405
// (componentNameMapping: sha256-digest) needs an OCI registry, which this
// scenario does not provision.

// knownGap links a case to the issue whose fix will turn it into a regular case.
func knownGap(t *testing.T, issue int) {
	t.Helper()
	t.Logf("asserts unfixed behaviour of open-component-model/ocm-project#%d", issue)
}

const (
	ocmv1CertificateComponentName  = "ocm.software/ocmv1-compat-certificate"
	ocmv1NullLabelComponentName    = "ocm.software/ocmv1-compat-null-label"
	ocmv1LegacyNormComponentName   = "ocm.software/ocmv1-compat-legacy-normalisation"
	ocmv1MergeLabelComponentName   = "ocm.software/ocmv1-compat-merge-label"
	ocmv1LegacySchemaComponentName = "ocm.software/ocmv1-compat-legacy-schema"
	ocmv1S3VersionedComponentName  = "ocm.software/ocmv1-compat-s3-versioned"
	ocmv1HelmComponentName         = "ocm.software/ocmv1-compat-helm-repository"
	ocmv1HelmChartName             = "sample"
	ocmv1HelmChartVersion          = "0.1.0"
	ocmv1LocalOciBlobComponentName = "ocm.software/ocmv1-compat-local-oci-blob"
)

// Test_Integration_OCMv1CertificateSignature covers a v1 signature made with a
// certificate chain. v1 records the subject of the signing certificate in
// signature.issuer; v2 compares it with the issuer DN of the leaf.
func Test_Integration_OCMv1CertificateSignature(t *testing.T) {
	r := require.New(t)
	knownGap(t, 1367)

	v1Config := newCompatibilityConfig(t)
	signature := "repro-signer"
	privateKey, leafCertificate, rootCertificate := newOCMv1CertificateChain(t, signature)

	repository := filepath.Join(t.TempDir(), "ctf")
	addOCMv1ComponentVersion(t, v1Config, repository, plainTextConstructor(ocmv1CertificateComponentName))

	v1Binary := os.Getenv("OCM_V1_BIN")
	v1Reference := repository + "//" + ocmv1CertificateComponentName + ":" + ocmv1Version
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "sign", "cv", "--signature", signature,
		"--private-key", privateKey, "--public-key", leafCertificate, "--ca-cert", rootCertificate, v1Reference)
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "verify", "cv", "--signature", signature,
		"--ca-cert", rootCertificate, v1Reference)

	configPath := newVerificationConfig(t, signature, rootCertificate)
	reference := "ctf::" + v1Reference
	descriptor := readCompatDescriptor(t, configPath, reference)
	r.Len(descriptor.Signatures, 1)
	r.Equal("CN="+signature, descriptor.Signatures[0].Signature["issuer"],
		"v1 records the subject of the signing certificate")

	output := runFailingOCMv2Command(t, configPath, "verify", "cv", reference)
	r.Contains(output, "issuer mismatch")
}

// Test_Integration_OCMv1NullLabelSignature covers a signing-relevant label with
// value: null. The JCS normalisation engine drops nil map entries, so the
// normalised descriptor differs from the one v1 signed.
func Test_Integration_OCMv1NullLabelSignature(t *testing.T) {
	r := require.New(t)
	knownGap(t, 1402)

	v1Config := newCompatibilityConfig(t)
	privateKey, publicKey := newRSAKeyPair(t)
	repository := filepath.Join(t.TempDir(), "ctf")
	addOCMv1ComponentVersion(t, v1Config, repository, fmt.Sprintf(`name: %s
version: %s
provider:
  name: ocm.software
labels:
  - name: null-label
    value: null
    signing: true
resources:
  - name: text
    version: %s
    type: plainText
    relation: local
    input:
      type: utf8
      text: OCM v1 null label fixture
`, ocmv1NullLabelComponentName, ocmv1Version, ocmv1Version))

	v1Binary := os.Getenv("OCM_V1_BIN")
	v1Reference := repository + "//" + ocmv1NullLabelComponentName + ":" + ocmv1Version
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "sign", "cv", "--signature", ocmv1SignatureName, "--private-key", privateKey, v1Reference)
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "verify", "cv", "--signature", ocmv1SignatureName, "--public-key", publicKey, v1Reference)

	configPath := newVerificationConfig(t, ocmv1SignatureName, publicKey)
	reference := "ctf::" + v1Reference
	descriptor := readCompatDescriptor(t, configPath, reference)
	r.Equal([]map[string]any{{"name": "null-label", "value": nil, "signing": true}}, descriptor.Component.Labels,
		"v2 reads the null value; only normalisation drops it")

	output := runFailingOCMv2Command(t, configPath, "verify", "cv", reference)
	r.Contains(output, "digest mismatch")
}

// Test_Integration_OCMv1LegacyNormalisation covers jsonNormalisation/v2, the
// only v1 normalisation algorithm without a v2 mapping.
func Test_Integration_OCMv1LegacyNormalisation(t *testing.T) {
	r := require.New(t)
	knownGap(t, 1403)

	v1Config := newCompatibilityConfig(t)
	privateKey, publicKey := newRSAKeyPair(t)
	repository := filepath.Join(t.TempDir(), "ctf")
	addOCMv1ComponentVersion(t, v1Config, repository, plainTextConstructor(ocmv1LegacyNormComponentName))

	v1Binary := os.Getenv("OCM_V1_BIN")
	v1Reference := repository + "//" + ocmv1LegacyNormComponentName + ":" + ocmv1Version
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "sign", "cv", "--signature", ocmv1SignatureName,
		"--private-key", privateKey, "--normalization", "jsonNormalisation/v2", v1Reference)
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "verify", "cv", "--signature", ocmv1SignatureName, "--public-key", publicKey, v1Reference)

	configPath := newVerificationConfig(t, ocmv1SignatureName, publicKey)
	reference := "ctf::" + v1Reference
	descriptor := readCompatDescriptor(t, configPath, reference)
	r.Len(descriptor.Signatures, 1)
	r.Equal("jsonNormalisation/v2", descriptor.Signatures[0].Digest["normalisationAlgorithm"])

	output := runFailingOCMv2Command(t, configPath, "verify", "cv", reference)
	r.Contains(output, "unknown normalisation algorithm: jsonNormalisation/v2")
}

// Test_Integration_OCMv1MergeLabel covers the label merge field. v2 has no
// Merge field on its label types, so the field is dropped on read and the
// transferred copy no longer carries it for a v1 consumer.
func Test_Integration_OCMv1MergeLabel(t *testing.T) {
	r := require.New(t)
	knownGap(t, 1371)

	v1Config := newCompatibilityConfig(t)
	repository := filepath.Join(t.TempDir(), "ctf")
	addOCMv1ComponentVersion(t, v1Config, repository, fmt.Sprintf(`name: %s
version: %s
provider:
  name: ocm.software
labels:
  - name: merge-label
    value: initial
    merge:
      algorithm: default
resources:
  - name: text
    version: %s
    type: plainText
    relation: local
    input:
      type: utf8
      text: OCM v1 merge label fixture
`, ocmv1MergeLabelComponentName, ocmv1Version, ocmv1Version))

	v1Reference := repository + "//" + ocmv1MergeLabelComponentName + ":" + ocmv1Version
	source := readOCMv1Descriptor(t, v1Config, v1Reference)
	r.Equal([]map[string]any{{
		"name":  "merge-label",
		"value": "initial",
		"merge": map[string]any{"algorithm": "default"},
	}}, source.Component.Labels, "v1 stores and reads back the merge field")

	configPath := newCompatibilityConfig(t)
	reference := "ctf::" + v1Reference
	r.Equal([]map[string]any{{"name": "merge-label", "value": "initial"}},
		readCompatDescriptor(t, configPath, reference).Component.Labels)

	targetPath := filepath.Join(t.TempDir(), "transferred")
	runOCMv2Command(t, configPath, "transfer", "cv", reference, "ctf::"+targetPath)
	target := readOCMv1Descriptor(t, v1Config, targetPath+"//"+ocmv1MergeLabelComponentName+":"+ocmv1Version)
	r.Equal([]map[string]any{{"name": "merge-label", "value": "initial"}}, target.Component.Labels)
}

// Test_Integration_OCMv1LegacySchema covers descriptors v1 writes with the
// deprecated ocm.software/v3alpha1 schema. v2 decodes them as v2 and fails
// without naming the schema.
func Test_Integration_OCMv1LegacySchema(t *testing.T) {
	r := require.New(t)
	knownGap(t, 1407)

	v1Config := newCompatibilityConfig(t)
	repository := filepath.Join(t.TempDir(), "ctf")
	addOCMv1ComponentVersion(t, v1Config, repository,
		plainTextConstructor(ocmv1LegacySchemaComponentName), "--scheme", "ocm.software/v3alpha1")

	// The v3alpha1 shape has no "component" field, so it is only read as text.
	v1Reference := repository + "//" + ocmv1LegacySchemaComponentName + ":" + ocmv1Version
	v1Output := runCompatibilityTool(t, os.Getenv("OCM_V1_BIN"), "--config", v1Config,
		"get", "cv", "-o", "yaml", v1Reference)
	r.Contains(string(v1Output), "apiVersion: ocm.software/v3alpha1", "v1 reads its own v3alpha1 descriptor")

	configPath := newCompatibilityConfig(t)
	output := runFailingOCMv2Command(t, configPath, "get", "cv", "ctf::"+v1Reference)
	r.Contains(output, `parsing identity "name=,version="`)
	r.NotContains(output, "v3alpha1", "the error does not name the deprecated schema")
}

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

func plainTextConstructor(component string) string {
	return fmt.Sprintf(`name: %s
version: %s
provider:
  name: ocm.software
resources:
  - name: text
    version: %s
    type: plainText
    relation: local
    input:
      type: utf8
      text: OCM v1 compatibility fixture
`, component, ocmv1Version, ocmv1Version)
}

// newOCMv1CertificateChain creates a self-signed root and a code-signing leaf
// certificate whose common name is the signature name, which is what v1 writes
// into signature.issuer by default.
func newOCMv1CertificateChain(t *testing.T, commonName string) (string, string, string) {
	t.Helper()
	r := require.New(t)

	directory := t.TempDir()
	rootKey := filepath.Join(directory, "root.key")
	rootCertificate := filepath.Join(directory, "root.pem")
	privateKey := filepath.Join(directory, "leaf.key")
	request := filepath.Join(directory, "leaf.csr")
	leafCertificate := filepath.Join(directory, "leaf.pem")
	extensions := filepath.Join(directory, "leaf.ext")

	runCompatibilityTool(t, "openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes",
		"-keyout", rootKey, "-out", rootCertificate, "-days", "1", "-subj", "/CN=ocm-compat-root")
	runCompatibilityTool(t, "openssl", "genpkey", "-algorithm", "RSA", "-pkeyopt", "rsa_keygen_bits:2048", "-out", privateKey)
	runCompatibilityTool(t, "openssl", "req", "-new", "-key", privateKey, "-out", request, "-subj", "/CN="+commonName)
	r.NoError(os.WriteFile(extensions,
		[]byte("basicConstraints=CA:FALSE\nkeyUsage=digitalSignature\nextendedKeyUsage=codeSigning\n"), 0o600))
	runCompatibilityTool(t, "openssl", "x509", "-req", "-in", request, "-CA", rootCertificate, "-CAkey", rootKey,
		"-out", leafCertificate, "-days", "1", "-set_serial", "1", "-extfile", extensions)

	return privateKey, leafCertificate, rootCertificate
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

// Test_Integration_OCMv1LocalOciBlobAccess covers localOciBlob, the legacy form
// of localBlob that OCM v1 repositories still read. v2 does not know the type,
// so download fails and transfer silently leaves the blob behind.
func Test_Integration_OCMv1LocalOciBlobAccess(t *testing.T) {
	r := require.New(t)
	knownGap(t, 1369)

	v1Config := newCompatibilityConfig(t)
	repository := filepath.Join(t.TempDir(), "ctf")
	addOCMv1ComponentVersion(t, v1Config, repository, plainTextConstructor(ocmv1LocalOciBlobComponentName))

	// No released v1 CLI writes localOciBlob any more, so the stored descriptor
	// is rewritten into the legacy shape v1 still reads.
	rewriteOCMv1Descriptor(t, repository, func(descriptor map[string]any) {
		component, ok := descriptor["component"].(map[string]any)
		r.True(ok, "descriptor has a component")
		resources, ok := component["resources"].([]any)
		r.True(ok, "component has resources")
		r.Len(resources, 1)
		resource, ok := resources[0].(map[string]any)
		r.True(ok, "resource is an object")
		access, ok := resource["access"].(map[string]any)
		r.True(ok, "resource has an access")
		r.Equal("localBlob", access["type"])
		resource["access"] = map[string]any{
			"type":   "localOciBlob",
			"digest": access["localReference"],
		}
	})

	v1Reference := repository + "//" + ocmv1LocalOciBlobComponentName + ":" + ocmv1Version
	v1Output := runCompatibilityTool(t, os.Getenv("OCM_V1_BIN"), "--config", v1Config,
		"get", "cv", "-o", "yaml", v1Reference)
	r.Contains(string(v1Output), "type: localOciBlob", "v1 reads the legacy access")

	configPath := newCompatibilityConfig(t)
	reference := "ctf::" + v1Reference
	r.Equal("localOciBlob", readCompatDescriptor(t, configPath, reference).Component.Resources[0].Access["type"])

	downloadPath := filepath.Join(t.TempDir(), "text")
	output := runFailingOCMv2Command(t, configPath, "download", "resource", reference,
		"--identity", "name=text", "--output", downloadPath, "--extraction-policy", "disable")
	r.Contains(output, "localOciBlob")

	// The transfer reports success, but the blob is not carried over, so the
	// target references content it does not hold.
	targetPath := filepath.Join(t.TempDir(), "transferred")
	runOCMv2Command(t, configPath, "transfer", "cv", reference, "ctf::"+targetPath)
	targetReference := targetPath + "//" + ocmv1LocalOciBlobComponentName + ":" + ocmv1Version
	v1Output = runCompatibilityTool(t, os.Getenv("OCM_V1_BIN"), "--config", v1Config,
		"get", "cv", "-o", "yaml", targetReference)
	r.Contains(string(v1Output), "type: localOciBlob")

	// v1 reads the content from the source, but not from the target v2 wrote.
	runCompatibilityTool(t, os.Getenv("OCM_V1_BIN"), "--config", v1Config, "download", "resource",
		v1Reference, "text", "-O", filepath.Join(t.TempDir(), "source-text"))
	command := exec.CommandContext(t.Context(), os.Getenv("OCM_V1_BIN"), "--config", v1Config,
		"download", "resource", targetReference, "text", "-O", filepath.Join(t.TempDir(), "target-text"))
	v1Output, err := command.CombinedOutput()
	r.Error(err, "the blob is missing in the target: %s", v1Output)
	r.Contains(string(v1Output), "not found")
}
