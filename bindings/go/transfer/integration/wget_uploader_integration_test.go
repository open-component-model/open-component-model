package integration_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
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
)

// Test_Integration_TransferWgetResource_UploaderStreamsToHTTPTarget verifies that a wget
// resource routed through an uploader configuration is streamed to an HTTP PUT target and
// re-described with a Wget access pointing at the target URL, with the digest computed during
// the stream. The target HTTP server accepts PUT (stores) and re-serves the object over GET.
func Test_Integration_TransferWgetResource_UploaderStreamsToHTTPTarget(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	resourceData := []byte("uploader streaming integration payload")

	// Source HTTP server hosting the resource content.
	sourceSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(resourceData)
	}))
	t.Cleanup(sourceSrv.Close)

	// Target HTTP server: PUT stores, GET re-serves.
	var mu sync.Mutex
	stored := map[string][]byte{}
	targetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.Method {
		case http.MethodPut:
			body, err := io.ReadAll(req.Body)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			mu.Lock()
			stored[req.URL.Path] = body
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
		case http.MethodGet:
			mu.Lock()
			body, ok := stored[req.URL.Path]
			mu.Unlock()
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write(body)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(targetSrv.Close)

	// Target OCI registry for the component descriptor.
	registryAddr, user, password := startRegistry(t)

	componentName := "ocm.software/wget-uploader-test"
	componentVersion := "1.0.0"
	sourceCTFPath := t.TempDir()
	ctfRepo := createCTFRepository(t, sourceCTFPath)

	wgetAccess := &wgetaccessv1.Wget{
		Type:      runtime.NewVersionedType(wgetaccess.WgetConsumerType, wgetaccessv1.Version),
		URL:       sourceSrv.URL + "/artifacts/blob.tar",
		MediaType: "application/octet-stream",
	}
	rawWgetAccess := &runtime.Raw{}
	r.NoError(runtime.NewScheme(runtime.WithAllowUnknown()).Convert(wgetAccess, rawWgetAccess))

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: componentName, Version: componentVersion},
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

	sourceSpec := &ctfrepospec.Repository{
		Type:     runtime.Type{Name: ctfrepospec.Type, Version: ctfrepospec.Version},
		FilePath: sourceCTFPath,
	}
	targetSpec := &ocirepospec.Repository{
		Type:    runtime.Type{Name: ocirepospec.Type, Version: "v1"},
		BaseUrl: fmt.Sprintf("http://%s", registryAddr),
	}

	// The uploader routes the Wget resource through the HTTP streaming transformer to the target server.
	stream, err := runtime.UnstructuredFromMixedData(map[string]any{
		"type":      "HTTPStreaming/v1alpha1",
		"targetURL": targetSrv.URL + "/uploads{{.path}}",
		"method":    http.MethodPut,
	})
	r.NoError(err)
	streamRaw := &runtime.Raw{}
	r.NoError(runtime.NewScheme(runtime.WithAllowUnknown()).Convert(stream, streamRaw))
	uploaders := []*transferv1alpha1.UploaderConfig{{
		Type:   runtime.NewVersionedType(transferv1alpha1.UploaderConfigType, transferv1alpha1.Version),
		Match:  transferv1alpha1.UploaderMatch{AccessType: runtime.NewVersionedType("Wget", "v1")},
		Stream: streamRaw,
	}}

	tgd, err := transfer.BuildGraphDefinition(t.Context(),
		&transferv1alpha1.Config{CopyMode: transferv1alpha1.CopyModeAllResources},
		uploaders,
		transfer.Mapping{
			Components: []transfer.ComponentID{{Component: componentName, Version: componentVersion}},
			Target:     targetSpec,
			Resolver:   transfer.NewRepositoryResolver(ctfRepo, sourceSpec),
		},
	)
	r.NoError(err)
	r.NotNil(tgd)

	ctx := t.Context()
	credResolver := newCredResolver(t, registryCreds{registryAddr, user, password})
	repoProvider := provider.NewComponentVersionRepositoryProvider(provider.WithTempDir(t.TempDir()))
	resourceRepo := wgetrepository.NewResourceRepository(nil)
	b := transfer.NewDefaultBuilder(repoProvider, resourceRepo, credResolver)
	graph, err := b.BuildAndCheck(tgd)
	r.NoError(err)
	r.NoError(graph.Process(ctx))

	// The target HTTP server must have received the streamed object.
	expectedPath := "/uploads/artifacts/blob.tar"
	mu.Lock()
	got, ok := stored[expectedPath]
	mu.Unlock()
	r.True(ok, "target server should have received a PUT at %s", expectedPath)
	r.Equal(resourceData, got, "streamed content must match the source content")

	// The transferred descriptor's resource must reference the target URL via a Wget access
	// and carry the digest computed during the stream.
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
	r.NoError(err)
	r.Len(gotDesc.Component.Resources, 1)
	gotResource := gotDesc.Component.Resources[0]

	gotAccess := gotResource.Access
	r.NotNil(gotAccess)
	r.Equal(wgetaccess.WgetConsumerType, gotAccess.GetType().Name,
		"uploaded resource should carry a Wget access after streaming")

	var typedWget wgetaccessv1.Wget
	r.NoError(wgetaccess.Scheme.Convert(gotAccess, &typedWget))
	r.Equal(targetSrv.URL+expectedPath, typedWget.URL, "resource access URL should point at the target upload URL")

	r.NotNil(gotResource.Digest, "transferred resource should carry a digest")
	r.Equal(digestOf(resourceData).Encoded(), gotResource.Digest.Value,
		"resource digest should be the sha256 of the streamed content")

	// The target server must re-serve exactly what was streamed.
	getResp, err := http.Get(targetSrv.URL + expectedPath)
	r.NoError(err)
	defer func() { r.NoError(getResp.Body.Close()) }()
	reserved, err := io.ReadAll(getResp.Body)
	r.NoError(err)
	r.True(bytes.Equal(resourceData, reserved), "target should re-serve the streamed content over GET")
}
