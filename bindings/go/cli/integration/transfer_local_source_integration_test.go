package integration

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/cli/cmd"
	"ocm.software/open-component-model/bindings/go/cli/integration/internal"
	"ocm.software/open-component-model/bindings/go/ctf"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Sources are deprecated, but a source stored as a local blob is still copied by transfer:
// CTF -> OCI registry -> CTF must keep its content readable, while an external source
// stays a plain reference.
func Test_Integration_TransferComponentVersion_LocalBlobSource(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err, "should be able to start registry container")

	cfg := fmt.Sprintf(`
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
`, registry.Host, registry.Port, registry.User, registry.Password)
	cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
	r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

	const (
		componentName    = "ocm.software/local-source-transfer"
		componentVersion = "v1.0.0"
		sourceContent    = "local source content"
		gitRepository    = "https://example.invalid/repo.git"
	)
	constructorContent := fmt.Sprintf(`
components:
- name: %s
  version: %s
  provider:
    name: ocm.software
  sources:
  - name: local-source
    version: v1.0.0
    type: blob
    input:
      type: utf8/v1
      text: %s
  - name: git-source
    version: v1.0.0
    type: git
    access:
      type: Git/v1
      repository: %s
      commit: 7fd1a60b01f91b314f59955a4e4d4e80d8edf11d
`, componentName, componentVersion, sourceContent, gitRepository)
	constructorPath := filepath.Join(t.TempDir(), "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructorContent), os.ModePerm))

	sourceCTF := filepath.Join(t.TempDir(), "source-ctf")
	addCMD := cmd.New()
	addCMD.SetArgs([]string{
		"add", "component-version",
		"--repository", fmt.Sprintf("ctf::%s", sourceCTF),
		"--constructor", constructorPath,
	})
	r.NoError(addCMD.ExecuteContext(t.Context()))

	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	transfer := func(from, to string) {
		transferCMD := cmd.New()
		transferCMD.SetArgs([]string{"transfer", "component-version", from, to, "--config", cfgPath})
		r.NoError(transferCMD.ExecuteContext(ctx), "transfer %s -> %s", from, to)
	}
	registryRef := fmt.Sprintf("http://%s", registry.RegistryAddress)
	transfer(fmt.Sprintf("ctf::%s//%s:%s", sourceCTF, componentName, componentVersion), registryRef)
	targetCTF := filepath.Join(t.TempDir(), "target-ctf")
	transfer(fmt.Sprintf("%s//%s:%s", registryRef, componentName, componentVersion), fmt.Sprintf("ctf::%s", targetCTF))

	client := internal.CreateAuthClient(registry.RegistryAddress, registry.User, registry.Password)
	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(registry.RegistryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)
	registryRepo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)
	fs, err := filesystem.NewFS(targetCTF, os.O_RDONLY)
	r.NoError(err)
	ctfRepo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))))
	r.NoError(err)

	for name, repo := range map[string]*oci.Repository{"registry": registryRepo, "ctf": ctfRepo} {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			desc, err := repo.GetComponentVersion(ctx, componentName, componentVersion)
			r.NoError(err)
			r.Len(desc.Component.Sources, 2)

			local := desc.Component.Sources[0]
			r.Equal("local-source", local.Name)
			r.True(v2.IsLocalBlob(local.Access), "the local source must stay a local blob, got %T", local.Access)
			data, _, err := repo.GetLocalSource(ctx, componentName, componentVersion, local.ToIdentity())
			r.NoError(err)
			rc, err := data.ReadCloser()
			r.NoError(err)
			t.Cleanup(func() { r.NoError(rc.Close()) })
			got, err := io.ReadAll(rc)
			r.NoError(err)
			r.Equal(sourceContent, string(got))

			external := desc.Component.Sources[1]
			r.Equal("git-source", external.Name)
			raw, ok := external.Access.(*runtime.Raw)
			r.True(ok, "the external source access must be kept as is, got %T", external.Access)
			r.Contains(string(raw.Data), gitRepository)
		})
	}
}
