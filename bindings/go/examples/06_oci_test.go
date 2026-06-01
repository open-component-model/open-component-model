// Step 6: OCI Registry Round-Trips
//
// What you'll learn:
//   - Spinning up a local OCI registry with testcontainers
//   - Pushing component versions with resources to a real registry
//   - Listing and retrieving component versions from a remote registry
//   - The repository interface is the same as Step 4 (CTF), just with a
//     different backend
//   - Using OCM credentials to authenticate against a private registry
//
// This is the capstone of the tour. Everything from the previous steps —
// blobs, descriptors, repositories — comes together in a real OCI registry
// workflow. These tests require Docker and are skipped with -short.

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
	"golang.org/x/crypto/bcrypt"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	credidentity "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
	ocirepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/repository"
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

	// 5. List versions to confirm the component version was stored.
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

// TestExample_PrivateOCIRegistry demonstrates authenticating against a
// password-protected OCI registry using OCM credentials.
//
// OCM resolves credentials by matching a consumer identity — here derived
// from the registry URL — against a static credential map. This is the same
// identity model used in Steps 3 and 4, applied to a real registry.
//
// This test is skipped with -short because it requires Docker.
func TestExample_PrivateOCIRegistry(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping private OCI registry test in short mode (requires Docker)")
	}

	r := require.New(t)
	ctx := t.Context()

	// 1. Start a password-protected OCI registry.
	const username, password = "test-user", "test-password"
	htpasswdHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	r.NoError(err)

	registryContainer, err := registry.Run(ctx, "registry:3.0.0",
		registry.WithHtpasswd(fmt.Sprintf("%s:%s", username, string(htpasswdHash))),
	)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(testcontainers.TerminateContainer(registryContainer))
	})

	registryAddress, err := registryContainer.HostAddress(ctx)
	r.NoError(err)

	// 2. Build an OCM credential resolver for the registry.
	//
	// IdentityFromOCIRepository derives the consumer identity from the registry
	// URL (hostname, scheme). The static resolver matches this identity at
	// request time and returns the username/password.
	repoSpec := &ocirepospec.Repository{
		Type:    runtime.Type{Name: ocirepospec.Type, Version: "v1"},
		BaseUrl: fmt.Sprintf("http://%s", registryAddress),
	}
	identity, err := credidentity.IdentityFromOCIRepository(repoSpec)
	r.NoError(err)

	credResolver := credentials.NewStaticCredentialsResolver(map[string]map[string]string{
		identity.String(): {"username": username, "password": password},
	})

	// 3. Create the OCI repository client using the credentials for authentication.
	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(registryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(&auth.Client{
			Client: retry.DefaultClient,
			Cache:  auth.NewCache(),
			Credential: auth.StaticCredential(registryAddress, auth.Credential{
				Username: username,
				Password: password,
			}),
		}),
	)
	r.NoError(err)

	repo, err := oci.NewRepository(
		oci.WithResolver(resolver),
		oci.WithTempDir(t.TempDir()),
	)
	r.NoError(err)

	// 4. Push a component version — identical to the anonymous case in Step 6.
	component := "acme.org/private-example"
	version := "1.0.0"
	resourceContent := []byte("secret payload")

	res := &descriptor.Resource{
		Relation: descriptor.LocalRelation,
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: "secret-data", Version: version},
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
				ObjectMeta: descriptor.ObjectMeta{Name: component, Version: version},
			},
			Resources: []descriptor.Resource{*res},
		},
	}

	b := inmemory.New(bytes.NewReader(resourceContent))
	newRes, err := repo.AddLocalResource(ctx, component, version, res, b)
	r.NoError(err)
	desc.Component.Resources[0] = *newRes
	r.NoError(repo.AddComponentVersion(ctx, desc))

	// 5. Verify the credential resolver can resolve credentials for this registry.
	resolvedCreds, err := credResolver.Resolve(ctx, identity)
	r.NoError(err)
	r.Equal(username, resolvedCreds["username"])
	r.Equal(password, resolvedCreds["password"])

	// 6. Retrieve the component version to confirm authentication succeeded.
	got, err := repo.GetComponentVersion(ctx, component, version)
	r.NoError(err)
	r.Equal(component, got.Component.Name)
	r.Equal(version, got.Component.Version)
	r.Len(got.Component.Resources, 1)

	// 7. Download the resource and verify its content.
	readBlob, _, err := repo.GetLocalResource(ctx, component, version, map[string]string{
		"name":    "secret-data",
		"version": version,
	})
	r.NoError(err)

	var buf bytes.Buffer
	r.NoError(blob.Copy(&buf, readBlob))
	r.Equal(resourceContent, buf.Bytes())
}
