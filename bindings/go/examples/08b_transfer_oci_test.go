package examples

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/stretchr/testify/require"
	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	credentialsconfig "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	"ocm.software/open-component-model/bindings/go/oci/repository/resource"
	accessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ociidentityv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
	ocirepositoryspecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

// newRegistry starts an in-memory registry and returns its host:port. users maps a
// repository prefix (e.g. "project1") to the basic-auth user and password it requires.
func newRegistry(t *testing.T, users map[string]authn.Basic) string {
	t.Helper()
	reg := registry.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for prefix, want := range users {
			if strings.HasPrefix(r.URL.Path, "/v2/"+prefix+"/") {
				if u, p, ok := r.BasicAuth(); !ok || u != want.Username || p != want.Password {
					w.Header().Set("WWW-Authenticate", `Basic realm="registry"`)
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
			}
		}
		reg.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return server.Listener.Addr().String()
}

// TestOciTransfer tests building a CV referencing two OCI images stored in a remote registry,
// the `imageRegistry`. This registry requires authentication, and it requires different credentials
// for the two images we want to include. The CV is added to the `srcRegistry` and then transported
// to the `targetRegistry`, including the two OCI images from `imageRegistry`.
// The test demonstrates setting up an OCM Configuration for authentication and http config (plainHTTP)
// and transporting the CV.
func TestOciTransfer(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)
	component, version := "my.org/mycomponent", "1.0.0"

	srcRegistry := newRegistry(t, nil)

	imageRegistryUsers := map[string]authn.Basic{
		"project1": {Username: "user1", Password: "password1"},
		"project2": {Username: "user2", Password: "password2"},
	}
	imageRegistry := newRegistry(t, imageRegistryUsers)
	imageHost, imagePort, err := net.SplitHostPort(imageRegistry)
	r.NoError(err)

	targetRegistryUsers := map[string]authn.Basic{
		"project1": {Username: "user3", Password: "password3"},
		"project2": {Username: "user4", Password: "password4"},
	}
	targetRegistry := newRegistry(t, targetRegistryUsers)
	targetHost, targetPort, err := net.SplitHostPort(targetRegistry)
	r.NoError(err)

	cfgYAML := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identities:
          - type: OCIRegistry/v1
            hostname: %[1]s
            port: "%[2]s"
            path: project1/**
        credentials:
          - type: Credentials/v1
            properties:
              username: user1
              password: password1
      - identities:
          - type: OCIRegistry/v1
            hostname: %[1]s
            port: "%[2]s"
            path: project2/**
        credentials:
          - type: Credentials/v1
            properties:
              username: user2
              password: password2
      - identities:
          - type: OCIRegistry/v1
            hostname: %[3]s
            port: "%[4]s"
            path: project1/**
        credentials:
          - type: Credentials/v1
            properties:
              username: user3
              password: password3
      - identities:
          - type: OCIRegistry/v1
            hostname: %[3]s
            port: "%[4]s"
            path: project1
        credentials:
          - type: Credentials/v1
            properties:
              username: user3
              password: password3
      - identities:
          - type: OCIRegistry/v1
            hostname: %[3]s
            port: "%[4]s"
            path: project2/**
        credentials:
          - type: Credentials/v1
            properties:
              username: user4
              password: password4

  - type: http.config.ocm.software/v1alpha1
    hosts:
      "%[5]s": {plainHTTP: true}
      "%[6]s": {plainHTTP: true}
      "%[7]s": {plainHTTP: true}
`, imageHost, imagePort, targetHost, targetPort, srcRegistry, targetRegistry, imageRegistry)

	var cfg genericv1.Config
	r.NoError(genericv1.Scheme.Decode(strings.NewReader(cfgYAML), &cfg))
	httpCfg, err := httpv1alpha1.ResolveHTTPConfig(&cfg)
	r.NoError(err)

	// Push the image into the two projects on the `imageRegistry`, then prepare
	// the resource descriptor we're going to use in the CV descriptor later.
	img, err := crane.Image(map[string][]byte{"hello.txt": []byte("hello world")})
	r.NoError(err)
	var resources []descriptor.Resource
	for i, project := range []string{"project1", "project2"} {
		ref := imageRegistry + "/" + project + "/myimage/hello:1.0.0"
		user := imageRegistryUsers[project]
		r.Error(crane.Push(img, ref, crane.Insecure), "push without credentials must be rejected")
		r.NoError(crane.Push(img, ref, crane.Insecure, crane.WithAuth(&user)))
		resources = append(resources, descriptor.Resource{
			Name: fmt.Sprintf("hello%d", i+1), Version: "1.0.0",
			Relation: descriptor.ExternalRelation,
			Type:     "ociImage",
			Access: &accessv1.OCIImage{
				Type:           runtime.NewVersionedType(accessv1.OCIImageType, accessv1.Version),
				ImageReference: ref,
			},
		})
	}

	repoProvider := provider.NewComponentVersionRepositoryProvider(provider.WithHTTPConfig(httpCfg))
	srcSpec := &ocirepositoryspecv1.Repository{
		Type:    runtime.NewVersionedType(ocirepositoryspecv1.Type, ocirepositoryspecv1.Version),
		BaseUrl: srcRegistry,
	}
	srcRepo, err := repoProvider.GetComponentVersionRepository(ctx, srcSpec, nil)
	r.NoError(err)
	r.NoError(srcRepo.AddComponentVersion(ctx, &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			Provider:      descriptor.Provider{Name: "ocm.software"},
			ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: component, Version: version}},
			Resources:     resources,
		},
	}))

	// Credentials from the same config. The identity scheme lets OCIRegistry/v1 in the
	// config match the unversioned OCIRegistry the lookup uses.
	identityScheme := runtime.NewScheme()
	ociidentityv1.MustRegisterIdentityType(identityScheme)
	credCfg, err := credentialsconfig.LookupCredentialConfig(&cfg)
	r.NoError(err)
	credResolver, err := credentials.ToGraph(ctx, credCfg, credentials.Options{ConsumerIdentityTypeScheme: identityScheme})
	r.NoError(err)

	// Transfer by value: the OCI uploader copies each image into the target registry.
	uploaders := []transferv1alpha1.UploaderConfig{&transferv1alpha1.OCIUploaderConfig{
		Type: runtime.NewVersionedType(transferv1alpha1.OCIUploaderConfigType, transferv1alpha1.Version),
	}}
	tgd, err := transfer.BuildGraphDefinition(ctx, nil, uploaders, transfer.Mapping{
		Components: []transfer.ComponentID{{Component: component, Version: version}},
		Target: &ocirepositoryspecv1.Repository{
			Type:    runtime.NewVersionedType(ocirepositoryspecv1.Type, ocirepositoryspecv1.Version),
			BaseUrl: targetRegistry + "/project1", // store components in project1 so this will require authentication as well
		},
		Resolver: transfer.NewRepositoryResolver(srcRepo, srcSpec),
	})
	r.NoError(err)

	// The HTTP config goes to every layer that opens connections: the OCI-to-OCI copy
	// uses a resource repository the builder creates from transfer.WithHTTPConfig.
	resourceRepo := resource.NewResourceRepository(nil, resource.WithHTTPConfig(httpCfg))
	graph, err := transfer.NewDefaultBuilder(repoProvider, resourceRepo, credResolver, transfer.WithHTTPConfig(httpCfg)).BuildAndCheck(tgd)
	r.NoError(err)
	r.NoError(graph.Process(ctx))
}
