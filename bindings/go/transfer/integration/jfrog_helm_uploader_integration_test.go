package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	helminput "ocm.software/open-component-model/bindings/go/helm/input"
	helmresource "ocm.software/open-component-model/bindings/go/helm/repository/resource"
	helmaccess "ocm.software/open-component-model/bindings/go/helm/spec/access"
	helmaccessv1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	helminputv1 "ocm.software/open-component-model/bindings/go/helm/spec/input/v1"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ctfrepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ocirepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

// Test_Integration_TransferHelmResource_JFrogHelmUploaderDeploysChart verifies that a Helm/v1
// resource routed through a JFrog Helm uploader configuration is streamed as a chart archive
// to a fake Artifactory PUT endpoint and re-described with a Helm/v1 access pointing at the
// Artifactory Helm API, with the correct digest computed during the stream.
func Test_Integration_TransferHelmResource_JFrogHelmUploaderDeploysChart(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	// Read the expected chart .tgz bytes for later comparison.
	// helm/testdata/mychart-0.1.0.tgz is a symlink to provenance/mychart-0.1.0.tgz.
	chartTgzBytes, err := os.ReadFile("../../helm/testdata/mychart-0.1.0.tgz")
	r.NoError(err)
	r.NotEmpty(chartTgzBytes)

	// Source HTTP server: serves the provenance directory which contains the .tgz and .prov files.
	// Using the provenance dir because that's where the actual tgz lives (the root-level one is a symlink).
	// The helm downloader with helmChart "mychart-0.1.0.tgz" will GET /mychart-0.1.0.tgz directly.
	srcSrv := httptest.NewServer(http.FileServer(http.Dir("../../helm/testdata/provenance")))
	t.Cleanup(srcSrv.Close)

	// Target "Artifactory" HTTP server: stores PUT body + headers per path, returns 201.
	var mu sync.Mutex
	stored := map[string][]byte{}
	putHeaders := map[string]http.Header{}
	var reindexed []string
	targetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.Method {
		case http.MethodPut:
			if req.Header.Get("X-Checksum-Deploy") == "true" {
				// Artifactory has no content with this checksum yet.
				w.WriteHeader(http.StatusNotFound)
				return
			}
			body, err := io.ReadAll(req.Body)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			mu.Lock()
			stored[req.URL.Path] = body
			putHeaders[req.URL.Path] = req.Header.Clone()
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
		case http.MethodPost:
			mu.Lock()
			_, uploaded := stored["/artifactory/helm-local/mychart-0.1.0.tgz"]
			reindexed = append(reindexed, fmt.Sprintf("%s uploaded=%t", req.URL.Path, uploaded))
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(targetSrv.Close)

	// Target OCI registry for the component descriptor.
	registryAddr, user, password := startRegistry(t)

	componentName := "ocm.software/jfrog-helm-uploader-test"
	componentVersion := "1.0.0"
	sourceCTFPath := t.TempDir()
	ctfRepo := createCTFRepository(t, sourceCTFPath)

	// Build the Helm/v1 access as raw JSON.
	// helmChart is "mychart-0.1.0.tgz" (the file name) so the downloader GETs it directly
	// from the file server, without needing an index.yaml.
	helmAccessData, err := json.Marshal(map[string]string{
		"type":           "Helm/v1",
		"helmRepository": srcSrv.URL,
		"helmChart":      "mychart-0.1.0.tgz",
	})
	r.NoError(err)
	rawHelmAccess := &runtime.Raw{}
	r.NoError(rawHelmAccess.UnmarshalJSON(helmAccessData))

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
						// Deliberately differs from Chart.yaml: name and version come from the chart.
						ObjectMeta: descriptor.ObjectMeta{Name: "chart-resource", Version: "9.9.9"},
					},
					Type:     "helmChart",
					Relation: descriptor.ExternalRelation,
					Access:   rawHelmAccess,
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

	// JFrog Helm uploader routes Helm/v1 resources to the fake Artifactory server.
	uploaders := []transferv1alpha1.UploaderConfig{&transferv1alpha1.JFrogHelmUploaderConfig{
		Type:       runtime.NewVersionedType(transferv1alpha1.JFrogHelmUploaderConfigType, transferv1alpha1.Version),
		MatchSpec:  transferv1alpha1.UploaderMatch{AccessType: runtime.NewVersionedType(helmaccessv1.Type, helmaccessv1.LegacyTypeVersion)},
		URL:        targetSrv.URL,
		Repository: "helm-local",
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
	resourceRepo := helmresource.NewResourceRepository(nil)
	b := transfer.NewDefaultBuilder(repoProvider, resourceRepo, credResolver)
	graph, err := b.BuildAndCheck(tgd)
	r.NoError(err)
	r.NoError(graph.Process(ctx))

	// The target server must have received the chart at the expected Artifactory path.
	expectedPath := "/artifactory/helm-local/mychart-0.1.0.tgz"
	mu.Lock()
	got, ok := stored[expectedPath]
	gotHeaders := putHeaders[expectedPath]
	mu.Unlock()
	r.True(ok, "target server should have received a PUT at %s; stored paths: %v", expectedPath, storedKeys(stored))
	r.Equal(chartTgzBytes, got, "uploaded bytes must equal the chart .tgz")
	r.Equal("application/gzip", gotHeaders.Get("Content-Type"),
		"Content-Type must be application/gzip")
	mu.Lock()
	r.Equal([]string{"/artifactory/api/helm/helm-local/reindex uploaded=true"}, reindexed,
		"the helm index must be recalculated once, after the chart was deployed")
	mu.Unlock()

	// Verify the transferred descriptor in the target OCI registry.
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

	// The published access must be Helm/v1.
	r.NotNil(gotResource.Access)
	r.Equal(helmaccessv1.Type, gotResource.Access.GetType().Name,
		"uploaded resource should carry a Helm access")

	var typedHelm helmaccessv1.Helm
	r.NoError(helmaccess.Scheme.Convert(gotResource.Access, &typedHelm))
	r.Equal(targetSrv.URL+"/artifactory/api/helm/helm-local", typedHelm.HelmRepository,
		"helmRepository should point at the Artifactory Helm API")
	r.Equal("mychart:0.1.0", typedHelm.HelmChart,
		"helmChart should be <name>:<version>")

	// The digest must be genericBlobDigest/v1 of the chart .tgz.
	r.NotNil(gotResource.Digest, "transferred resource should carry a digest")
	r.Equal("SHA-256", gotResource.Digest.HashAlgorithm)
	r.Equal("genericBlobDigest/v1", gotResource.Digest.NormalisationAlgorithm)
	r.Equal(digestOf(chartTgzBytes).Encoded(), gotResource.Digest.Value,
		"digest value should be the sha256 of the chart .tgz")
}

// Test_Integration_TransferLocalBlobHelmResource_JFrogHelmUploaderDeploysChart verifies that a
// LocalBlob/v1 resource containing a Helm chart is correctly streamed to the fake Artifactory
// endpoint by the JFrog Helm uploader. Two media types are tested:
//   - packaged chart: the blob is the chart .tgz itself, uploaded unchanged.
//   - OCI layout: the blob is an OCI image layout (as produced by the helm input), and only
//     the chart layer is extracted and uploaded.
func Test_Integration_TransferLocalBlobHelmResource_JFrogHelmUploaderDeploysChart(t *testing.T) {
	t.Parallel()

	// Read the expected chart .tgz bytes for comparison.
	chartTgzBytes, err := os.ReadFile("../../helm/testdata/mychart-0.1.0.tgz")
	require.NoError(t, err)
	require.NotEmpty(t, chartTgzBytes)

	// Build the OCI layout blob using the real helm input path. Using the packaged tgz (not the
	// chart directory) ensures the chart layer bytes inside the layout match chartTgzBytes exactly.
	ociLayoutBlob, _, err := helminput.GetV1HelmBlob(t.Context(), helminputv1.Helm{
		Path: "../../helm/testdata/mychart-0.1.0.tgz",
	}, t.TempDir())
	require.NoError(t, err)

	ociLayoutRC, err := ociLayoutBlob.ReadCloser()
	require.NoError(t, err)
	ociLayoutBytes, err := io.ReadAll(ociLayoutRC)
	require.NoError(t, err)
	require.NoError(t, ociLayoutRC.Close())

	tests := []struct {
		name      string
		mediaType string
		blobData  []byte
		// wantBody is what the PUT body should equal. For a packaged chart it is the chart
		// tgz; for an OCI layout it is the chart layer (which equals the chart tgz).
		wantBody []byte
	}{
		{
			name:      "packaged chart",
			mediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip",
			blobData:  chartTgzBytes,
			wantBody:  chartTgzBytes,
		},
		{
			name:      "OCI layout",
			mediaType: "application/vnd.ocm.software.oci.layout.v1+tar+gzip",
			blobData:  ociLayoutBytes,
			wantBody:  chartTgzBytes,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			// Target "Artifactory" HTTP server.
			var mu sync.Mutex
			stored := map[string][]byte{}
			putHeaders := map[string]http.Header{}
			var reindexed []string
			targetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				switch req.Method {
				case http.MethodPut:
					if req.Header.Get("X-Checksum-Deploy") == "true" {
						// Artifactory has no content with this checksum yet.
						w.WriteHeader(http.StatusNotFound)
						return
					}
					body, err := io.ReadAll(req.Body)
					if err != nil {
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					mu.Lock()
					stored[req.URL.Path] = body
					putHeaders[req.URL.Path] = req.Header.Clone()
					mu.Unlock()
					w.WriteHeader(http.StatusCreated)
				case http.MethodPost:
					mu.Lock()
					_, uploaded := stored["/artifactory/helm-local/mychart-0.1.0.tgz"]
					reindexed = append(reindexed, fmt.Sprintf("%s uploaded=%t", req.URL.Path, uploaded))
					mu.Unlock()
					w.WriteHeader(http.StatusOK)
				default:
					w.WriteHeader(http.StatusMethodNotAllowed)
				}
			}))
			t.Cleanup(targetSrv.Close)

			// Target OCI registry for the component descriptor.
			registryAddr, user, password := startRegistry(t)

			componentName := "ocm.software/jfrog-helm-local-blob-test"
			componentVersion := "1.0.0"
			sourceCTFPath := t.TempDir()
			ctfRepo := createCTFRepository(t, sourceCTFPath)

			// Build a LocalBlob resource with the given media type.
			res := descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{Name: "mychart", Version: "0.1.0"},
				},
				Type:     "helmChart",
				Relation: descriptor.LocalRelation,
				Access: &descriptorv2.LocalBlob{
					Type:      runtime.NewVersionedType(descriptorv2.LocalBlobAccessType, descriptorv2.LocalBlobAccessTypeVersion),
					MediaType: tt.mediaType,
				},
			}
			updatedResource, err := ctfRepo.AddLocalResource(t.Context(), componentName, componentVersion, &res, inmemory.New(bytes.NewReader(tt.blobData), inmemory.WithMediaType(tt.mediaType)))
			r.NoError(err)

			desc := &descriptor.Descriptor{
				Meta: descriptor.Meta{Version: "v2"},
				Component: descriptor.Component{
					ComponentMeta: descriptor.ComponentMeta{
						ObjectMeta: descriptor.ObjectMeta{Name: componentName, Version: componentVersion},
					},
					Provider:  descriptor.Provider{Name: "test-provider"},
					Resources: []descriptor.Resource{*updatedResource},
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

			// JFrog Helm uploader routes LocalBlob/v1 resources to the fake Artifactory server.
			uploaders := []transferv1alpha1.UploaderConfig{&transferv1alpha1.JFrogHelmUploaderConfig{
				Type:       runtime.NewVersionedType(transferv1alpha1.JFrogHelmUploaderConfigType, transferv1alpha1.Version),
				MatchSpec:  transferv1alpha1.UploaderMatch{AccessType: runtime.NewVersionedType(descriptorv2.LocalBlobAccessType, descriptorv2.LocalBlobAccessTypeVersion)},
				URL:        targetSrv.URL,
				Repository: "helm-local",
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
			resourceRepo := helmresource.NewResourceRepository(nil)
			b := transfer.NewDefaultBuilder(repoProvider, resourceRepo, credResolver)
			graph, err := b.BuildAndCheck(tgd)
			r.NoError(err)
			r.NoError(graph.Process(ctx))

			// The target server must have received the chart at the expected Artifactory path.
			expectedPath := "/artifactory/helm-local/mychart-0.1.0.tgz"
			mu.Lock()
			got, ok := stored[expectedPath]
			gotHeaders := putHeaders[expectedPath]
			mu.Unlock()
			r.True(ok, "target server should have received a PUT at %s; stored paths: %v", expectedPath, storedKeys(stored))
			r.Equal(tt.wantBody, got, "uploaded bytes must equal the chart .tgz")
			r.Equal("application/gzip", gotHeaders.Get("Content-Type"),
				"Content-Type must be application/gzip")
			mu.Lock()
			r.Equal([]string{"/artifactory/api/helm/helm-local/reindex uploaded=true"}, reindexed,
				"the helm index must be recalculated once, after the chart was deployed")
			mu.Unlock()

			// Verify the transferred descriptor in the target OCI registry.
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

			// The published access must be Helm/v1.
			r.NotNil(gotResource.Access)
			r.Equal(helmaccessv1.Type, gotResource.Access.GetType().Name,
				"uploaded resource should carry a Helm access")

			var typedHelm helmaccessv1.Helm
			r.NoError(helmaccess.Scheme.Convert(gotResource.Access, &typedHelm))
			r.Equal(targetSrv.URL+"/artifactory/api/helm/helm-local", typedHelm.HelmRepository,
				"helmRepository should point at the Artifactory Helm API")
			r.Equal("mychart:0.1.0", typedHelm.HelmChart,
				"helmChart should be <name>:<version>")

			// The digest must be genericBlobDigest/v1 of the chart .tgz.
			r.NotNil(gotResource.Digest, "transferred resource should carry a digest")
			r.Equal("SHA-256", gotResource.Digest.HashAlgorithm)
			r.Equal("genericBlobDigest/v1", gotResource.Digest.NormalisationAlgorithm)
			r.Equal(digestOf(tt.wantBody).Encoded(), gotResource.Digest.Value,
				"digest value should be the sha256 of the chart .tgz")
		})
	}
}

// storedKeys returns the keys of a map for diagnostic messages.
func storedKeys(m map[string][]byte) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
