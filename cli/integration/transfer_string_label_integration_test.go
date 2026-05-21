package integration

// Regression test for https://github.com/open-component-model/open-component-model/issues/2585:
// Transfer of a component version whose label has a non-object JSON value (string, number, bool)
// must succeed with --copy-resources. Before the fix, the schema validator rejected string labels
// with "type: object" mismatch, causing the transfer to abort.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	"ocm.software/open-component-model/cli/cmd"
	"ocm.software/open-component-model/cli/integration/internal"
)

// Test_Integration_TransferComponentVersion_StringLabel verifies that a component version
// carrying a string-valued label can be transferred with --copy-resources without schema
// validation errors.
func Test_Integration_TransferComponentVersion_StringLabel(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err, "should start registry container")

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

	componentName := "ocm.software/test-string-label"
	componentVersion := "v1.0.0"

	fromDesc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    componentName,
					Version: componentVersion,
					Labels: []descriptor.Label{
						// String-valued label — the trigger for issue #2585.
						{
							Name:  "imagevector.gardener.cloud/name",
							Value: json.RawMessage(`"alpine"`),
						},
						// Number and boolean for completeness.
						{
							Name:  "priority",
							Value: json.RawMessage(`42`),
						},
						{
							Name:  "enabled",
							Value: json.RawMessage(`true`),
						},
					},
				},
			},
			Provider:  descriptor.Provider{Name: "ocm.software"},
			Resources: []descriptor.Resource{},
		},
	}

	fromDesc.Component.Resources = []descriptor.Resource{
		{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-blob",
					Version: "1.0.0",
				},
			},
			Type:     "plainText",
			Relation: descriptor.LocalRelation,
			Access: &v2.LocalBlob{
				MediaType: "text/plain",
			},
		},
	}

	sourceCTFPath := filepath.Join(t.TempDir(), "source-ctf")
	fs, err := filesystem.NewFS(sourceCTFPath, os.O_RDWR|os.O_CREATE)
	r.NoError(err)
	archive := ctf.NewFileSystemCTF(fs)
	sourceRepo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(archive)))
	r.NoError(err)

	ctx := t.Context()

	blobData := []byte("hello from string-label transfer test")
	updatedRes, err := sourceRepo.AddLocalResource(
		ctx, componentName, componentVersion,
		&fromDesc.Component.Resources[0],
		inmemory.New(bytes.NewReader(blobData)),
	)
	r.NoError(err)
	fromDesc.Component.Resources[0] = *updatedRes

	r.NoError(sourceRepo.AddComponentVersion(ctx, fromDesc))

	sourceRef := fmt.Sprintf("ctf::%s//%s:%s", sourceCTFPath, componentName, componentVersion)
	targetRef := fmt.Sprintf("http://%s", registry.RegistryAddress)

	transferCMD := cmd.New()
	transferCMD.SetArgs([]string{
		"transfer", "component-version",
		sourceRef, targetRef,
		"--config", cfgPath,
		"--copy-resources",
	})

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	r.NoError(transferCMD.ExecuteContext(ctx), "transfer with string label must succeed")

	// Verify the transferred component preserves the labels.
	client := internal.CreateAuthClient(registry.RegistryAddress, registry.User, registry.Password)
	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(registry.RegistryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)
	targetRepo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	got, err := targetRepo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err, "transferred component must be retrievable")
	r.Equal(componentName, got.Component.Name)
	r.Equal(componentVersion, got.Component.Version)

	// Labels must survive the round-trip through OCI storage.
	// Before the fix for #2585 the validator rejected string/number/bool values, so these
	// assertions would never be reached — the transfer itself would have failed.
	r.Len(got.Component.Labels, 3, "all three labels must be preserved")

	labelsByName := make(map[string]json.RawMessage, len(got.Component.Labels))
	for _, l := range got.Component.Labels {
		labelsByName[l.Name] = l.Value
	}

	r.JSONEq(`"alpine"`, string(labelsByName["imagevector.gardener.cloud/name"]))
	r.JSONEq(`42`, string(labelsByName["priority"]))
	r.JSONEq(`true`, string(labelsByName["enabled"]))
}
