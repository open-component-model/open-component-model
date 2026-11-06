package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/cli/cmd"
	"ocm.software/open-component-model/bindings/go/cli/integration/internal"
)

// Test_Integration_PlainHTTPConfig covers the user story behind per-host
// plainHTTP: a scheme-less repository reference reaches a registry without TLS
// because the HTTP config marks the host, not because of an http:// prefix.
func Test_Integration_PlainHTTPConfig(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err)

	const credentialsConfig = `
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: OCIRegistry
      hostname: %[1]q
      port: %[2]q
    credentials:
    - type: Credentials/v1
      properties:
        username: %[3]q
        password: %[4]q
`
	const plainHTTPConfig = `
- type: http.config.ocm.software/v1alpha1
  hosts:
    %[5]q:
      plainHTTP: true
`
	writeConfig := func(t *testing.T, template string) string {
		t.Helper()
		cfg := fmt.Sprintf(template, registry.Host, registry.Port, registry.User, registry.Password, registry.RegistryAddress)
		cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
		require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))
		return cfgPath
	}

	componentName := "ocm.software/plain-http-config"
	componentVersion := "v1.0.0"
	constructorPath := filepath.Join(t.TempDir(), "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, fmt.Appendf(nil, `
components:
- name: %s
  version: %s
  provider:
    name: ocm.software
  resources:
  - name: test-resource
    version: v1.0.0
    type: plainText
    input:
      type: utf8
      text: "Hello over plain HTTP from the HTTP config!"
`, componentName, componentVersion), os.ModePerm))

	addComponentVersion := func(t *testing.T, cfgPath, repository string) error {
		t.Helper()
		addCMD := cmd.New()
		addCMD.SetArgs([]string{
			"add", "component-version",
			"--repository", repository,
			"--constructor", constructorPath,
			"--config", cfgPath,
		})
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		return addCMD.ExecuteContext(ctx)
	}

	t.Run("without the host entry a scheme-less reference tries TLS and fails", func(t *testing.T) {
		r := require.New(t)
		err := addComponentVersion(t, writeConfig(t, credentialsConfig), registry.RegistryAddress)
		r.ErrorContains(err, "server gave HTTP response to HTTPS client", "the failure must come from TLS against a plain HTTP registry")
	})

	t.Run("without the host entry an http prefix is rejected before any request", func(t *testing.T) {
		r := require.New(t)
		err := addComponentVersion(t, writeConfig(t, credentialsConfig), "http://"+registry.RegistryAddress)
		r.ErrorContains(err, "plain HTTP is not enabled", "the prefix alone must not downgrade to plain HTTP")
		r.ErrorContains(err, registry.RegistryAddress, "the error must name the host")
	})

	t.Run("with plainHTTP for the host an http prefix still works", func(t *testing.T) {
		r := require.New(t)
		r.NoError(addComponentVersion(t, writeConfig(t, credentialsConfig+plainHTTPConfig), "http://"+registry.RegistryAddress))
	})

	t.Run("with plainHTTP for the host add and get work over cleartext", func(t *testing.T) {
		r := require.New(t)
		cfgPath := writeConfig(t, credentialsConfig+plainHTTPConfig)
		r.NoError(addComponentVersion(t, cfgPath, registry.RegistryAddress)) // no scheme: the config decides

		getCMD := cmd.New()
		getCMD.SetArgs([]string{
			"get", "component-version",
			fmt.Sprintf("%s//%s:%s", registry.RegistryAddress, componentName, componentVersion),
			"--config", cfgPath,
			"--output", "json",
		})
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		r.NoError(getCMD.ExecuteContext(ctx))

		desc, err := registry.Connect(t).GetComponentVersion(ctx, componentName, componentVersion)
		r.NoError(err)
		r.Equal(componentName, desc.Component.Name)
		r.Equal(componentVersion, desc.Component.Version)
		r.Len(desc.Component.Resources, 1)
		r.Equal("test-resource", desc.Component.Resources[0].Name)
	})
}
