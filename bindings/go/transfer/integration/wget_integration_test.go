package integration_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ctfrepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ocirepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	wgetrepository "ocm.software/open-component-model/bindings/go/wget/repository"
	wgetaccess "ocm.software/open-component-model/bindings/go/wget/spec/access"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
	wgetv1alpha1 "ocm.software/open-component-model/bindings/go/wget/transformation/spec/v1alpha1"
)

// Test_Integration_TransferWgetResource_CopyModeAllResources verifies that a wget resource (content
// behind an HTTP URL, not stored in the source CTF) is transferred by value: the content is
// downloaded from the URL and embedded as a localBlob in the target OCI registry. wget resources are
// external, so they are only copied under CopyModeAllResources.
func Test_Integration_TransferWgetResource_CopyModeAllResources(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	// 1. Serve the resource content over HTTP.
	resourceData := []byte("Hello from wget integration test!")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write(resourceData)
	}))
	t.Cleanup(srv.Close)

	// 2. Start the target OCI registry.
	registryAddr, user, password := startRegistry(t)

	// 3. Create the source CTF with a component whose resource points at the HTTP server.
	componentName := "ocm.software/wget-resource-test"
	componentVersion := "1.0.0"
	sourceCTFPath := t.TempDir()
	ctfRepo := createCTFRepository(t, sourceCTFPath)

	// The wget access lives in a separate binding the CTF/OCI repository scheme does not know,
	// so store it pre-encoded as raw JSON (how descriptors carry access on disk). The transfer
	// discovery decodes it via its own scheme, which registers the wget access type.
	wgetAccess := &wgetaccessv1.Wget{
		Type:      runtime.NewVersionedType(wgetaccess.WgetConsumerType, wgetaccessv1.Version),
		URL:       srv.URL + "/artifact.txt",
		MediaType: "text/plain",
	}
	rawWgetAccess := &runtime.Raw{}
	r.NoError(runtime.NewScheme(runtime.WithAllowUnknown()).Convert(wgetAccess, rawWgetAccess))

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    componentName,
					Version: componentVersion,
				},
			},
			Provider: descriptor.Provider{Name: "test-provider"},
			Resources: []descriptor.Resource{
				{
					ElementMeta: descriptor.ElementMeta{
						ObjectMeta: descriptor.ObjectMeta{Name: "wget-resource", Version: "1.0.0"},
					},
					Type:     "blob",
					Relation: descriptor.ExternalRelation,
					Access:   rawWgetAccess,
				},
			},
		},
	}
	r.NoError(ctfRepo.AddComponentVersion(t.Context(), desc))

	// 4. Build the transfer graph with CopyModeAllResources (external resources are skipped otherwise).
	sourceSpec := &ctfrepospec.Repository{
		Type:     runtime.Type{Name: ctfrepospec.Type, Version: ctfrepospec.Version},
		FilePath: sourceCTFPath,
	}
	targetSpec := &ocirepospec.Repository{
		Type:    runtime.Type{Name: ocirepospec.Type, Version: "v1"},
		BaseUrl: fmt.Sprintf("http://%s", registryAddr),
	}

	tgd, err := transfer.BuildGraphDefinition(t.Context(),
		&transferv1alpha1.Config{CopyMode: transferv1alpha1.CopyModeAllResources},
		transfer.Mapping{
			Components: []transfer.ComponentID{{Component: componentName, Version: componentVersion}},
			Target:     targetSpec,
			Resolver:   transfer.NewRepositoryResolver(ctfRepo, sourceSpec),
		},
	)
	r.NoError(err)
	r.NotNil(tgd)

	// A wget resource should generate a DownloadWgetResource transformation.
	hasDownloadWget := false
	for _, tr := range tgd.Transformations {
		if tr.Type.Name == wgetv1alpha1.DownloadWgetResourceType {
			hasDownloadWget = true
			break
		}
	}
	r.True(hasDownloadWget, "wget resource should generate a DownloadWgetResource transformation")

	// 5. Build and execute the graph.
	ctx := t.Context()
	credResolver := newCredResolver(t, registryCreds{registryAddr, user, password})
	repoProvider := provider.NewComponentVersionRepositoryProvider(provider.WithTempDir(t.TempDir()))
	// A wget resource is downloaded via the wget resource repository. In the CLI this is a dynamic
	// registry dispatching by access type; here the transfer only involves a wget resource, so the
	// wget repository is the concrete repo to provide (mirrors the OCI tests passing the OCI repo).
	resourceRepo := wgetrepository.NewResourceRepository(nil)
	b := transfer.NewDefaultBuilder(repoProvider, resourceRepo, credResolver)
	graph, err := b.BuildAndCheck(tgd)
	r.NoError(err)
	r.NoError(graph.Process(ctx))

	// 6. Verify the component arrived in the target registry with the resource stored as a localBlob.
	client := createAuthClient(registryAddr, user, password)
	urlRes, err := urlresolver.New(
		urlresolver.WithBaseURL(registryAddr),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)
	targetRepo, err := oci.NewRepository(oci.WithResolver(urlRes), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	gotDesc, err := targetRepo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err, "should find transferred component in target registry")
	r.Equal(componentName, gotDesc.Component.Name)
	r.Len(gotDesc.Component.Resources, 1)
	r.Equal("wget-resource", gotDesc.Component.Resources[0].Name)

	gotAccess := gotDesc.Component.Resources[0].Access
	r.NotNil(gotAccess, "resource access should not be nil")
	r.Equal(descriptorv2.LocalBlobAccessType, gotAccess.GetType().Name,
		"wget resource should be stored as localBlob in target after transfer")

	// Verify GlobalAccess is not set — transfer should produce a pure local blob without global access.
	accessScheme := runtime.NewScheme(runtime.WithAllowUnknown())
	descriptorv2.MustAddToScheme(accessScheme)
	var typedLocalBlob descriptorv2.LocalBlob
	r.NoError(accessScheme.Convert(gotAccess, &typedLocalBlob), "should convert access to LocalBlob")
	r.Nil(typedLocalBlob.GlobalAccess, "localBlob should not have globalAccess after transfer")
	r.Equal("text/plain", typedLocalBlob.MediaType, "localBlob should preserve the wget media type")

	// The stored resource must be fully described: a digest over the downloaded content.
	r.NotNil(gotDesc.Component.Resources[0].Digest, "transferred resource should carry a digest")
	r.Equal(digestOf(resourceData).Encoded(), gotDesc.Component.Resources[0].Digest.Value,
		"resource digest should be the sha256 of the served content")

	// 7. Verify the blob is present and its content matches what the HTTP server served.
	resourceIdentity := gotDesc.Component.Resources[0].ToIdentity()
	localBlob, _, err := targetRepo.GetLocalResource(ctx, componentName, componentVersion, resourceIdentity)
	r.NoError(err, "local blob should be retrievable from target repository")
	reader, err := localBlob.ReadCloser()
	r.NoError(err, "local blob should be readable")
	defer func() { r.NoError(reader.Close()) }()
	content, err := io.ReadAll(reader)
	r.NoError(err)
	r.Equal(resourceData, content, "transferred blob content should match the served content")
}
