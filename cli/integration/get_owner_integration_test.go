package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	"ocm.software/open-component-model/bindings/go/repository"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/cmd"
	"ocm.software/open-component-model/cli/integration/internal"
)

// Test_Integration_GetOwner_OCIRepository covers the `ocm get owner` command
// end-to-end against a live containerised OCI registry. It pushes a component
// version with a by-value resource through the OCM OCI binding (which attaches
// an ownership referrer to the resource's subject manifest), then runs the CLI
// to verify the owner is discovered and rendered through the shared
// `get cv` pipeline.
func Test_Integration_GetOwner_OCIRepository(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	registry, err := internal.CreateOCIRegistry(t)
	r.NoError(err)

	cfgPath, err := internal.CreateOCMConfigForRegistry(t, []internal.ConfigOpts{
		{Host: registry.Host, Port: registry.Port, User: registry.User, Password: registry.Password},
	})
	r.NoError(err)

	const (
		componentName    = "ocm.software/test-component"
		componentVersion = "v1.0.0"
		resourceName     = "backend-image"
	)

	subjectDigest := pushOwningComponentVersion(t, registry, componentName, componentVersion, resourceName)
	imageRef := fmt.Sprintf("http://%s/component-descriptors/%s@%s", registry.RegistryAddress, componentName, subjectDigest)

	cases := []struct {
		name  string
		args  []string
		check func(t *testing.T, out string)
	}{
		{
			name: "table output renders the owning component version",
			args: []string{"get", "owner", imageRef, "--config", cfgPath},
			check: func(t *testing.T, out string) {
				require.Contains(t, out, componentName, "table must include the component name")
				require.Contains(t, out, componentVersion, "table must include the component version")
				require.Contains(t, out, "ocm.software", "table must include the provider")
			},
		},
		{
			name: "yaml output renders the full descriptor",
			args: []string{"get", "owner", imageRef, "--config", cfgPath, "-o", "yaml"},
			check: func(t *testing.T, out string) {
				require.Contains(t, out, "name: "+componentName)
				require.Contains(t, out, "version: "+componentVersion)
				require.Contains(t, out, "schemaVersion: v2")
			},
		},
		{
			name: "json output emits the raw owner-lookup payload (no cv lookup)",
			args: []string{"get", "owner", imageRef, "--config", cfgPath, "-o", "json"},
			check: func(t *testing.T, out string) {
				var decoded []repository.ResourceOwner
				require.NoError(t, json.Unmarshal([]byte(out), &decoded), "json output must decode into owner payloads")
				require.Len(t, decoded, 1, "expected exactly one ownership entry")
				require.Equal(t, componentName, decoded[0].ComponentName)
				require.Equal(t, componentVersion, decoded[0].ComponentVersion)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			getCMD := cmd.New()
			var out bytes.Buffer
			getCMD.SetOut(&out)
			getCMD.SetArgs(tc.args)

			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			r.NoError(getCMD.ExecuteContext(ctx), "`ocm get owner` should succeed against the test registry")
			tc.check(t, out.String())
		})
	}
}

// pushOwningComponentVersion uploads a component version with a single by-value
// OCI resource to the test registry, with the ownership-referrer policy
// enabled so the resource gets an ownership referrer pointing back at the CV.
// Returns the subject digest the referrer is attached to — i.e. the image
// reference the `ocm get owner` command resolves.
func pushOwningComponentVersion(t *testing.T, registry *internal.OCIRegistry, component, version, resourceName string) digest.Digest {
	t.Helper()
	r := require.New(t)

	client := internal.CreateAuthClient(registry.RegistryAddress, registry.User, registry.Password)
	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(registry.RegistryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)
	repo, err := oci.NewRepository(
		oci.WithResolver(resolver),
		oci.WithTempDir(t.TempDir()),
		oci.WithOwnershipReferrerPolicy(oci.OwnershipReferrerPolicyEnabled),
	)
	r.NoError(err)

	artifact := internal.CreateSingleLayerOCIImageLayoutTar(t, []byte("ownership-payload")).Bytes()
	res := &descruntime.Resource{
		ElementMeta: descruntime.ElementMeta{
			ObjectMeta: descruntime.ObjectMeta{Name: resourceName, Version: version},
		},
		Type:     "ociArtifact",
		Relation: descruntime.LocalRelation,
		Access: &v2.LocalBlob{
			Type: ocmruntime.Type{
				Name:    v2.LocalBlobAccessType,
				Version: v2.LocalBlobAccessTypeVersion,
			},
			MediaType:      layout.MediaTypeOCIImageLayoutTarV1,
			LocalReference: digest.FromBytes(artifact).String(),
		},
	}
	newRes, err := repo.AddLocalResource(t.Context(), component, version, res, inmemory.New(bytes.NewReader(artifact)))
	r.NoError(err)

	// AddLocalResource only uploads the resource blob + its ownership
	// referrer. The component-descriptor manifest itself still needs to be
	// pushed so `get owner`'s cv-rendering step can resolve the owning CV.
	desc := &descruntime.Descriptor{
		Meta: descruntime.Meta{Version: "v2"},
		Component: descruntime.Component{
			ComponentMeta: descruntime.ComponentMeta{
				ObjectMeta: descruntime.ObjectMeta{Name: component, Version: version},
			},
			Provider:  descruntime.Provider{Name: "ocm.software"},
			Resources: []descruntime.Resource{*newRes},
		},
	}
	r.NoError(repo.AddComponentVersion(t.Context(), desc))

	var localAccess v2.LocalBlob
	r.NoError(v2.Scheme.Convert(newRes.Access, &localAccess))
	return digest.Digest(localAccess.LocalReference)
}
