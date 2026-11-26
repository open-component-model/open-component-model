package integration

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/oras-project/oras-go/v3/registry/remote"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/cli/cmd"
	"ocm.software/open-component-model/bindings/go/cli/integration/internal"
)

// listerCredentialConfig authenticates against the test registry over plain HTTP.
const listerCredentialConfig = `
type: generic.config.ocm.software/v1
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
- type: http.config.ocm.software/v1alpha1
  plainHTTP: true`

// listerPlainHTTPOnlyConfig has no credentials, only plain HTTP.
const listerPlainHTTPOnlyConfig = `
type: generic.config.ocm.software/v1
configurations:
- type: http.config.ocm.software/v1alpha1
  plainHTTP: true`

// Test_Integration_GetComponentVersion_RepositoryReference lists components of a
// private registry and looks up their versions through the CLI. It is the
// regression test for the version lookup that previously ran without
// credentials: listing succeeded but the version lookup failed with 401. It
// also covers that a component which cannot be read is skipped by get and
// fails transfer.
func Test_Integration_GetComponentVersion_RepositoryReference(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := t.Context()

	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err)

	cfg := fmt.Sprintf(listerCredentialConfig, registry.Host, registry.Port, registry.User, registry.Password)
	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	repoRef := fmt.Sprintf("http://%s", registry.RegistryAddress)

	// Push two readable components into the private registry.
	constructor := `
components:
- name: ocm.software/app-a
  version: v1.0.0
  provider:
    name: ocm.software
  resources:
  - name: text
    version: v1.0.0
    type: plainText
    input:
      type: utf8
      text: "a"
- name: ocm.software/app-b
  version: v1.0.0
  provider:
    name: ocm.software
  resources:
  - name: text
    version: v1.0.0
    type: plainText
    input:
      type: utf8
      text: "b"`
	constructorPath := filepath.Join(t.TempDir(), "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructor), os.ModePerm))

	addCMD := cmd.New()
	addCMD.SetArgs([]string{
		"add", "component-version",
		"--repository", repoRef,
		"--constructor", constructorPath,
		"--config", cfgPath,
	})
	r.NoError(addCMD.ExecuteContext(ctx), "add component-version should succeed")

	// Push an unreadable "component": a plain manifest under a
	// component-descriptors/ repository that is not an OCM component descriptor.
	// Its version lists, but reading the descriptor fails, so it exercises the
	// skip (get) and fail (transfer) paths.
	const brokenComponent = "broken/unreadable"
	pushUnreadableComponent(t, ctx, registry, "component-descriptors/"+brokenComponent, "v1.0.0")

	t.Run("names lists every component including the unreadable one", func(t *testing.T) {
		r := require.New(t)
		out := new(bytes.Buffer)
		getCMD := cmd.New()
		getCMD.SetOut(out)
		getCMD.SetArgs([]string{
			"get", "component-version", repoRef,
			"--detail", "names", "--output", "json",
			"--config", cfgPath,
		})
		r.NoError(getCMD.ExecuteContext(ctx), "names should succeed; it does not read components")
		printed := out.String()
		r.Contains(printed, "ocm.software/app-a")
		r.Contains(printed, "ocm.software/app-b")
		// names does not validate, so the catalog-listed broken component appears.
		r.Contains(printed, brokenComponent)
	})

	t.Run("versions lists readable components and skips the unreadable one", func(t *testing.T) {
		r := require.New(t)
		out := new(bytes.Buffer)
		getCMD := cmd.New()
		getCMD.SetOut(out)
		getCMD.SetArgs([]string{
			"get", "component-version", repoRef,
			"--detail", "versions", "--output", "json",
			"--config", cfgPath,
		})
		r.NoError(getCMD.ExecuteContext(ctx), "versions should succeed despite an unreadable component")
		printed := out.String()
		r.Contains(printed, "ocm.software/app-a")
		r.Contains(printed, "ocm.software/app-b")
		r.Contains(printed, "v1.0.0")
		r.NotContains(printed, brokenComponent, "the unreadable component must be skipped")
	})

	t.Run("full fetches descriptors and skips the unreadable one", func(t *testing.T) {
		r := require.New(t)
		out := new(bytes.Buffer)
		getCMD := cmd.New()
		getCMD.SetOut(out)
		getCMD.SetArgs([]string{
			"get", "component-version", repoRef,
			"--detail", "full", "--latest", "--output", "json",
			"--config", cfgPath,
		})
		r.NoError(getCMD.ExecuteContext(ctx), "full should succeed despite an unreadable component")
		printed := out.String()
		r.Contains(printed, "ocm.software/app-a")
		r.Contains(printed, "ocm.software/app-b")
		r.NotContains(printed, brokenComponent, "the unreadable component must be skipped")
	})

	t.Run("full tree output shows the repository as the first row", func(t *testing.T) {
		r := require.New(t)
		out := new(bytes.Buffer)
		getCMD := cmd.New()
		getCMD.SetOut(out)
		getCMD.SetArgs([]string{
			"get", "component-version", repoRef,
			"--detail", "full", "--latest", "--output", "tree",
			"--config", cfgPath,
		})
		r.NoError(getCMD.ExecuteContext(ctx))
		printed := out.String()
		// The repository row (with its type) is the first data row, above the components.
		repoRow := strings.Index(printed, "(OCIRepository)")
		compRow := strings.Index(printed, "ocm.software/app-a")
		r.GreaterOrEqual(repoRow, 0, "repository row should be shown")
		r.GreaterOrEqual(compRow, 0, "component should be shown")
		r.Less(repoRow, compRow, "the repository row must come first")
	})

	t.Run("without credentials the command fails with an auth error, not an empty result", func(t *testing.T) {
		r := require.New(t)
		noCredsPath := filepath.Join(t.TempDir(), "nocreds.yaml")
		r.NoError(os.WriteFile(noCredsPath, []byte(listerPlainHTTPOnlyConfig), os.ModePerm))

		getCMD := cmd.New()
		getCMD.SetOut(new(bytes.Buffer))
		getCMD.SetArgs([]string{
			"get", "component-version", repoRef,
			"--latest",
			"--config", noCredsPath,
		})
		err := getCMD.ExecuteContext(ctx)
		r.Error(err, "listing a private registry without credentials must fail")
		lower := strings.ToLower(err.Error())
		r.True(strings.Contains(lower, "credential") || strings.Contains(lower, "unauthorized") || strings.Contains(lower, "401"),
			"error should indicate an authentication failure, got: %s", err.Error())
	})
}

// pushUnreadableComponent pushes a plain (non-OCM) manifest to name:tag so the
// repository appears in the catalog and lists a version, but reading it as a
// component descriptor fails.
func pushUnreadableComponent(t *testing.T, ctx context.Context, registry *internal.OCIRegistry, name, tag string) {
	t.Helper()
	r := require.New(t)

	authClient := internal.CreateAuthClient(registry.RegistryAddress, registry.User, registry.Password)
	reg, err := remote.NewRegistry(registry.RegistryAddress)
	r.NoError(err)
	reg.PlainHTTP = true
	reg.Client = authClient

	repo, err := reg.Repository(ctx, name)
	r.NoError(err)

	configBytes := []byte("{}")
	configDesc := ociImageSpecV1.Descriptor{
		MediaType: "application/vnd.oci.image.config.v1+json",
		Digest:    digest.FromBytes(configBytes),
		Size:      int64(len(configBytes)),
	}
	r.NoError(repo.Push(ctx, configDesc, bytes.NewReader(configBytes)))

	manifest := ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config:    configDesc,
		Layers:    []ociImageSpecV1.Descriptor{},
	}
	manifestBytes, err := json.Marshal(manifest)
	r.NoError(err)
	manifestDesc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromBytes(manifestBytes),
		Size:      int64(len(manifestBytes)),
	}
	r.NoError(repo.Manifests().Push(ctx, manifestDesc, bytes.NewReader(manifestBytes)))
	r.NoError(repo.Tag(ctx, manifestDesc, tag))
}
