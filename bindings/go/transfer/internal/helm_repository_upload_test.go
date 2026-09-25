package internal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/helm/chartarchive"
	helmaccess "ocm.software/open-component-model/bindings/go/helm/spec/access"
	helmaccessv1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	helmidentityv1 "ocm.software/open-component-model/bindings/go/helm/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	wgetcredsv1 "ocm.software/open-component-model/bindings/go/wget/spec/credentials/v1"
	wgetidentityv1 "ocm.software/open-component-model/bindings/go/wget/spec/identity/v1"
)

// chartResourceRepo serves the chart from DownloadResource and derives no source credential identity.
type chartResourceRepo struct {
	repository.ResourceRepository
	chart []byte
}

func (s *chartResourceRepo) GetResourceCredentialConsumerIdentity(context.Context, *descriptor.Resource) (runtime.Identity, error) {
	return nil, nil
}

func (s *chartResourceRepo) DownloadResource(context.Context, *descriptor.Resource, runtime.Typed) (blob.ReadOnlyBlob, error) {
	return inmemory.New(bytes.NewReader(s.chart), inmemory.WithSize(int64(len(s.chart)))), nil
}

// credentialsByType resolves credentials by the type attribute of the consumer identity.
type credentialsByType map[string]runtime.Typed

func (c credentialsByType) Resolve(_ context.Context, id runtime.Identity) (runtime.Typed, error) {
	if creds, ok := c[id[runtime.IdentityAttributeType]]; ok {
		return creds, nil
	}
	return nil, credentials.ErrNotFound
}

// localChartRepo serves the chart as a local resource and records the request.
type localChartRepo struct {
	repository.ComponentVersionRepository
	chart              []byte
	component, version string
	identity           runtime.Identity
}

func (l *localChartRepo) GetLocalResource(_ context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	l.component, l.version, l.identity = component, version, identity
	return inmemory.New(bytes.NewReader(l.chart)), nil, nil
}

type localChartRepoProvider struct {
	repository.ComponentVersionRepositoryProvider
	repo *localChartRepo
}

func (p *localChartRepoProvider) GetComponentVersionRepositoryCredentialConsumerIdentity(context.Context, runtime.Typed) (runtime.Identity, error) {
	return nil, errors.New("no identity")
}

func (p *localChartRepoProvider) GetComponentVersionRepository(context.Context, runtime.Typed, runtime.Typed) (repository.ComponentVersionRepository, error) {
	return p.repo, nil
}

type artifactoryRequest struct {
	method, path, contentType string
	username, password        string
	basic                     bool
	authorization             string
	checksum                  string
	deploy                    bool
	body                      []byte
}

// fakeArtifactory emulates the parts of an Artifactory Helm repository the uploader uses. Like
// Artifactory, it records chart name and version properties for deployed content it recognizes
// as a chart; here, recognition is a lookup of the content digest in charts.
type fakeArtifactory struct {
	*httptest.Server
	charts        map[string][2]string // sha256 -> name, version
	reindexStatus int

	mu       sync.Mutex
	requests []artifactoryRequest
	contents map[string]bool   // sha256 of stored content
	paths    map[string]string // repository path -> sha256
}

func newFakeArtifactory(t *testing.T, charts map[string][2]string) *fakeArtifactory {
	t.Helper()
	f := &fakeArtifactory{charts: charts, reindexStatus: http.StatusOK, contents: map[string]bool{}, paths: map[string]string{}}
	f.Server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeArtifactory) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	req := artifactoryRequest{method: r.Method, path: r.URL.Path, contentType: r.Header.Get("Content-Type"), authorization: r.Header.Get("Authorization"),
		checksum: r.Header.Get("X-Checksum-Sha256"), deploy: r.Header.Get("X-Checksum-Deploy") == "true", body: body}
	req.username, req.password, req.basic = r.BasicAuth()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)

	const repoPrefix, storagePrefix = "/artifactory/helm-local/", "/artifactory/api/storage/helm-local/"
	switch {
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/artifactory/api/helm/helm-local/") && strings.HasSuffix(r.URL.Path, "/reindex"):
		w.WriteHeader(f.reindexStatus)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, storagePrefix):
		chart, ok := f.charts[f.paths[strings.TrimPrefix(r.URL.Path, storagePrefix)]]
		if !ok {
			http.Error(w, `{"errors":[{"status":404,"message":"No properties could be found."}]}`, http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"properties": map[string][]string{"chart.name": {chart[0]}, "chart.version": {chart[1]}}})
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, repoPrefix):
		delete(f.paths, strings.TrimPrefix(r.URL.Path, repoPrefix))
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, repoPrefix):
		path := strings.TrimPrefix(r.URL.Path, repoPrefix)
		if req.deploy {
			// Deploy by checksum succeeds only for content Artifactory already stores.
			if !f.contents[req.checksum] {
				http.NotFound(w, r)
				return
			}
			f.paths[path] = req.checksum
			w.WriteHeader(http.StatusCreated)
			return
		}
		sum := sha256.Sum256(body)
		digest := hex.EncodeToString(sum[:])
		// Like Artifactory, reject a body that does not match the announced checksum.
		if req.checksum != "" && req.checksum != digest {
			http.Error(w, "checksum mismatch", http.StatusConflict)
			return
		}
		f.contents[digest] = true
		f.paths[path] = digest
		w.WriteHeader(http.StatusCreated)
	default:
		http.Error(w, "unexpected request", http.StatusBadRequest)
	}
}

func (f *fakeArtifactory) recorded() []artifactoryRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]artifactoryRequest(nil), f.requests...)
}

func (f *fakeArtifactory) stored(path string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.paths[path]
	return ok
}

func TestHelmRepositoryUpload_Transform_Artifactory(t *testing.T) {
	chartTGZ, err := os.ReadFile("../../helm/testdata/mychart-0.1.0.tgz")
	require.NoError(t, err)
	sum := sha256.Sum256(chartTGZ)
	chartDigest := hex.EncodeToString(sum[:])
	charts := map[string][2]string{chartDigest: {"mychart", "0.1.0"}}

	scheme := runtime.NewScheme()
	scheme.MustRegisterWithAlias(&HelmRepositoryUploadTransformation{}, HelmRepositoryUploadVersionedType)
	scheme.MustRegisterScheme(helmaccess.Scheme)

	const (
		chartPath   = "ocm.software/test/1.0.0/renamed-9.9.9.tgz"
		putPath     = "/artifactory/helm-local/" + chartPath
		storagePath = "/artifactory/api/storage/helm-local/" + chartPath
		reindexPath = "/artifactory/api/helm/helm-local/" + chartPath + "/reindex"
	)
	source := func() *descriptorv2.Resource {
		return &descriptorv2.Resource{
			ElementMeta: descriptorv2.ElementMeta{Name: "renamed", Version: "9.9.9"},
			Type:        "helmChart",
			Relation:    descriptorv2.ExternalRelation,
			Access:      &runtime.Raw{Type: runtime.NewVersionedType("Wget", "v1"), Data: []byte(`{"type":"Wget/v1","url":"https://charts.example/mychart-0.1.0.tgz"}`)},
		}
	}
	step := func(url string, reindex bool, res *descriptorv2.Resource) *HelmRepositoryUploadTransformation {
		return &HelmRepositoryUploadTransformation{
			Type: HelmRepositoryUploadVersionedType,
			ID:   "upload",
			Spec: &HelmRepositoryUploadSpec{
				Resource:         res,
				ComponentVersion: &HelmRepositoryUploadComponentVersion{Component: "ocm.software/test", Version: "1.0.0"},
				Server:           transferv1alpha1.HelmRepositoryServerArtifactory,
				URL:              url,
				Repository:       "helm-local",
				Reindex:          reindex,
			},
		}
	}
	transformerFor := func(content []byte, creds credentials.Resolver) *HelmRepositoryUpload {
		repo := &chartResourceRepo{chart: content}
		return &HelmRepositoryUpload{
			Scheme:                  scheme,
			Charts:                  &chartarchive.Source{ResourceRepository: repo},
			ResourceRepository:      repo,
			CredentialProvider:      creds,
			chartMetadataInterval: time.Millisecond,
		}
	}
	transformer := func(creds credentials.Resolver) *HelmRepositoryUpload { return transformerFor(chartTGZ, creds) }
	methods := func(reqs []artifactoryRequest) []string {
		var out []string
		for _, req := range reqs {
			out = append(out, req.method+" "+req.path)
		}
		return out
	}
	helmChart := func(r *require.Assertions, out runtime.Typed) string {
		var access helmaccessv1.Helm
		r.NoError(helmaccess.Scheme.Convert(out.(*HelmRepositoryUploadTransformation).Output.Resource.Access, &access))
		return access.HelmChart
	}
	helmCreds := &helmcredsv1.HelmHTTPCredentials{Type: runtime.NewVersionedType(helmcredsv1.HelmHTTPCredentialsType, helmcredsv1.Version), Username: "helm-user", Password: "helm-pass"}
	wgetCreds := &wgetcredsv1.WgetCredentials{Type: wgetcredsv1.WgetCredentialsVersionedType, IdentityToken: "wget-token"}
	helmType, wgetType := helmidentityv1.Type.String(), wgetidentityv1.Type.String()

	t.Run("stores the chart under the component version and publishes the name artifactory records", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, charts)
		out, err := transformer(nil).Transform(t.Context(), step(srv.URL, true, source()))
		r.NoError(err)

		got := srv.recorded()
		r.Equal([]string{"PUT " + putPath, "GET " + storagePath, "POST " + reindexPath}, methods(got))
		r.Equal("application/gzip", got[0].contentType)
		r.Equal(chartTGZ, got[0].body)
		r.Empty(got[0].checksum, "without a source digest there is no checksum to announce")

		res := out.(*HelmRepositoryUploadTransformation).Output.Resource
		r.Equal("renamed", res.Name)
		var access helmaccessv1.Helm
		r.NoError(helmaccess.Scheme.Convert(res.Access, &access))
		r.Equal(srv.URL+"/artifactory/api/helm/helm-local", access.HelmRepository)
		r.Equal("mychart:0.1.0", access.HelmChart, "name and version come from artifactory, not from the resource")
		r.Equal(&descriptorv2.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: chartDigest}, res.Digest)
	})

	t.Run("no reindex when disabled", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, charts)
		_, err := transformer(nil).Transform(t.Context(), step(srv.URL, false, source()))
		r.NoError(err)
		r.Equal([]string{"PUT " + putPath, "GET " + storagePath}, methods(srv.recorded()))
	})

	t.Run("a failing reindex does not fail the upload", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, charts)
		srv.reindexStatus = http.StatusForbidden
		out, err := transformer(nil).Transform(t.Context(), step(srv.URL, true, source()))
		r.NoError(err)
		r.Equal("mychart:0.1.0", helmChart(r, out))
	})

	t.Run("content artifactory does not recognize as a chart is deleted and fails", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, charts)
		notAChart := []byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xff, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
		_, err := transformerFor(notAChart, nil).Transform(t.Context(), step(srv.URL, true, source()))
		r.ErrorContains(err, "is not a helm chart: artifactory recorded no chart name and version")
		got := methods(srv.recorded())
		r.Equal("PUT "+putPath, got[0])
		r.Equal("GET "+storagePath, got[1])
		r.Len(got, 2+chartMetadataAttempts, "properties are polled, then the file is deleted")
		r.Equal("DELETE "+putPath, got[len(got)-1])
		r.False(srv.stored(chartPath))
	})

	credTests := []struct {
		name      string
		creds     credentialsByType
		wantBasic []string
		wantAuth  string
		wantErr   string
	}{
		{name: "HelmChartRepository HelmHTTPCredentials use basic auth", creds: credentialsByType{helmType: helmCreds}, wantBasic: []string{"helm-user", "helm-pass"}},
		{name: "Wget WgetCredentials use a bearer token", creds: credentialsByType{wgetType: wgetCreds}, wantAuth: "Bearer wget-token"},
		{name: "HelmChartRepository credentials win over Wget", creds: credentialsByType{helmType: helmCreds, wgetType: wgetCreds}, wantBasic: []string{"helm-user", "helm-pass"}},
		{name: "HelmHTTPCredentials client certificates are rejected", creds: credentialsByType{helmType: &helmcredsv1.HelmHTTPCredentials{
			Type: runtime.NewVersionedType(helmcredsv1.HelmHTTPCredentialsType, helmcredsv1.Version), CertFile: "/cert.pem", KeyFile: "/key.pem",
		}}, wantErr: "HelmHTTPCredentials certFile/keyFile are not supported for helm repository uploads"},
	}
	for _, tt := range credTests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			srv := newFakeArtifactory(t, charts)
			_, err := transformer(tt.creds).Transform(t.Context(), step(srv.URL, true, source()))
			if tt.wantErr != "" {
				r.ErrorContains(err, tt.wantErr)
				r.Empty(srv.recorded(), "no request may be sent")
				return
			}
			r.NoError(err)
			got := srv.recorded()
			r.Len(got, 3)
			for _, req := range got {
				if tt.wantBasic != nil {
					r.True(req.basic, "%s %s must use basic auth", req.method, req.path)
					r.Equal(tt.wantBasic, []string{req.username, req.password})
				} else {
					r.Equal(tt.wantAuth, req.authorization)
				}
			}
		})
	}

	t.Run("source digest is announced as checksum and deployed by checksum on re-transfer", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, charts)
		res := source()
		res.Digest = &descriptorv2.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: chartDigest}
		out, err := transformer(nil).Transform(t.Context(), step(srv.URL, false, res))
		r.NoError(err)
		got := srv.recorded()
		r.Equal([]string{"PUT " + putPath, "PUT " + putPath, "GET " + storagePath}, methods(got))
		r.True(got[0].deploy, "deploy by checksum is tried first")
		r.Empty(got[0].body)
		r.False(got[1].deploy)
		r.Equal(chartDigest, got[1].checksum)
		r.Equal(chartTGZ, got[1].body)
		r.Equal(res.Digest, out.(*HelmRepositoryUploadTransformation).Output.Resource.Digest)

		// Artifactory now stores the chart, so a second transfer does not upload it again.
		out, err = transformer(nil).Transform(t.Context(), step(srv.URL, false, res))
		r.NoError(err)
		got = srv.recorded()[3:]
		r.Equal([]string{"PUT " + putPath, "GET " + storagePath}, methods(got))
		r.True(got[0].deploy)
		r.Empty(got[0].body)
		r.Equal("mychart:0.1.0", helmChart(r, out))
		r.Equal(res.Digest, out.(*HelmRepositoryUploadTransformation).Output.Resource.Digest)
	})

	t.Run("source digest mismatch is rejected by artifactory", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, charts)
		res := source()
		res.Digest = &descriptorv2.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: "0000"}
		_, err := transformer(nil).Transform(t.Context(), step(srv.URL, true, res))
		r.ErrorContains(err, "returned status 409")
		r.Equal([]string{"PUT " + putPath, "PUT " + putPath}, methods(srv.recorded()), "deploy by checksum and the rejected upload, nothing else")
		r.False(srv.stored(chartPath))
	})

	t.Run("unsupported source digest fails before uploading", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, charts)
		res := source()
		res.Digest = &descriptorv2.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "ociArtifactDigest/v1", Value: chartDigest}
		_, err := transformer(nil).Transform(t.Context(), step(srv.URL, true, res))
		r.ErrorContains(err, "unsupported normalisation algorithm")
		r.Empty(srv.recorded())
	})

	t.Run("extra identity gets its own path", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, charts)
		res := source()
		res.ExtraIdentity = runtime.Identity{"arch": "arm64"}
		_, err := transformer(nil).Transform(t.Context(), step(srv.URL, false, res))
		r.NoError(err)
		put := srv.recorded()[0].path
		r.True(strings.HasPrefix(put, "/artifactory/helm-local/ocm.software/test/1.0.0/renamed-9.9.9-"), put)
		r.NotEqual(putPath, put)
	})

	t.Run("resource names cannot escape the component version path", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, charts)
		res := source()
		res.Name = "../../other"
		_, err := transformer(nil).Transform(t.Context(), step(srv.URL, true, res))
		r.ErrorContains(err, "must not contain path separators")
		r.Empty(srv.recorded())
	})

	t.Run("local blob without repository provider", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, charts)
		s := step(srv.URL, false, source())
		s.Spec.ComponentVersion.Repository = &runtime.Raw{Type: runtime.NewVersionedType("OCIRepository", "v1"), Data: []byte(`{"type":"OCIRepository/v1","baseUrl":"ghcr.io/source"}`)}
		_, err := transformer(nil).Transform(t.Context(), s)
		r.ErrorContains(err, "no component version repository provider configured")
		r.Empty(srv.recorded())
	})

	t.Run("local blob is read from the source component version", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, charts)
		repo := &localChartRepo{chart: chartTGZ}
		tr := transformer(credentialsByType{})
		tr.RepoProvider = &localChartRepoProvider{repo: repo}
		res := source()
		res.Access = &runtime.Raw{Type: runtime.NewVersionedType("LocalBlob", "v1"), Data: []byte(`{"type":"LocalBlob/v1","localReference":"sha256:abc","mediaType":"application/vnd.cncf.helm.chart.content.v1.tar+gzip"}`)}
		s := step(srv.URL, false, res)
		s.Spec.ComponentVersion.Repository = &runtime.Raw{Type: runtime.NewVersionedType("OCIRepository", "v1"), Data: []byte(`{"type":"OCIRepository/v1","baseUrl":"ghcr.io/source"}`)}
		out, err := tr.Transform(t.Context(), s)
		r.NoError(err)
		r.Equal("ocm.software/test", repo.component)
		r.Equal("1.0.0", repo.version)
		r.Equal(runtime.Identity{"name": "renamed", "version": "9.9.9"}, repo.identity)
		got := srv.recorded()
		r.Equal("PUT "+putPath, methods(got)[0])
		r.Equal(chartTGZ, got[0].body)
		r.Equal("mychart:0.1.0", helmChart(r, out))
	})
}

// fakeNexus emulates the parts of a Nexus Repository 3 Helm hosted repository the uploader uses.
// Like Nexus, it stores an uploaded chart under <name>-<version> from the chart, ignoring the
// uploaded file name, and finds stored charts by the SHA-256 of their content; here, the chart
// name and version are a lookup of the content digest in charts.
type fakeNexus struct {
	*httptest.Server
	charts    map[string][2]string // sha256 -> name, version
	basePath  string
	allowOnce bool

	mu       sync.Mutex
	requests []string
	stored   map[string]string // <name>-<version> -> sha256
}

func newFakeNexus(t *testing.T, charts map[string][2]string, basePath string, allowOnce bool) *fakeNexus {
	t.Helper()
	f := &fakeNexus{charts: charts, basePath: basePath, allowOnce: allowOnce, stored: map[string]string{}}
	f.Server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeNexus) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)

	switch {
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, f.basePath+"/repository/helm-hosted/"):
		sum := sha256.Sum256(body)
		digest := hex.EncodeToString(sum[:])
		chart, ok := f.charts[digest]
		if !ok {
			// Nexus fails to read Chart.yaml from content that is not a chart.
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		key := chart[0] + "-" + chart[1]
		if _, exists := f.stored[key]; exists && f.allowOnce {
			http.Error(w, "helm-hosted/"+key+".tgz -  cannot be updated as asset already exists and redeploy is not allowed", http.StatusConflict)
			return
		}
		f.stored[key] = digest
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodGet && r.URL.Path == f.basePath+"/service/rest/v1/search":
		q := r.URL.Query()
		items := []map[string]string{}
		if q.Get("repository") == "helm-hosted" && q.Get("format") == "helm" {
			for key, digest := range f.stored {
				if digest != q.Get("sha256") {
					continue
				}
				for _, chart := range f.charts {
					if chart[0]+"-"+chart[1] == key {
						items = append(items, map[string]string{"id": "c-" + digest[:8], "format": "helm", "name": chart[0], "version": chart[1]})
					}
				}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "continuationToken": nil})
	default:
		http.Error(w, "unexpected request", http.StatusBadRequest)
	}
}

// store records content as stored under <name>-<version>, as an earlier upload would.
func (f *fakeNexus) store(key, digest string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stored[key] = digest
}

func (f *fakeNexus) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

func TestHelmRepositoryUpload_Transform_Nexus(t *testing.T) {
	chartTGZ, err := os.ReadFile("../../helm/testdata/mychart-0.1.0.tgz")
	require.NoError(t, err)
	sum := sha256.Sum256(chartTGZ)
	chartDigest := hex.EncodeToString(sum[:])
	charts := map[string][2]string{chartDigest: {"mychart", "0.1.0"}}

	scheme := runtime.NewScheme()
	scheme.MustRegisterWithAlias(&HelmRepositoryUploadTransformation{}, HelmRepositoryUploadVersionedType)
	scheme.MustRegisterScheme(helmaccess.Scheme)

	const (
		putPath    = "/repository/helm-hosted/renamed-9.9.9.tgz"
		searchPath = "/service/rest/v1/search"
	)
	source := func(digest string) *descriptorv2.Resource {
		res := &descriptorv2.Resource{
			ElementMeta: descriptorv2.ElementMeta{Name: "renamed", Version: "9.9.9"},
			Type:        "helmChart",
			Relation:    descriptorv2.ExternalRelation,
			Access:      &runtime.Raw{Type: runtime.NewVersionedType("Wget", "v1"), Data: []byte(`{"type":"Wget/v1","url":"https://charts.example/mychart-0.1.0.tgz"}`)},
		}
		if digest != "" {
			res.Digest = &descriptorv2.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: digest}
		}
		return res
	}
	transform := func(t *testing.T, url string, res *descriptorv2.Resource) (*HelmRepositoryUploadTransformation, error) {
		repo := &chartResourceRepo{chart: chartTGZ}
		tr := &HelmRepositoryUpload{
			Scheme:                scheme,
			Charts:                &chartarchive.Source{ResourceRepository: repo},
			ResourceRepository:    repo,
			chartMetadataInterval: time.Millisecond,
		}
		out, err := tr.Transform(t.Context(), &HelmRepositoryUploadTransformation{
			Type: HelmRepositoryUploadVersionedType,
			ID:   "upload",
			Spec: &HelmRepositoryUploadSpec{
				Resource:         res,
				ComponentVersion: &HelmRepositoryUploadComponentVersion{Component: "ocm.software/test", Version: "1.0.0"},
				Server:           transferv1alpha1.HelmRepositoryServerNexus,
				URL:              url,
				Repository:       "helm-hosted",
			},
		})
		if err != nil {
			return nil, err
		}
		return out.(*HelmRepositoryUploadTransformation), nil
	}
	access := func(r *require.Assertions, out *HelmRepositoryUploadTransformation) helmaccessv1.Helm {
		var access helmaccessv1.Helm
		r.NoError(helmaccess.Scheme.Convert(out.Output.Resource.Access, &access))
		return access
	}

	t.Run("uploads to the repository root and publishes the chart nexus stores", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeNexus(t, charts, "", false)
		out, err := transform(t, srv.URL, source(""))
		r.NoError(err)
		r.Equal([]string{"PUT " + putPath, "GET " + searchPath}, srv.recorded())
		a := access(r, out)
		r.Equal(srv.URL+"/repository/helm-hosted", a.HelmRepository)
		r.Equal("mychart:0.1.0", a.HelmChart, "name and version come from nexus, not from the resource")
		r.Equal(&descriptorv2.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: chartDigest}, out.Output.Resource.Digest)
	})

	t.Run("content already stored is not uploaded again", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeNexus(t, charts, "", false)
		srv.store("mychart-0.1.0", chartDigest)
		res := source(chartDigest)
		out, err := transform(t, srv.URL, res)
		r.NoError(err)
		r.NotContains(srv.recorded(), "PUT "+putPath)
		r.Equal("mychart:0.1.0", access(r, out).HelmChart)
		r.Equal(res.Digest, out.Output.Resource.Digest)
	})

	t.Run("a redeploy rejection of the content already stored succeeds", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeNexus(t, charts, "", true)
		srv.store("mychart-0.1.0", chartDigest)
		out, err := transform(t, srv.URL, source(""))
		r.NoError(err)
		got := srv.recorded()
		r.Equal("PUT "+putPath, got[0])
		r.Equal("GET "+searchPath, got[1])
		r.Equal("mychart:0.1.0", access(r, out).HelmChart)
	})

	t.Run("a redeploy rejection of different content under the same chart version fails", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeNexus(t, charts, "", true)
		srv.store("mychart-0.1.0", strings.Repeat("ab", 32))
		_, err := transform(t, srv.URL, source(""))
		r.ErrorContains(err, "returned status 409")
	})

	t.Run("context path is kept", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeNexus(t, charts, "/nexus", false)
		out, err := transform(t, srv.URL+"/nexus", source(""))
		r.NoError(err)
		r.Equal("PUT /nexus"+putPath, srv.recorded()[0])
		r.Equal(srv.URL+"/nexus/repository/helm-hosted", access(r, out).HelmRepository)
	})

	t.Run("source digest mismatch fails after the upload and deletes nothing", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeNexus(t, charts, "", false)
		_, err := transform(t, srv.URL, source("0000"))
		r.EqualError(err, "digest mismatch: expected 0000, got "+chartDigest)
		for _, req := range srv.recorded() {
			r.NotContains(req, http.MethodDelete)
		}
	})
}
