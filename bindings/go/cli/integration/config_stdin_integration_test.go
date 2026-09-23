package integration

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/direct"
	"ocm.software/open-component-model/bindings/go/cli/cmd"
	"ocm.software/open-component-model/bindings/go/cli/integration/internal"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
)

// Test_Integration_ConfigFromStdin proves that a configuration piped through `--config -`
// reaches the credential graph and the signing setup. Each subtest has a control run
// without the stdin config, so a success cannot come from configuration found elsewhere.
func Test_Integration_ConfigFromStdin(t *testing.T) {
	r := require.New(t)

	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err, "should be able to start registry container")
	repo := fmt.Sprintf("http://%s", registry.RegistryAddress)

	t.Run("registry credentials", func(t *testing.T) {
		constructor := writeMinimalConstructor(t, "ocm.software/config-stdin-test", "v1.0.0")

		t.Run("wrong password on stdin fails", func(t *testing.T) {
			err := runOCM(t, registryConfig(registry, registry.Password+"-invalid"),
				"add", "component-version", "--repository", repo, "--constructor", constructor, "--config", "-")
			require.ErrorContains(t, err, "401")
		})

		t.Run("credentials on stdin authenticate", func(t *testing.T) {
			r := require.New(t)
			err := runOCM(t, registryConfig(registry, registry.Password),
				"add", "component-version", "--repository", repo, "--constructor", constructor, "--config", "-")
			r.NoError(err)

			desc, err := registry.Connect(t).GetComponentVersion(t.Context(), "ocm.software/config-stdin-test", "v1.0.0")
			r.NoError(err, "component version should exist in the registry")
			r.Equal("ocm.software/config-stdin-test", desc.Component.Name)
		})
	})

	t.Run("signing config", func(t *testing.T) {
		r := require.New(t)

		// Registry credentials come from a file, the signing config from stdin: this is the
		// "secret part on stdin, other settings in a file" shape merged in command line order.
		registryCfg, err := internal.CreateOCMConfigForRegistry(t, []internal.ConfigOpts{
			{Host: registry.Host, Port: registry.Port, User: registry.User, Password: registry.Password},
		})
		r.NoError(err)

		name, version := "ocm.software/config-stdin-signing-test", "v1.0.0"
		uploadComponentVersion(t, registry.Connect(t), name, version, resource{
			Resource: &descriptor.Resource{
				ElementMeta:  descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "raw", Version: "v1.0.0"}},
				Type:         "some-arbitrary-type-packed-in-image",
				Access:       &v2.LocalBlob{},
				CreationTime: descriptor.CreationTime(time.Now()),
			},
			ReadOnlyBlob: direct.NewFromBytes([]byte("signed-with-stdin-config")),
		})
		ref := fmt.Sprintf("%s//%s:%s", repo, name, version)
		signingCfg := gpgSigningConfig(t)

		t.Run("sign without signing config fails", func(t *testing.T) {
			err := runOCM(t, nil, "sign", "cv", ref, "--config", registryCfg)
			require.ErrorContains(t, err, "private key not found")
		})

		t.Run("sign and verify with signing config on stdin", func(t *testing.T) {
			r := require.New(t)
			r.NoError(runOCM(t, signingCfg, "sign", "cv", ref, "--config", registryCfg, "--config", "-"))
			r.NoError(runOCM(t, signingCfg, "verify", "cv", ref, "--config", registryCfg, "--config", "-"))
		})
	})
}

// runOCM runs one CLI command with the given stdin and a bounded context.
func runOCM(t *testing.T, stdin []byte, args ...string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	c := cmd.New()
	c.SetIn(bytes.NewReader(stdin))
	c.SetArgs(args)
	return c.ExecuteContext(ctx)
}

// registryConfig renders the credentials for the registry in memory, so the secret
// under test never touches disk.
func registryConfig(registry *internal.OCIRegistry, password string) []byte {
	return fmt.Appendf(nil, `
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: OCIRegistry
      hostname: %q
      port: %q
      scheme: http
    credentials:
    - type: Credentials/v1
      properties:
        username: %q
        password: %q
`, registry.Host, registry.Port, registry.User, password)
}

// gpgSigningConfig writes a fresh GPG key pair and returns a config that carries both the
// key credentials and the signer/verifier selection.
func gpgSigningConfig(t *testing.T) []byte {
	t.Helper()
	dir := t.TempDir()
	entity := mustGPGEntity(t)
	privKey := filepath.Join(dir, "signing-key.asc")
	pubKey := filepath.Join(dir, "verify-key.asc")
	writeArmoredPrivKey(t, entity, privKey)
	writeArmoredPubKey(t, entity, pubKey)

	return fmt.Appendf(nil, `
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: GPG/v1alpha1
      signature: default
    credentials:
    - type: Credentials/v1
      properties:
        privateKeyPGPFile: %q
        publicKeyPGPFile: %q
- type: signing.config.ocm.software/v1alpha1
  signer:
    type: GPGSigningConfiguration/v1alpha1
  verifier:
    type: GPGSigningConfiguration/v1alpha1
`, privKey, pubKey)
}

func writeMinimalConstructor(t *testing.T, name, version string) string {
	t.Helper()
	content := fmt.Sprintf(`
components:
- name: %s
  version: %s
  provider:
    name: ocm.software
`, name, version)
	path := filepath.Join(t.TempDir(), "constructor.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), os.ModePerm))
	return path
}
