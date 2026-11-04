package conformance

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	ocmv1ComponentName      = "ocm.software/ocmv1-compat-signed-basic"
	ocmv1ChildComponentName = "ocm.software/ocmv1-compat-signed-child"
	ocmv1SignatureName      = "default"
	ocmv1ResourceContent    = "OCM v1 compatibility fixture"
)

func Test_Integration_OCMv1SignedArchiveTransfer(t *testing.T) {
	r := require.New(t)

	v1Config := newCompatibilityConfig(t)
	signedPath, publicKey := prepareOCMv1SignedCTF(t, v1Config)
	configPath := newVerificationConfig(t, ocmv1SignatureName, publicKey)

	signedReference := "ctf::" + signedPath + "//" + ocmv1ComponentName + ":" + ocmv1Version
	source := readCompatDescriptor(t, configPath, signedReference)
	r.Equal(ocmv1ComponentName, source.Component.Name)
	r.Equal(ocmv1Version, source.Component.Version)
	r.Len(source.Signatures, 1)
	r.Len(source.Component.Resources, 1)
	r.Len(source.Component.References, 1)
	r.NotEmpty(source.Component.References[0].Digest, "the v1 signature covers the reference digest")
	runOCMv2Command(t, configPath, "verify", "cv", signedReference)

	archivePath := filepath.Join(t.TempDir(), "signed.tgz")
	runCompatibilityTool(t, os.Getenv("OCM_V1_BIN"), "--config", v1Config,
		"transfer", "ctf", "--type", "tgz", signedPath, archivePath)
	archiveReference := "ctf::" + archivePath
	r.Equal(source, readCompatDescriptor(t, configPath, archiveReference+"//"+ocmv1ComponentName+":"+ocmv1Version))

	content := []byte(ocmv1ResourceContent)
	downloadPath := filepath.Join(t.TempDir(), "text")
	runOCMv2Command(t, configPath, "download", "resource", signedReference,
		"--identity", "name=text", "--output", downloadPath, "--extraction-policy", "disable")
	got, err := os.ReadFile(downloadPath)
	r.NoError(err)
	r.Equal(content, got)
	hash := sha256.Sum256(content)
	r.Equal(hex.EncodeToString(hash[:]), source.Component.Resources[0].Digest["value"])

	// Transfer must not touch signed metadata, whichever uploader handles the
	// resources. --copy-resources is the deprecated spelling v1 users carry over.
	for _, testCase := range []struct {
		name      string
		arguments []string
	}{
		{name: "by-reference"},
		{name: "copy-resources", arguments: []string{"--copy-resources"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			r := require.New(t)

			targetPath := filepath.Join(t.TempDir(), "transferred")
			arguments := append([]string{"transfer", "cv", "--recursive"}, testCase.arguments...)
			runOCMv2Command(t, configPath, append(arguments, archiveReference, "ctf::"+targetPath)...)

			targetReference := "ctf::" + targetPath + "//" + ocmv1ComponentName + ":" + ocmv1Version
			target := readCompatDescriptor(t, configPath, targetReference)
			assertSignedMetadataEqual(t, source, target)
			r.Equal(source.Component, target.Component)
			runOCMv2Command(t, configPath, "verify", "cv", targetReference)

			childReference := "ctf::" + targetPath + "//" + ocmv1ChildComponentName + ":" + ocmv1Version
			child := readCompatDescriptor(t, configPath, childReference)
			r.Equal(ocmv1ChildComponentName, child.Component.Name, "--recursive must transfer the referenced version")
		})
	}
}

// assertSignedMetadataEqual compares everything a v1 signature covers: the
// signatures themselves and the digest of every resource and reference.
func assertSignedMetadataEqual(t *testing.T, source, target ocmv1Descriptor) {
	t.Helper()
	r := require.New(t)

	r.Equal(source.Signatures, target.Signatures)

	resources := make(map[string]map[string]any, len(target.Component.Resources))
	for _, resource := range target.Component.Resources {
		resources[resource.Name] = resource.Digest
	}
	r.Len(resources, len(source.Component.Resources))
	for _, resource := range source.Component.Resources {
		r.Equal(resource.Digest, resources[resource.Name], "digest of resource %q", resource.Name)
	}

	references := make(map[string]map[string]any, len(target.Component.References))
	for _, reference := range target.Component.References {
		references[reference.Name] = reference.Digest
	}
	r.Len(references, len(source.Component.References))
	for _, reference := range source.Component.References {
		r.Equal(reference.Digest, references[reference.Name], "digest of reference %q", reference.Name)
	}
}

func prepareOCMv1SignedCTF(t *testing.T, v1Config string) (string, string) {
	t.Helper()

	privateKey, publicKey := newRSAKeyPair(t)
	signedPath := filepath.Join(t.TempDir(), "v1-signed-ctf")

	addOCMv1ComponentVersion(t, v1Config, signedPath, fmt.Sprintf(`name: %s
version: %s
provider:
  name: ocm.software
resources:
  - name: child-text
    version: %s
    type: plainText
    relation: local
    input:
      type: utf8
      text: OCM v1 compatibility child fixture
`, ocmv1ChildComponentName, ocmv1Version, ocmv1Version))

	addOCMv1ComponentVersion(t, v1Config, signedPath, fmt.Sprintf(`name: %s
version: %s
provider:
  name: ocm.software
componentReferences:
  - name: child
    componentName: %s
    version: %s
resources:
  - name: text
    version: %s
    type: plainText
    relation: local
    input:
      type: utf8
      text: %s
`, ocmv1ComponentName, ocmv1Version, ocmv1ChildComponentName, ocmv1Version, ocmv1Version, ocmv1ResourceContent))

	v1Binary := os.Getenv("OCM_V1_BIN")
	v1Reference := signedPath + "//" + ocmv1ComponentName + ":" + ocmv1Version
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "sign", "cv", "--signature", ocmv1SignatureName, "--private-key", privateKey, v1Reference)
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "verify", "cv", "--signature", ocmv1SignatureName, "--public-key", publicKey, v1Reference)

	return signedPath, publicKey
}
