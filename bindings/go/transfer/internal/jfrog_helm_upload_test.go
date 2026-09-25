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

func TestJFrogHelmUpload_Transform(t *testing.T) {
	chartTGZ, err := os.ReadFile("../../helm/testdata/mychart-0.1.0.tgz")
	require.NoError(t, err)
	sum := sha256.Sum256(chartTGZ)
	chartDigest := hex.EncodeToString(sum[:])
	charts := map[string][2]string{chartDigest: {"mychart", "0.1.0"}}

	scheme := runtime.NewScheme()
	scheme.MustRegisterWithAlias(&JFrogHelmUploadTransformation{}, JFrogHelmUploadVersionedType)
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
	step := func(url string, reindex bool, res *descriptorv2.Resource) *JFrogHelmUploadTransformation {
		return &JFrogHelmUploadTransformation{
			Type: JFrogHelmUploadVersionedType,
			ID:   "upload",
			Spec: &JFrogHelmUploadSpec{
				Resource:         res,
				ComponentVersion: &JFrogHelmUploadComponentVersion{Component: "ocm.software/test", Version: "1.0.0"},
				URL:              url,
				Repository:       "helm-local",
				Reindex:          reindex,
			},
		}
	}
	transformerFor := func(content []byte, creds credentials.Resolver) *JFrogHelmUpload {
		repo := &chartResourceRepo{chart: content}
		return &JFrogHelmUpload{
			Scheme:                  scheme,
			Charts:                  &chartarchive.Source{ResourceRepository: repo},
			ResourceRepository:      repo,
			CredentialProvider:      creds,
			chartPropertiesInterval: time.Millisecond,
		}
	}
	transformer := func(creds credentials.Resolver) *JFrogHelmUpload { return transformerFor(chartTGZ, creds) }
	methods := func(reqs []artifactoryRequest) []string {
		var out []string
		for _, req := range reqs {
			out = append(out, req.method+" "+req.path)
		}
		return out
	}
	helmChart := func(r *require.Assertions, out runtime.Typed) string {
		var access helmaccessv1.Helm
		r.NoError(helmaccess.Scheme.Convert(out.(*JFrogHelmUploadTransformation).Output.Resource.Access, &access))
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

		res := out.(*JFrogHelmUploadTransformation).Output.Resource
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
		r.Len(got, 2+chartPropertiesAttempts, "properties are polled, then the file is deleted")
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
		}}, wantErr: "HelmHTTPCredentials certFile/keyFile are not supported for JFrog uploads"},
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
		r.Equal(res.Digest, out.(*JFrogHelmUploadTransformation).Output.Resource.Digest)

		// Artifactory now stores the chart, so a second transfer does not upload it again.
		out, err = transformer(nil).Transform(t.Context(), step(srv.URL, false, res))
		r.NoError(err)
		got = srv.recorded()[3:]
		r.Equal([]string{"PUT " + putPath, "GET " + storagePath}, methods(got))
		r.True(got[0].deploy)
		r.Empty(got[0].body)
		r.Equal("mychart:0.1.0", helmChart(r, out))
		r.Equal(res.Digest, out.(*JFrogHelmUploadTransformation).Output.Resource.Digest)
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
