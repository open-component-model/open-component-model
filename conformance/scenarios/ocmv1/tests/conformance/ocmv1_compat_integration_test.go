package conformance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	ocmv1ComponentName       = "ocm.software/ocmv1-compat-signed-basic"
	ocmv1AccessComponentName = "ocm.software/ocmv1-compat-access"
	ocmv1InputComponentName  = "ocm.software/ocmv1-compat-input"
	ocmv1Version             = "1.0.0"
)

type ocmv1Descriptor struct {
	Component struct {
		Name      string `json:"name"`
		Version   string `json:"version"`
		Resources []struct {
			Name   string         `json:"name"`
			Digest map[string]any `json:"digest"`
			Access map[string]any `json:"access"`
		} `json:"resources"`
	} `json:"component"`
	Signatures []struct {
		Name      string         `json:"name"`
		Digest    map[string]any `json:"digest"`
		Signature map[string]any `json:"signature"`
	} `json:"signatures"`
}

func Test_Integration_OCMv1Compatibility(t *testing.T) {
	r := require.New(t)
	if os.Getenv("OCM_V1_BIN") == "" || os.Getenv("OCM_V2_BIN") == "" {
		t.Skip("run through task conformance/ocmv1:run with both CLI binaries")
	}
	signedPath, accessPath, inputPath, publicKey := prepareOCMv1Scenario(t)

	config := fmt.Sprintf(`type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: RSA/v1alpha1
      algorithm: RSASSA-PKCS1-V1_5
      signature: default
    credentials:
    - type: Credentials/v1
      properties:
        public_key_pem_file: %q
`, publicKey)
	configPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	r.NoError(os.WriteFile(configPath, []byte(config), 0o600))

	signedReference := "ctf::" + signedPath + "//" + ocmv1ComponentName + ":" + ocmv1Version
	signedDescriptor := readCompatDescriptor(t, configPath, signedReference)
	r.Equal(ocmv1ComponentName, signedDescriptor.Component.Name)
	r.Equal(ocmv1Version, signedDescriptor.Component.Version)
	r.Len(signedDescriptor.Signatures, 1)
	runOCMv2Command(t, configPath, "verify", "cv", signedReference)
	r.Len(signedDescriptor.Component.Resources, 1)
	content := []byte("OCM v1 compatibility fixture")
	path := filepath.Join(t.TempDir(), "text")
	runOCMv2Command(t, configPath, "download", "resource", signedReference,
		"--identity", "name=text", "--output", path, "--extraction-policy", "disable")
	got, err := os.ReadFile(path)
	r.NoError(err)
	r.Equal(content, got)
	hash := sha256.Sum256(content)
	r.Equal(hex.EncodeToString(hash[:]), signedDescriptor.Component.Resources[0].Digest["value"])

	accessReference := "ctf::" + accessPath + "//" + ocmv1AccessComponentName + ":" + ocmv1Version
	accessDescriptor := readCompatDescriptor(t, configPath, accessReference)
	r.Equal(ocmv1AccessComponentName, accessDescriptor.Component.Name)
	r.Equal(ocmv1Version, accessDescriptor.Component.Version)
	r.Len(accessDescriptor.Component.Resources, 4)

	resources := map[string]struct {
		accessType string
		fields     map[string]any
	}{
		"helm": {accessType: "helm", fields: map[string]any{"helmRepository": "https://example.invalid/charts", "helmChart": "sample:1.0.0"}},
		"oci":  {accessType: "ociArtifact", fields: map[string]any{"imageReference": "example.invalid/sample/image:1.0.0"}},
		"git":  {accessType: "git", fields: map[string]any{"repository": "https://example.invalid/sample/repository.git", "ref": "refs/heads/main", "commit": "0123456789abcdef0123456789abcdef01234567"}},
		"s3":   {accessType: "s3", fields: map[string]any{"region": "eu-central-1", "bucket": "sample-bucket", "key": "sample/object.txt"}},
	}
	seen := make(map[string]bool, len(resources))
	for _, resource := range accessDescriptor.Component.Resources {
		expected, ok := resources[resource.Name]
		r.True(ok, "unexpected resource %q", resource.Name)
		r.False(seen[resource.Name], "duplicate resource %q", resource.Name)
		seen[resource.Name] = true
		t.Run(resource.Name, func(t *testing.T) {
			r := require.New(t)
			r.Equal(expected.accessType, resource.Access["type"])
			for field, value := range expected.fields {
				r.Equal(value, resource.Access[field], field)
			}
		})
	}
	r.Len(seen, len(resources))

	inputReference := "ctf::" + inputPath + "//" + ocmv1InputComponentName + ":" + ocmv1Version
	inputDescriptor := readCompatDescriptor(t, configPath, inputReference)
	r.Equal(ocmv1InputComponentName, inputDescriptor.Component.Name)
	r.Len(inputDescriptor.Component.Resources, 2)
	seenInput := make(map[string]bool, 2)
	for _, resource := range inputDescriptor.Component.Resources {
		r.Contains([]string{"helm-input", "git-input"}, resource.Name)
		r.False(seenInput[resource.Name], "duplicate resource %q", resource.Name)
		seenInput[resource.Name] = true
		r.Equal("localBlob", resource.Access["type"])
		r.NotEmpty(resource.Digest["value"])
		t.Run(resource.Name, func(t *testing.T) {
			r := require.New(t)
			if resource.Name == "helm-input" {
				r.Equal("ociArtifactDigest/v1", resource.Digest["normalisationAlgorithm"])
				r.Equal("application/vnd.oci.image.manifest.v1+tar+gzip", resource.Access["mediaType"])
				return
			}
			r.Equal("genericBlobDigest/v1", resource.Digest["normalisationAlgorithm"])
			downloadPath := filepath.Join(t.TempDir(), "download")
			runOCMv2Command(t, configPath, "download", "resource", inputReference,
				"--identity", "name="+resource.Name, "--output", downloadPath, "--extraction-policy", "disable")
			data, err := os.ReadFile(downloadPath)
			r.NoError(err)
			hash := sha256.Sum256(data)
			r.Equal(hex.EncodeToString(hash[:]), resource.Digest["value"])
		})
	}
	r.Len(seenInput, 2)
}

func runOCMv2Command(t *testing.T, configPath string, arguments ...string) []byte {
	t.Helper()
	r := require.New(t)
	binary := os.Getenv("OCM_V2_BIN")
	command := exec.CommandContext(t.Context(), binary, append(arguments, "--config", configPath, "--loglevel", "error")...)
	output, err := command.CombinedOutput()
	r.NoError(err, "%s", output)
	return output
}

func readCompatDescriptor(t *testing.T, configPath, reference string) ocmv1Descriptor {
	t.Helper()
	r := require.New(t)
	output := runOCMv2Command(t, configPath, "get", "cv", reference, "-o", "json")
	var descriptors []ocmv1Descriptor
	r.NoError(json.Unmarshal(output, &descriptors))
	r.Len(descriptors, 1)
	return descriptors[0]
}
