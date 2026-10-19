package integration

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
	"oras.land/oras-go/v2"
	orasoci "oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"

	"ocm.software/open-component-model/bindings/go/cli/integration/internal"
)

const (
	ocmv1ComponentName = "ocm.software/ocmv1-compat-signed-basic"
	ocmv1Version       = "1.0.0"
)

type ocmv1Descriptor struct {
	Component struct {
		Name      string `json:"name"`
		Version   string `json:"version"`
		Resources []struct {
			Name   string         `json:"name"`
			Digest map[string]any `json:"digest"`
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
		t.Skip("run through task compat/ocmv1:test with both CLI binaries")
	}
	ctfPath, layoutPath, publicKey, artifactSet := prepareOCMv1Scenario(t)
	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err)

	config := fmt.Sprintf(`type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: OCIRegistry
      hostname: %[1]q
      port: %[2]q
      scheme: http
    credentials:
    - type: Credentials/v1
      properties:
        username: %[3]q
        password: %[4]q
  - identity:
      type: RSA/v1alpha1
      algorithm: RSASSA-PKCS1-V1_5
      signature: default
    credentials:
    - type: Credentials/v1
      properties:
        public_key_pem_file: %[5]q
`, registry.Host, registry.Port, registry.User, registry.Password, publicKey)
	configPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	r.NoError(os.WriteFile(configPath, []byte(config), 0o600))

	layout, err := orasoci.NewFromFS(t.Context(), os.DirFS(layoutPath))
	r.NoError(err)
	repository, err := remote.NewRepository(registry.Reference("component-descriptors/" + ocmv1ComponentName))
	r.NoError(err)
	repository.PlainHTTP = true
	repository.Client = &auth.Client{
		Client: retry.DefaultClient,
		Credential: auth.StaticCredential(registry.RegistryAddress, auth.Credential{
			Username: registry.User,
			Password: registry.Password,
		}),
	}
	_, err = oras.Copy(t.Context(), layout, ocmv1Version, repository, ocmv1Version, oras.DefaultCopyOptions)
	r.NoError(err)

	sources := []struct {
		name       string
		repository string
	}{
		{name: "ctf", repository: "ctf::" + ctfPath},
		{name: "oci", repository: "http://" + registry.RegistryAddress},
	}
	for _, source := range sources {
		t.Run(source.name, func(t *testing.T) {
			r := require.New(t)
			sourceReference := source.repository + "//" + ocmv1ComponentName + ":" + ocmv1Version
			original := readCompatDescriptor(t, configPath, sourceReference)
			r.Equal(ocmv1ComponentName, original.Component.Name)
			r.Equal(ocmv1Version, original.Component.Version)
			r.Len(original.Signatures, 1)
			r.Len(original.Component.Resources, 2)
			runOCMv2Command(t, configPath, "verify", "cv", sourceReference)

			expectedContent := map[string][]byte{
				"text":         []byte("OCM v1 compatibility fixture"),
				"artifact-set": artifactSet,
			}
			for _, resource := range original.Component.Resources {
				downloadPath := filepath.Join(t.TempDir(), resource.Name)
				runOCMv2Command(t, configPath, "download", "resource", sourceReference,
					"--identity", "name="+resource.Name, "--output", downloadPath, "--extraction-policy", "disable")
				content, readErr := os.ReadFile(downloadPath)
				r.NoError(readErr)
				r.Equal(expectedContent[resource.Name], content)
				if resource.Name == "text" {
					hash := sha256.Sum256(content)
					r.Equal(hex.EncodeToString(hash[:]), resource.Digest["value"])
				} else {
					r.Equal("ociArtifactDigest/v1", resource.Digest["normalisationAlgorithm"])
				}
			}

			for _, copyResources := range []bool{false, true} {
				for _, targetKind := range []string{"ctf", "oci"} {
					name := fmt.Sprintf("%s/copy=%t/target=%s", source.name, copyResources, targetKind)
					t.Run(name, func(t *testing.T) {
						targetRepository := "ctf::" + filepath.Join(t.TempDir(), "target")
						if targetKind == "oci" {
							targetRepository = "http://" + registry.Reference(fmt.Sprintf("ocmv1-%s-%t", source.name, copyResources))
						}
						arguments := []string{"transfer", "cv", sourceReference, targetRepository}
						if copyResources {
							arguments = append(arguments, "--copy-resources")
						}
						runOCMv2Command(t, configPath, arguments...)
						targetReference := targetRepository + "//" + ocmv1ComponentName + ":" + ocmv1Version
						runOCMv2Command(t, configPath, "verify", "cv", targetReference)
						transferred := readCompatDescriptor(t, configPath, targetReference)
						require.Equal(t, original.Signatures, transferred.Signatures)
						require.Equal(t, original.Component.Resources, transferred.Component.Resources)
						for _, resource := range transferred.Component.Resources {
							downloadPath := filepath.Join(t.TempDir(), resource.Name)
							runOCMv2Command(t, configPath, "download", "resource", targetReference,
								"--identity", "name="+resource.Name, "--output", downloadPath, "--extraction-policy", "disable")
							content, readErr := os.ReadFile(downloadPath)
							require.NoError(t, readErr)
							require.Equal(t, expectedContent[resource.Name], content)
						}
					})
				}
			}
		})
	}
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
