package examples

import (
	"bytes"
	"fmt"
	"io"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/registry"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	access "ocm.software/open-component-model/bindings/go/oci/spec/access"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	repository "ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// TestExample_OCIRegistryRoundTrip demonstrates a full OCI registry workflow:
// start a local registry, push a component version with a local resource,
// list versions, and retrieve the resource content.
//
// This test is skipped with -short because it requires Docker to spin up a
// container-based OCI registry.
func TestExample_OCIRegistryRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping OCI registry test in short mode (requires Docker)")
	}

	r := require.New(t)
	ctx := t.Context()

	// 1. Start a local OCI registry using testcontainers.
	registryContainer, err := registry.Run(ctx, "registry:3.0.0")
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(testcontainers.TerminateContainer(registryContainer))
	})

	registryAddress, err := registryContainer.HostAddress(ctx)
	r.NoError(err)

	// 2. Create an OCI repository client pointing at the local registry.
	scheme := runtime.NewScheme()
	access.MustAddToScheme(scheme)
	v2.MustAddToScheme(scheme)

	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(registryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(&auth.Client{
			Client: retry.DefaultClient,
			Cache:  auth.NewCache(),
		}),
	)
	r.NoError(err)

	repo, err := oci.NewRepository(
		oci.WithResolver(resolver),
		oci.WithScheme(scheme),
		oci.WithTempDir(t.TempDir()),
	)
	r.NoError(err)

	component := "acme.org/oci-example"
	version := "1.0.0"
	resourceContent := []byte("hello from OCI registry")

	// 3. Build a component descriptor with a local blob resource.
	res := &descriptor.Resource{
		Relation: descriptor.LocalRelation,
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "greeting",
				Version: version,
			},
		},
		Type: "plainText",
		Access: &v2.LocalBlob{
			LocalReference: digest.FromBytes(resourceContent).String(),
			MediaType:      "text/plain",
		},
	}

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			Provider: descriptor.Provider{Name: "acme.org"},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    component,
					Version: version,
				},
			},
			Resources: []descriptor.Resource{*res},
		},
	}

	// 4. Upload the resource blob and store the component version.
	b := inmemory.New(bytes.NewReader(resourceContent))
	newRes, err := repo.AddLocalResource(ctx, component, version, res, b)
	r.NoError(err)
	desc.Component.Resources[0] = *newRes

	r.NoError(repo.AddComponentVersion(ctx, desc))

	// 5. Verify the component version does not exist before (fresh registry).
	//    List versions to confirm it was stored.
	versions, err := repo.ListComponentVersions(ctx, component)
	r.NoError(err)
	r.Contains(versions, version)

	// 6. Retrieve the component version.
	got, err := repo.GetComponentVersion(ctx, component, version)
	r.NoError(err)
	r.Equal(component, got.Component.Name)
	r.Equal(version, got.Component.Version)
	r.Len(got.Component.Resources, 1)

	// 7. Download the resource and verify its content.
	readBlob, _, err := repo.GetLocalResource(ctx, component, version, map[string]string{
		"name":    "greeting",
		"version": version,
	})
	r.NoError(err)

	var buf bytes.Buffer
	r.NoError(blob.Copy(&buf, readBlob))
	r.Equal(resourceContent, buf.Bytes())

	// 8. Verify that a non-existent version returns ErrNotFound.
	_, err = repo.GetComponentVersion(ctx, component, "99.99.99")
	r.ErrorIs(err, repository.ErrNotFound)
}

// TestExample_OCIRegistryMultipleVersions demonstrates pushing multiple
// versions to an OCI registry and listing them.
func TestExample_OCIRegistryMultipleVersions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping OCI registry test in short mode (requires Docker)")
	}

	r := require.New(t)
	ctx := t.Context()

	registryContainer, err := registry.Run(ctx, "registry:3.0.0")
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(testcontainers.TerminateContainer(registryContainer))
	})

	registryAddress, err := registryContainer.HostAddress(ctx)
	r.NoError(err)

	scheme := runtime.NewScheme()
	access.MustAddToScheme(scheme)
	v2.MustAddToScheme(scheme)

	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(registryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(&auth.Client{
			Client: retry.DefaultClient,
			Cache:  auth.NewCache(),
		}),
	)
	r.NoError(err)

	repo, err := oci.NewRepository(
		oci.WithResolver(resolver),
		oci.WithScheme(scheme),
		oci.WithTempDir(t.TempDir()),
	)
	r.NoError(err)

	component := "acme.org/multi-version"

	// Push three versions of the same component.
	for _, ver := range []string{"1.0.0", "1.1.0", "2.0.0"} {
		content := []byte(fmt.Sprintf("payload for %s", ver))
		res := &descriptor.Resource{
			Relation: descriptor.LocalRelation,
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: "data", Version: ver},
			},
			Type: "plainText",
			Access: &v2.LocalBlob{
				LocalReference: digest.FromBytes(content).String(),
				MediaType:      "text/plain",
			},
		}

		desc := &descriptor.Descriptor{
			Meta: descriptor.Meta{Version: "v2"},
			Component: descriptor.Component{
				Provider: descriptor.Provider{Name: "acme.org"},
				ComponentMeta: descriptor.ComponentMeta{
					ObjectMeta: descriptor.ObjectMeta{Name: component, Version: ver},
				},
				Resources: []descriptor.Resource{*res},
			},
		}

		b := inmemory.New(bytes.NewReader(content))
		newRes, err := repo.AddLocalResource(ctx, component, ver, res, b)
		r.NoError(err)
		desc.Component.Resources[0] = *newRes

		r.NoError(repo.AddComponentVersion(ctx, desc))
	}

	// List and verify all versions are present.
	versions, err := repo.ListComponentVersions(ctx, component)
	r.NoError(err)
	r.Len(versions, 3)

	// Retrieve a specific version and verify the resource content matches.
	readBlob, _, err := repo.GetLocalResource(ctx, component, "1.1.0", map[string]string{
		"name":    "data",
		"version": "1.1.0",
	})
	r.NoError(err)

	rc, err := readBlob.ReadCloser()
	r.NoError(err)
	defer rc.Close()

	data, err := io.ReadAll(rc)
	r.NoError(err)
	r.Equal("payload for 1.1.0", string(data))
}
