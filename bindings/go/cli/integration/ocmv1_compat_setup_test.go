package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type ocmv1ArtifactIndex struct {
	Artifacts []struct {
		Repository string `json:"repository"`
		Tag        string `json:"tag"`
		Digest     string `json:"digest"`
		MediaType  string `json:"mediaType"`
	} `json:"artifacts"`
}

func prepareOCMv1Scenario(t *testing.T) (ctfPath, layoutPath, publicKey string, artifactSet []byte) {
	t.Helper()
	r := require.New(t)
	workingDirectory := t.TempDir()
	v1Binary := os.Getenv("OCM_V1_BIN")
	v1Config := filepath.Join(workingDirectory, "v1-config.yaml")
	r.NoError(os.WriteFile(v1Config, []byte("type: generic.config.ocm.software/v1\nconfigurations: []\n"), 0o600))
	privateKey := filepath.Join(workingDirectory, "private.pem")
	publicKey = filepath.Join(workingDirectory, "public.pem")
	runCompatibilityTool(t, "openssl", "genpkey", "-algorithm", "RSA", "-pkeyopt", "rsa_keygen_bits:2048", "-out", privateKey)
	runCompatibilityTool(t, "openssl", "pkey", "-in", privateKey, "-pubout", "-out", publicKey)

	artifactSetPath, err := filepath.Abs(filepath.Join("..", "..", "oci", "transformer", "testdata", "oci-layout.tar.gz"))
	r.NoError(err)
	artifactSet, err = os.ReadFile(artifactSetPath)
	r.NoError(err)
	constructor := fmt.Sprintf(`name: %s
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
  - name: artifact-set
    version: %s
    type: ociImage
    relation: local
    input:
      type: file
      path: %q
      mediaType: application/vnd.oci.image.manifest.v1+tar+gzip
`, ocmv1ComponentName, ocmv1Version, ocmv1Version, ocmv1Version, artifactSetPath)
	constructorPath := filepath.Join(workingDirectory, "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructor), 0o600))
	ctfPath = filepath.Join(workingDirectory, "v1-ctf")
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "add", "cv", "--create", "--file", ctfPath, constructorPath)
	v1Reference := ctfPath + "//" + ocmv1ComponentName + ":" + ocmv1Version
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "sign", "cv", "--signature", "default", "--private-key", privateKey, v1Reference)
	runCompatibilityTool(t, v1Binary, "--config", v1Config, "verify", "cv", "--signature", "default", "--public-key", publicKey, v1Reference)
	layoutPath = filepath.Join(workingDirectory, "oci-layout")
	createOCMv1Layout(t, ctfPath, layoutPath)
	return ctfPath, layoutPath, publicKey, artifactSet
}

func createOCMv1Layout(t *testing.T, ctfPath, layoutPath string) {
	t.Helper()
	r := require.New(t)
	indexData, err := os.ReadFile(filepath.Join(ctfPath, "artifact-index.json"))
	r.NoError(err)
	var artifactIndex ocmv1ArtifactIndex
	r.NoError(json.Unmarshal(indexData, &artifactIndex))
	r.NoError(os.MkdirAll(filepath.Join(layoutPath, "blobs", "sha256"), 0o700))
	blobs, err := os.ReadDir(filepath.Join(ctfPath, "blobs"))
	r.NoError(err)
	for _, entry := range blobs {
		digest, ok := strings.CutPrefix(entry.Name(), "sha256.")
		if !ok {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(ctfPath, "blobs", entry.Name()))
		r.NoError(readErr)
		r.NoError(os.WriteFile(filepath.Join(layoutPath, "blobs", "sha256", digest), data, 0o600))
	}
	r.NoError(os.WriteFile(filepath.Join(layoutPath, "oci-layout"), []byte(`{"imageLayoutVersion":"1.0.0"}`), 0o600))
	for _, artifact := range artifactIndex.Artifacts {
		if artifact.Repository != "component-descriptors/"+ocmv1ComponentName || artifact.Tag != ocmv1Version {
			continue
		}
		manifestDigest, ok := strings.CutPrefix(artifact.Digest, "sha256:")
		r.True(ok)
		manifestData, readErr := os.ReadFile(filepath.Join(ctfPath, "blobs", "sha256."+manifestDigest))
		r.NoError(readErr)
		index := map[string]any{
			"schemaVersion": 2,
			"manifests": []map[string]any{{
				"mediaType": artifact.MediaType,
				"digest":    artifact.Digest,
				"size":      len(manifestData),
				"annotations": map[string]string{
					"org.opencontainers.image.ref.name": artifact.Tag,
				},
			}},
		}
		encoded, marshalErr := json.Marshal(index)
		r.NoError(marshalErr)
		r.NoError(os.WriteFile(filepath.Join(layoutPath, "index.json"), encoded, 0o600))
		return
	}
	t.Fatal("OCM v1 CTF has no signed component version")
}

func runCompatibilityTool(t *testing.T, binary string, arguments ...string) []byte {
	t.Helper()
	command := exec.CommandContext(t.Context(), binary, arguments...)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s %v: %s", binary, arguments, output)
	return output
}
