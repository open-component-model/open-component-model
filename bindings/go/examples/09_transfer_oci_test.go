package examples

import (
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"os"
	"slices"
	"strings"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/oras-project/oras-go/v3"
	"github.com/oras-project/oras-go/v3/registry/remote"
	"github.com/oras-project/oras-go/v3/registry/remote/auth"
	orascreds "github.com/oras-project/oras-go/v3/registry/remote/credentials"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"golang.org/x/crypto/bcrypt"

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

const zotImage = "ghcr.io/project-zot/zot:%s"

// startZot starts a zot registry and returns its host:port. users maps a repository
// prefix (e.g. "project1") to the only basic-auth user allowed to access repositories
// below it. Without users the registry accepts anonymous access.
func startZot(t *testing.T, users map[string]orascreds.Credential) string {
	t.Helper()
	r := require.New(t)

	httpConfig := map[string]any{"address": "0.0.0.0", "port": "5000"}
	var files []testcontainers.ContainerFile
	if len(users) > 0 {
		var htpasswd strings.Builder
		repositories := map[string]any{}
		for _, prefix := range slices.Sorted(maps.Keys(users)) {
			user := users[prefix]
			hash, err := bcrypt.GenerateFromPassword([]byte(user.Password), bcrypt.DefaultCost)
			r.NoError(err)
			fmt.Fprintf(&htpasswd, "%s:%s\n", user.Username, hash)
			repositories[prefix+"/**"] = map[string]any{"policies": []any{map[string]any{
				"users":   []string{user.Username},
				"actions": []string{"read", "create", "update", "delete"},
			}}}
		}
		httpConfig["auth"] = map[string]any{"htpasswd": map[string]any{"path": "/etc/zot/htpasswd"}}
		httpConfig["accessControl"] = map[string]any{"repositories": repositories}
		files = append(files, testcontainers.ContainerFile{
			Reader: strings.NewReader(htpasswd.String()), ContainerFilePath: "/etc/zot/htpasswd", FileMode: 0o644,
		})
	}
	config, err := json.Marshal(map[string]any{
		"distSpecVersion": "1.1.1",
		"storage":         map[string]any{"rootDirectory": "/var/lib/registry"},
		"http":            httpConfig,
	})
	r.NoError(err)
	files = append(files, testcontainers.ContainerFile{
		Reader: strings.NewReader(string(config)), ContainerFilePath: "/etc/zot/config.json", FileMode: 0o644,
	})

	r.NotEmpty(os.Getenv("ZOT_VERSION"), "the ZOT_VERSION environment variable must be set")
	container, err := testcontainers.Run(t.Context(), fmt.Sprintf(zotImage, os.Getenv("ZOT_VERSION")),
		testcontainers.WithExposedPorts("5000/tcp"),
		testcontainers.WithFiles(files...),
		testcontainers.WithWaitStrategy(wait.ForHTTP("/v2/").WithPort("5000/tcp").
			WithStatusCodeMatcher(func(status int) bool { return status < 500 })),
	)
	testcontainers.CleanupContainer(t, container)
	r.NoError(err)
	endpoint, err := container.PortEndpoint(t.Context(), "5000/tcp", "")
	r.NoError(err)
	return endpoint
}

// pushImage pushes a minimal OCI image to repository:tag over plain HTTP with the given credentials.
func pushImage(t *testing.T, repository, tag string, credential orascreds.Credential) error {
	t.Helper()
	repo, err := remote.NewRepository(repository)
	require.NoError(t, err)
	repo.Registry.PlainHTTP = true
	repo.Registry.Client = &auth.Client{CredentialFunc: orascreds.StaticCredentialFunc(repo.Registry.Reference.Registry, credential)}

	layer, err := oras.PushBytes(t.Context(), repo, "text/plain", []byte("hello world"))
	if err != nil {
		return err
	}
	manifest, err := oras.PackManifest(t.Context(), repo, oras.PackManifestVersion1_1, "application/vnd.example.hello", oras.PackManifestOptions{Layers: []ocispec.Descriptor{layer}})
	if err != nil {
		return err
	}
	return repo.Tag(t.Context(), manifest, tag)
}

// TestOciTransfer tests building a CV referencing two OCI images stored in a remote registry,
// the `imageRegistry`. This registry requires authentication, and it requires different credentials
// for the two images we want to include. The CV is added to the `srcRegistry` and then transported
// to the `targetRegistry`, including the two OCI images from `imageRegistry`.
// The test demonstrates setting up an OCM Configuration for authentication and http config (plainHTTP)
// and transporting the CV.
//
// This test is skipped with -short because it requires Docker to spin up zot registries.
func TestOciTransfer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping OCI registry test in short mode (requires Docker)")
	}
	ctx := t.Context()
	r := require.New(t)
	component, version := "my.org/mycomponent", "1.0.0"

	srcRegistry := startZot(t, nil)

	imageRegistryUsers := map[string]orascreds.Credential{
		"project1": {Username: "user1", Password: "password1"},
		"project2": {Username: "user2", Password: "password2"},
	}
	imageRegistry := startZot(t, imageRegistryUsers)
	imageHost, imagePort, err := net.SplitHostPort(imageRegistry)
	r.NoError(err)

	targetRegistryUsers := map[string]orascreds.Credential{
		"project1": {Username: "user3", Password: "password3"},
		"project2": {Username: "user4", Password: "password4"},
	}
	targetRegistry := startZot(t, targetRegistryUsers)
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
            path: "{project1,project1/**}"
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
	var resources []descriptor.Resource
	for i, project := range []string{"project1", "project2"} {
		repository := imageRegistry + "/" + project + "/myimage/hello"
		r.Error(pushImage(t, repository, "1.0.0", orascreds.EmptyCredential), "push without credentials must be rejected")
		r.NoError(pushImage(t, repository, "1.0.0", imageRegistryUsers[project]))
		resources = append(resources, descriptor.Resource{
			Name: fmt.Sprintf("hello%d", i+1), Version: "1.0.0",
			Relation: descriptor.ExternalRelation,
			Type:     "ociImage",
			Access: &accessv1.OCIImage{
				Type:           runtime.NewVersionedType(accessv1.OCIImageType, accessv1.Version),
				ImageReference: repository + ":1.0.0",
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
