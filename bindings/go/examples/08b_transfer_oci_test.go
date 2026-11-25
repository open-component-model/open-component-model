package examples

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/oras-project/oras-go/v3/registry/remote/auth"
	"github.com/oras-project/oras-go/v3/registry/remote/retry"
	"github.com/stretchr/testify/require"
	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	credentialsconfig "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	"ocm.software/open-component-model/bindings/go/oci/repository/resource"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	accessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ociidentityv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
	ocirepositoryspecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

func newRegistry(t *testing.T, creds [][3]string) string {
	t.Helper()
	handler := registry.New(registry.WithReferrersSupport(true))
	if len(creds) > 0 {
		handler = basicAuthHandler(handler, creds)
	}

	server := httptest.NewServer(handler)

	t.Cleanup(func() { server.Close() })

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("failed to parse registry URL: %v", err)
	}

	return u.Host
}

func basicAuthHandler(h http.Handler, creds [][3]string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, f := range creds {
			if strings.HasPrefix(r.URL.Path, "/v2/"+f[0]) {
				if u, p, ok := r.BasicAuth(); ok && f[1] == u && f[2] == p {
					h.ServeHTTP(w, r)
					return
				}
				w.Header().Set("WWW-Authenticate", `Basic realm="registry"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		h.ServeHTTP(w, r)
		return

	})
}

func TestOciTransfer(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)
	component, version := "my.org/mycomponent", "1.0.0"

	srcRegistry := newRegistry(t, nil)

	srcResolver, err := urlresolver.New(
		urlresolver.WithBaseURL(srcRegistry),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(&auth.Client{
			Client: retry.DefaultClient,
			Cache:  auth.NewCache(),
		}),
	)
	r.NoError(err)

	srcRepo, err := oci.NewRepository(
		oci.WithResolver(srcResolver),
		oci.WithTempDir(t.TempDir()),
	)
	r.NoError(err)

	srcSpec := &ocirepositoryspecv1.Repository{
		Type:    runtime.NewVersionedType(ocirepositoryspecv1.Type, ocirepositoryspecv1.Version),
		BaseUrl: "http://" + srcRegistry, // since the source registry is http, we need to add the scheme here
	}

	targetRegistry := newRegistry(t, nil)

	targetSpec := &ocirepositoryspecv1.Repository{
		Type:    runtime.NewVersionedType(ocirepositoryspecv1.Type, ocirepositoryspecv1.Version),
		BaseUrl: "http://" + targetRegistry, // since the target registry is http, we need to add the scheme here
	}

	// image source
	imageRegistry := newRegistry(t, [][3]string{{"project1", "user1", "password1"}, {"project2", "user2", "password2"}})

	img, err := crane.Image(map[string][]byte{"hello.txt": []byte("hello world")})
	r.NoError(err)

	project1auth := &authn.Basic{Username: "user1", Password: "password1"}
	project2auth := &authn.Basic{Username: "user2", Password: "password2"}

	r.Error(crane.Push(img, imageRegistry+"/project1/hello:1.0.0", crane.Insecure))
	r.Error(crane.Push(img, imageRegistry+"/project2/hello:1.0.0", crane.Insecure))

	r.NoError(crane.Push(img, imageRegistry+"/project1/hello:1.0.0", crane.Insecure, crane.WithAuth(project1auth)))
	r.NoError(crane.Push(img, imageRegistry+"/project2/hello:1.0.0", crane.Insecure, crane.WithAuth(project2auth)))

	// Build the component
	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			Provider: descriptor.Provider{Name: "opendefense.cloud"},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    component,
					Version: version,
				},
			},
			Resources: []descriptor.Resource{{
				Relation: descriptor.ExternalRelation,
				Name:     "hello1", Version: "1.0.0",
				Type: "ociImage",
				Access: &accessv1.OCIImage{
					Type:           runtime.NewVersionedType(accessv1.OCIImageType, accessv1.Version),
					ImageReference: "http://" + imageRegistry + "/project1/hello:1.0.0",
				},
			}, {
				Relation: descriptor.ExternalRelation,
				Name:     "hello2", Version: "1.0.0",
				Type: "ociImage",
				Access: &accessv1.OCIImage{
					Type:           runtime.NewVersionedType(accessv1.OCIImageType, accessv1.Version),
					ImageReference: "http://" + imageRegistry + "/project2/hello:1.0.0",
				},
			}},
		},
	}

	r.NoError(srcRepo.AddComponentVersion(ctx, desc))

	// Create http configuration

	imageHost, imagePort, err := net.SplitHostPort(imageRegistry)
	r.NoError(err)

	yamlConfig := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
  - type: credentials.config.ocm.software
    consumers:
      - identities:
          - type: OCIRegistry/v1
            hostname: %[1]s
            port: "%[2]s"
            path: project1/*
        credentials:
          - type: Credentials/v1
            properties:
              username: user1
              password: password1
      - identities:
          - type: OCIRegistry/v1
            hostname: %[1]s
            port: "%[2]s"
            path: project2/*
        credentials:
          - type: Credentials/v1
            properties:
              username: user2
              password: password2

  - type: http.config.ocm.software/v1alpha1
    timeout: 45s
    tlsHandshakeTimeout: 10s
    hosts:
      "%[3]s":
        plainHTTP: true
      "%[4]s":
        plainHTTP: true
      "%[5]s":
        plainHTTP: true
`, imageHost, imagePort, srcRegistry, targetRegistry, imageRegistry)

	fmt.Printf("%s\n", yamlConfig)

	var cfg genericv1.Config
	r.NoError(genericv1.Scheme.Decode(strings.NewReader(yamlConfig), &cfg))

	httpCfg, err := httpv1alpha1.ResolveHTTPConfig(&cfg)
	r.NoError(err)

	// Transfer

	uploaders := []transferv1alpha1.UploaderConfig{
		&transferv1alpha1.OCIUploaderConfig{
			Type: runtime.NewVersionedType(transferv1alpha1.OCIUploaderConfigType,
				transferv1alpha1.Version),
		},
	}

	tgd, err := transfer.BuildGraphDefinition(ctx, nil, uploaders,
		transfer.Mapping{
			Components: []transfer.ComponentID{{Component: component, Version: version}},
			Target:     targetSpec,
			Resolver:   transfer.NewRepositoryResolver(srcRepo, srcSpec),
		},
	)
	r.NoError(err)
	r.NotEmpty(tgd.Transformations)

	repoProvider := provider.NewComponentVersionRepositoryProvider(
		provider.WithHTTPConfig(httpCfg),
		provider.WithTempDir(t.TempDir()),
	)
	resourceRepo := resource.NewResourceRepository(nil, resource.WithHTTPConfig(httpCfg))

	identityScheme := runtime.NewScheme()
	ociidentityv1.MustRegisterIdentityType(identityScheme)
	credCfg, err := credentialsconfig.LookupCredentialConfig(&cfg)
	r.NoError(err)
	credResolver, err := credentials.ToGraph(ctx, credCfg, credentials.Options{
		ConsumerIdentityTypeScheme:     identityScheme,
		CredentialRepositoryTypeScheme: runtime.NewScheme(),
	})
	r.NoError(err)

	builder := transfer.NewDefaultBuilder(repoProvider, resourceRepo, credResolver, transfer.WithHTTPConfig(httpCfg))
	graph, err := builder.BuildAndCheck(tgd)
	r.NoError(err)
	r.NoError(graph.Process(ctx))

}
