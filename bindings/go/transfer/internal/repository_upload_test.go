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
	"net/url"
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
	wgetaccess "ocm.software/open-component-model/bindings/go/wget/spec/access"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
	wgetcredsv1 "ocm.software/open-component-model/bindings/go/wget/spec/credentials/v1"
	wgetidentityv1 "ocm.software/open-component-model/bindings/go/wget/spec/identity/v1"
)

// chartResourceRepo serves the chart from DownloadResource and derives no source credential identity.
type chartResourceRepo struct {
	repository.ResourceRepository
	chart     []byte
	mediaType string
}

func (s *chartResourceRepo) GetResourceCredentialConsumerIdentity(context.Context, *descriptor.Resource) (runtime.Identity, error) {
	return nil, nil
}

func (s *chartResourceRepo) DownloadResource(_ context.Context, _ *descriptor.Resource, _ runtime.Typed) (blob.ReadOnlyBlob, error) {
	return inmemory.New(bytes.NewReader(s.chart), inmemory.WithSize(int64(len(s.chart))), inmemory.WithMediaType(s.mediaType)), nil
}

// credentialsByType resolves credentials by the type attribute of the consumer identity.
type credentialsByType map[string]runtime.Typed

func (c credentialsByType) Resolve(_ context.Context, id runtime.Identity) (runtime.Typed, error) {
	typ, ok := id["type"]
	if !ok {
		return nil, credentials.ErrNotFound
	}
	cred, ok := c[typ]
	if !ok {
		return nil, credentials.ErrNotFound
	}
	return cred, nil
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
	return inmemory.New(bytes.NewReader(l.chart), inmemory.WithSize(int64(len(l.chart)))), nil, nil
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
	method, path, query, contentType string
	username, password               string
	basic                            bool
	authorization                    string
	checksum                         string
	deploy                           bool
	// properties are the deploy matrix parameters of a PUT.
	properties map[string]string
	body       []byte
}

// fakeArtifactory emulates the parts of an Artifactory repository the uploader uses. Like
// Artifactory, it records chart name and version properties for deployed content it recognizes
// as a chart; here, recognition is a lookup of the content digest in charts. Matrix parameters
// of a deploy are stored as properties of the file.
type fakeArtifactory struct {
	*httptest.Server
	charts map[string][2]string // sha256 -> name, version

	// packageType and rclass control the GET /artifactory/api/repositories/<repo> response.
	packageType string
	rclass      string
	// detectionStatus overrides the response status of the detection endpoint (0 means 200).
	detectionStatus int

	mu         sync.Mutex
	requests   []artifactoryRequest
	contents   map[string]bool              // sha256 of stored content
	paths      map[string]string            // repository path -> sha256
	properties map[string]map[string]string // repository path -> properties
}

func newFakeArtifactory(t *testing.T, charts map[string][2]string) *fakeArtifactory {
	t.Helper()
	f := &fakeArtifactory{
		charts:      charts,
		packageType: "helm",
		rclass:      "local",
		contents:    map[string]bool{},
		paths:       map[string]string{},
		properties:  map[string]map[string]string{},
	}
	f.Server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.Close)
	return f
}

// splitMatrixParams splits ;key=value matrix parameters off an escaped request path and
// returns the unescaped path and parameter values (Artifactory's backslash escapes removed).
func splitMatrixParams(escapedPath string) (string, map[string]string) {
	parts := strings.Split(escapedPath, ";")
	path, _ := url.PathUnescape(parts[0])
	var params map[string]string
	for _, part := range parts[1:] {
		key, value, _ := strings.Cut(part, "=")
		value, _ = url.PathUnescape(value)
		if params == nil {
			params = map[string]string{}
		}
		params[key] = strings.NewReplacer(`\\`, `\`, `\,`, `,`, `\|`, `|`, `\=`, `=`, `\;`, `;`).Replace(value)
	}
	return path, params
}

// store records content at path with the deploy properties, replacing earlier ones.
func (f *fakeArtifactory) store(path, digest string, props map[string]string) {
	f.paths[path] = digest
	f.properties[path] = props
}

func (f *fakeArtifactory) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	path, params := splitMatrixParams(r.URL.EscapedPath())
	req := artifactoryRequest{
		method: r.Method, path: path, query: r.URL.RawQuery, contentType: r.Header.Get("Content-Type"), authorization: r.Header.Get("Authorization"),
		checksum: r.Header.Get("X-Checksum-Sha256"), deploy: r.Header.Get("X-Checksum-Deploy") == "true", properties: params, body: body,
	}
	req.username, req.password, req.basic = r.BasicAuth()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)

	const repoPrefix, storagePrefix = "/artifactory/helm-local/", "/artifactory/api/storage/helm-local/"

	// Detection: GET /artifactory/api/repositories/<repo>
	if r.Method == http.MethodGet && path == "/artifactory/api/repositories/helm-local" {
		status := f.detectionStatus
		if status == 0 {
			status = http.StatusOK
		}
		if status != http.StatusOK {
			http.Error(w, "forbidden", status)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"packageType": f.packageType, "rclass": f.rclass})
		return
	}

	switch {
	case r.Method == http.MethodGet && strings.HasPrefix(path, storagePrefix) && !r.URL.Query().Has("properties"):
		digest, ok := f.paths[strings.TrimPrefix(path, storagePrefix)]
		if !ok {
			http.Error(w, `{"errors":[{"status":404,"message":"Unable to find item"}]}`, http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"checksums": map[string]string{"sha256": digest}})
	case r.Method == http.MethodGet && strings.HasPrefix(path, storagePrefix):
		stored := strings.TrimPrefix(path, storagePrefix)
		all := map[string][]string{}
		if chart, ok := f.charts[f.paths[stored]]; ok {
			all["chart.name"], all["chart.version"] = []string{chart[0]}, []string{chart[1]}
		}
		for k, v := range f.properties[stored] {
			all[k] = []string{v}
		}
		props := map[string][]string{}
		for _, key := range strings.Split(r.URL.Query().Get("properties"), ",") {
			if v, ok := all[key]; ok {
				props[key] = v
			}
		}
		if len(props) == 0 {
			http.Error(w, `{"errors":[{"status":404,"message":"No properties could be found."}]}`, http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"properties": props})
	case r.Method == http.MethodDelete && strings.HasPrefix(path, repoPrefix):
		delete(f.paths, strings.TrimPrefix(path, repoPrefix))
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPut && strings.HasPrefix(path, repoPrefix):
		stored := strings.TrimPrefix(path, repoPrefix)
		if req.deploy {
			// Deploy by checksum succeeds only for content Artifactory already stores.
			if !f.contents[req.checksum] {
				http.NotFound(w, r)
				return
			}
			f.store(stored, req.checksum, params)
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
		f.store(stored, digest, params)
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

func TestArtifactoryUpload_Transform_Helm(t *testing.T) {
	chartTGZ, err := os.ReadFile("../../helm/testdata/mychart-0.1.0.tgz")
	require.NoError(t, err)
	sum := sha256.Sum256(chartTGZ)
	chartDigest := hex.EncodeToString(sum[:])
	charts := map[string][2]string{chartDigest: {"mychart", "0.1.0"}}

	scheme := runtime.NewScheme()
	scheme.MustRegisterWithAlias(&ArtifactoryUploadTransformation{}, ArtifactoryUploadVersionedType)
	scheme.MustRegisterScheme(helmaccess.Scheme)

	const (
		detectionPath = "/artifactory/api/repositories/helm-local"
		chartPath     = "ocm.software/test/1.0.0/renamed-9.9.9.tgz"
		putPath       = "/artifactory/helm-local/" + chartPath
		storagePath   = "/artifactory/api/storage/helm-local/" + chartPath
		chartProps    = storagePath + "?properties=chart.name,chart.version"
	)
	owner := map[string]string{
		"ocm.component.name": "ocm.software/test", "ocm.component.version": "1.0.0",
		"ocm.resource.name": "renamed", "ocm.resource.version": "9.9.9",
	}
	source := func() *descriptorv2.Resource {
		return &descriptorv2.Resource{
			ElementMeta: descriptorv2.ElementMeta{Name: "renamed", Version: "9.9.9"},
			Type:        "helmChart",
			Relation:    descriptorv2.ExternalRelation,
			Access:      &runtime.Raw{Type: runtime.NewVersionedType("Wget", "v1"), Data: []byte(`{"type":"Wget/v1","url":"https://charts.example/mychart-0.1.0.tgz"}`)},
		}
	}
	step := func(url string, res *descriptorv2.Resource) *ArtifactoryUploadTransformation {
		return &ArtifactoryUploadTransformation{
			Type: ArtifactoryUploadVersionedType,
			ID:   "upload",
			Spec: &RepositoryUploadSpec{
				Resource:         res,
				ComponentVersion: &RepositoryUploadComponentVersion{Component: "ocm.software/test", Version: "1.0.0"},
				URL:              url,
				Repository:       "helm-local",
			},
		}
	}
	transformerFor := func(content []byte, creds credentials.Resolver) *ArtifactoryUpload {
		repo := &chartResourceRepo{chart: content}
		return &ArtifactoryUpload{repositoryUploader{
			Scheme:                scheme,
			Charts:                &chartarchive.Source{ResourceRepository: repo},
			ResourceRepository:    repo,
			CredentialProvider:    creds,
			chartMetadataInterval: time.Millisecond,
		}}
	}
	transformer := func(creds credentials.Resolver) *ArtifactoryUpload { return transformerFor(chartTGZ, creds) }
	methods := func(reqs []artifactoryRequest) []string {
		var out []string
		for _, req := range reqs {
			target := req.path
			if req.query != "" {
				target += "?" + req.query
			}
			out = append(out, req.method+" "+target)
		}
		return out
	}
	helmChart := func(r *require.Assertions, out runtime.Typed) string {
		var access helmaccessv1.Helm
		r.NoError(helmaccess.Scheme.Convert(out.(*ArtifactoryUploadTransformation).Output.Resource.Access, &access))
		return access.HelmChart
	}
	helmCreds := &helmcredsv1.HelmHTTPCredentials{Type: runtime.NewVersionedType(helmcredsv1.HelmHTTPCredentialsType, helmcredsv1.Version), Username: "helm-user", Password: "helm-pass"}
	wgetCreds := &wgetcredsv1.WgetCredentials{Type: wgetcredsv1.WgetCredentialsVersionedType, IdentityToken: "wget-token"}
	helmType, wgetType := helmidentityv1.Type.String(), wgetidentityv1.Type.String()

	t.Run("stores the chart under the component version and publishes the name artifactory records", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, charts)
		out, err := transformer(nil).Transform(t.Context(), step(srv.URL, source()))
		r.NoError(err)

		got := srv.recorded()
		r.Equal([]string{"GET " + detectionPath, "GET " + storagePath, "PUT " + putPath, "GET " + chartProps}, methods(got))
		r.Equal("application/gzip", got[2].contentType)
		r.Equal(chartTGZ, got[2].body)
		r.Empty(got[2].checksum, "without a source digest there is no checksum to announce")
		r.Equal(owner, got[2].properties, "the deploy records the owning resource as properties")

		res := out.(*ArtifactoryUploadTransformation).Output.Resource
		r.Equal("renamed", res.Name)
		var access helmaccessv1.Helm
		r.NoError(helmaccess.Scheme.Convert(res.Access, &access))
		r.Equal(srv.URL+"/artifactory/api/helm/helm-local", access.HelmRepository)
		r.Equal("mychart:0.1.0", access.HelmChart, "name and version come from artifactory, not from the resource")
		r.Equal(&descriptorv2.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: chartDigest}, res.Digest)
	})

	t.Run("content artifactory does not recognize as a chart is deleted and fails", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, charts)
		notAChart := []byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xff, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
		_, err := transformerFor(notAChart, nil).Transform(t.Context(), step(srv.URL, source()))
		r.ErrorContains(err, "is not a helm chart: artifactory recorded no chart name and version")
		got := methods(srv.recorded())
		r.Equal("GET "+detectionPath, got[0])
		r.Equal("GET "+storagePath, got[1])
		r.Equal("PUT "+putPath, got[2])
		r.Equal("GET "+chartProps, got[3])
		r.Len(got, 4+chartMetadataAttempts, "detection, location check, upload, properties polled, then delete")
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
		}}, wantErr: "HelmHTTPCredentials certFile/keyFile are not supported for repository uploads"},
	}
	for _, tt := range credTests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			srv := newFakeArtifactory(t, charts)
			_, err := transformer(tt.creds).Transform(t.Context(), step(srv.URL, source()))
			if tt.wantErr != "" {
				r.ErrorContains(err, tt.wantErr)
				r.Empty(srv.recorded(), "no request may be sent")
				return
			}
			r.NoError(err)
			got := srv.recorded()
			r.Len(got, 4, "detection, file info GET, PUT and property GET")
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
		out, err := transformer(nil).Transform(t.Context(), step(srv.URL, res))
		r.NoError(err)
		got := srv.recorded()
		r.Equal([]string{"GET " + detectionPath, "GET " + storagePath, "PUT " + putPath, "PUT " + putPath, "GET " + chartProps}, methods(got))
		r.True(got[2].deploy, "deploy by checksum is tried first")
		r.Empty(got[2].body)
		r.False(got[3].deploy)
		r.Equal(chartDigest, got[3].checksum)
		r.Equal(chartTGZ, got[3].body)
		r.Equal(res.Digest, out.(*ArtifactoryUploadTransformation).Output.Resource.Digest)

		// The upload location now holds the chart, so a second transfer writes nothing.
		out, err = transformer(nil).Transform(t.Context(), step(srv.URL, res))
		r.NoError(err)
		got = srv.recorded()[5:]
		r.Equal([]string{"GET " + detectionPath, "GET " + storagePath, "GET " + chartProps}, methods(got))
		r.Equal("mychart:0.1.0", helmChart(r, out))
		r.Equal(res.Digest, out.(*ArtifactoryUploadTransformation).Output.Resource.Digest)
	})

	t.Run("source digest mismatch is rejected by artifactory", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, charts)
		res := source()
		res.Digest = &descriptorv2.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: "0000"}
		_, err := transformer(nil).Transform(t.Context(), step(srv.URL, res))
		r.ErrorContains(err, "returned status 409")
		r.Equal([]string{"GET " + detectionPath, "GET " + storagePath, "PUT " + putPath, "PUT " + putPath}, methods(srv.recorded()), "detection, location check, deploy by checksum and the rejected upload, nothing else")
		r.False(srv.stored(chartPath))
	})

	t.Run("unsupported source digest fails before uploading", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, charts)
		res := source()
		res.Digest = &descriptorv2.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "ociArtifactDigest/v1", Value: chartDigest}
		_, err := transformer(nil).Transform(t.Context(), step(srv.URL, res))
		r.ErrorContains(err, "unsupported normalisation algorithm")
		r.Equal([]string{"GET " + detectionPath}, methods(srv.recorded()), "only detection, no upload")
	})

	t.Run("extra identity gets its own path and property", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, charts)
		res := source()
		res.ExtraIdentity = runtime.Identity{"arch": "arm64", "os": "linux"}
		_, err := transformer(nil).Transform(t.Context(), step(srv.URL, res))
		r.NoError(err)
		put := srv.recorded()[2]
		r.True(strings.HasPrefix(put.path, "/artifactory/helm-local/ocm.software/test/1.0.0/renamed-9.9.9-"), put.path)
		r.NotEqual(putPath, put.path)
		r.Equal("arch=arm64,os=linux", put.properties["ocm.resource.extraIdentity"], "separators survive the matrix parameter escaping")
	})

	t.Run("custom path", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, charts)
		s := step(srv.URL, source())
		s.Spec.Path = "team a/charts/mychart.tgz"
		out, err := transformer(nil).Transform(t.Context(), s)
		r.NoError(err)
		got := srv.recorded()
		r.Equal("PUT /artifactory/helm-local/team a/charts/mychart.tgz", methods(got)[2])
		r.Equal(owner, got[2].properties)
		r.Equal("mychart:0.1.0", helmChart(r, out))
	})

	for _, path := range []string{"../other/mychart.tgz", "/abs/mychart.tgz", "a//mychart.tgz", "a/./mychart.tgz", `a\b.tgz`, "mychart.zip"} {
		t.Run("invalid custom path "+path, func(t *testing.T) {
			r := require.New(t)
			srv := newFakeArtifactory(t, charts)
			s := step(srv.URL, source())
			s.Spec.Path = path
			_, err := transformer(nil).Transform(t.Context(), s)
			r.ErrorContains(err, "path")
			r.Equal([]string{"GET " + detectionPath}, methods(srv.recorded()), "only detection, no upload")
		})
	}

	t.Run("re-transfer of the same resource replaces its earlier upload", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, charts)
		_, err := transformer(nil).Transform(t.Context(), step(srv.URL, source()))
		r.NoError(err)
		_, err = transformer(nil).Transform(t.Context(), step(srv.URL, source()))
		r.NoError(err)
		r.Equal([]string{"GET " + detectionPath, "GET " + storagePath, "GET " + storagePath + "?properties=ocm.component.name,ocm.component.version,ocm.resource.name,ocm.resource.version,ocm.resource.extraIdentity", "PUT " + putPath, "GET " + chartProps},
			methods(srv.recorded()[4:]))
	})

	t.Run("a file stored for another resource is never overwritten", func(t *testing.T) {
		const shared = "shared/mychart.tgz"
		for name, props := range map[string]map[string]string{
			"other component version": {"ocm.component.name": "ocm.software/test", "ocm.component.version": "2.0.0", "ocm.resource.name": "renamed", "ocm.resource.version": "9.9.9"},
			"other extra identity":    {"ocm.component.name": "ocm.software/test", "ocm.component.version": "1.0.0", "ocm.resource.name": "renamed", "ocm.resource.version": "9.9.9", "ocm.resource.extraIdentity": "arch=arm64"},
			"not uploaded by ocm":     nil,
		} {
			t.Run(name, func(t *testing.T) {
				r := require.New(t)
				srv := newFakeArtifactory(t, charts)
				srv.store(shared, "0123", props)
				s := step(srv.URL, source())
				s.Spec.Path = shared
				_, err := transformer(nil).Transform(t.Context(), s)
				r.ErrorContains(err, "was not uploaded for this resource")
				for _, m := range methods(srv.recorded()) {
					r.NotContains(m, "PUT", "nothing may be written")
				}
				r.Equal("0123", srv.paths[shared])
			})
		}
	})

	t.Run("resource names cannot escape the component version path", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, charts)
		res := source()
		res.Name = "../../other"
		_, err := transformer(nil).Transform(t.Context(), step(srv.URL, res))
		r.ErrorContains(err, "must not contain path separators")
		r.Equal([]string{"GET " + detectionPath}, methods(srv.recorded()), "only detection, no upload")
	})

	t.Run("local blob without repository provider", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, charts)
		s := step(srv.URL, source())
		s.Spec.ComponentVersion.Repository = &runtime.Raw{Type: runtime.NewVersionedType("OCIRepository", "v1"), Data: []byte(`{"type":"OCIRepository/v1","baseUrl":"ghcr.io/source"}`)}
		_, err := transformer(nil).Transform(t.Context(), s)
		r.ErrorContains(err, "no component version repository provider configured")
		r.Equal([]string{"GET " + detectionPath}, methods(srv.recorded()), "only detection, no upload")
	})

	t.Run("local blob is read from the source component version", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, charts)
		repo := &localChartRepo{chart: chartTGZ}
		tr := transformer(credentialsByType{})
		tr.RepoProvider = &localChartRepoProvider{repo: repo}
		res := source()
		res.Access = &runtime.Raw{Type: runtime.NewVersionedType("LocalBlob", "v1"), Data: []byte(`{"type":"LocalBlob/v1","localReference":"sha256:abc","mediaType":"application/vnd.cncf.helm.chart.content.v1.tar+gzip"}`)}
		s := step(srv.URL, res)
		s.Spec.ComponentVersion.Repository = &runtime.Raw{Type: runtime.NewVersionedType("OCIRepository", "v1"), Data: []byte(`{"type":"OCIRepository/v1","baseUrl":"ghcr.io/source"}`)}
		out, err := tr.Transform(t.Context(), s)
		r.NoError(err)
		r.Equal("ocm.software/test", repo.component)
		r.Equal("1.0.0", repo.version)
		r.Equal(runtime.Identity{"name": "renamed", "version": "9.9.9"}, repo.identity)
		got := srv.recorded()
		r.Equal("PUT "+putPath, methods(got)[2])
		r.Equal(chartTGZ, got[2].body)
		r.Equal("mychart:0.1.0", helmChart(r, out))
	})
}

func TestArtifactoryUpload_Transform_Generic(t *testing.T) {
	const content = "hello"
	contentSum := sha256.Sum256([]byte(content))
	contentDigest := hex.EncodeToString(contentSum[:])

	scheme := runtime.NewScheme()
	scheme.MustRegisterWithAlias(&ArtifactoryUploadTransformation{}, ArtifactoryUploadVersionedType)
	scheme.MustRegisterScheme(wgetaccess.Scheme)

	const (
		detectionPath = "/artifactory/api/repositories/helm-local"
		genericPath   = "ocm.software/test/1.0.0/renamed-9.9.9"
		putPath       = "/artifactory/helm-local/" + genericPath
		storagePath   = "/artifactory/api/storage/helm-local/" + genericPath
	)
	owner := map[string]string{
		"ocm.component.name": "ocm.software/test", "ocm.component.version": "1.0.0",
		"ocm.resource.name": "renamed", "ocm.resource.version": "9.9.9",
	}
	source := func() *descriptorv2.Resource {
		return &descriptorv2.Resource{
			ElementMeta: descriptorv2.ElementMeta{Name: "renamed", Version: "9.9.9"},
			Type:        "blob",
			Relation:    descriptorv2.ExternalRelation,
			Access:      &runtime.Raw{Type: runtime.NewVersionedType("Wget", "v1"), Data: []byte(`{"type":"Wget/v1","url":"https://example.com/hello"}`)},
		}
	}
	step := func(url string, res *descriptorv2.Resource) *ArtifactoryUploadTransformation {
		return &ArtifactoryUploadTransformation{
			Type: ArtifactoryUploadVersionedType,
			ID:   "upload",
			Spec: &RepositoryUploadSpec{
				Resource:         res,
				ComponentVersion: &RepositoryUploadComponentVersion{Component: "ocm.software/test", Version: "1.0.0"},
				URL:              url,
				Repository:       "helm-local",
			},
		}
	}
	transformer := func() *ArtifactoryUpload {
		repo := &chartResourceRepo{chart: []byte(content), mediaType: "text/plain"}
		return &ArtifactoryUpload{repositoryUploader{
			Scheme:             scheme,
			Charts:             &chartarchive.Source{ResourceRepository: repo},
			ResourceRepository: repo,
		}}
	}
	methods := func(reqs []artifactoryRequest) []string {
		var out []string
		for _, req := range reqs {
			target := req.path
			if req.query != "" {
				target += "?" + req.query
			}
			out = append(out, req.method+" "+target)
		}
		return out
	}

	t.Run("uploads content with owner properties and publishes Wget access", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, nil)
		srv.packageType = "generic"
		out, err := transformer().Transform(t.Context(), step(srv.URL, source()))
		r.NoError(err)

		got := srv.recorded()
		// detection, claim (storage GET, 404), reuse (deploy-by-checksum PUT, 404), content PUT
		r.Equal([]string{"GET " + detectionPath, "GET " + storagePath, "PUT " + putPath, "PUT " + putPath}, methods(got))
		r.True(got[2].deploy, "deploy by checksum is tried first")
		r.Empty(got[2].body)
		r.False(got[3].deploy)
		r.Equal([]byte(content), got[3].body)
		r.Equal(owner, got[3].properties)

		res := out.(*ArtifactoryUploadTransformation).Output.Resource
		r.Equal("renamed", res.Name)
		var access wgetaccessv1.Wget
		r.NoError(wgetaccess.Scheme.Convert(res.Access, &access))
		r.Equal(srv.URL+"/artifactory/helm-local/"+genericPath, access.URL)
		r.Equal("application/octet-stream", access.MediaType)
		r.Equal(&descriptorv2.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: contentDigest}, res.Digest)
	})

	t.Run("no chart property GET is issued for generic uploads", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, nil)
		srv.packageType = "generic"
		_, err := transformer().Transform(t.Context(), step(srv.URL, source()))
		r.NoError(err)
		for _, req := range srv.recorded() {
			r.NotContains(req.query, "chart.name", "no chart property should be requested")
		}
	})
}

func TestArtifactoryUpload_Transform_DetectionErrors(t *testing.T) {
	scheme := runtime.NewScheme()
	scheme.MustRegisterWithAlias(&ArtifactoryUploadTransformation{}, ArtifactoryUploadVersionedType)
	scheme.MustRegisterScheme(helmaccess.Scheme)

	source := func() *descriptorv2.Resource {
		return &descriptorv2.Resource{
			ElementMeta: descriptorv2.ElementMeta{Name: "renamed", Version: "9.9.9"},
			Type:        "helmChart",
			Relation:    descriptorv2.ExternalRelation,
			Access:      &runtime.Raw{Type: runtime.NewVersionedType("Wget", "v1"), Data: []byte(`{"type":"Wget/v1","url":"https://charts.example/mychart-0.1.0.tgz"}`)},
		}
	}
	step := func(url string) *ArtifactoryUploadTransformation {
		return &ArtifactoryUploadTransformation{
			Type: ArtifactoryUploadVersionedType,
			ID:   "upload",
			Spec: &RepositoryUploadSpec{
				Resource:         source(),
				ComponentVersion: &RepositoryUploadComponentVersion{Component: "ocm.software/test", Version: "1.0.0"},
				URL:              url,
				Repository:       "helm-local",
			},
		}
	}
	transformer := func() *ArtifactoryUpload {
		chartTGZ, _ := os.ReadFile("../../helm/testdata/mychart-0.1.0.tgz")
		repo := &chartResourceRepo{chart: chartTGZ}
		return &ArtifactoryUpload{repositoryUploader{
			Scheme:             scheme,
			Charts:             &chartarchive.Source{ResourceRepository: repo},
			ResourceRepository: repo,
		}}
	}
	hasPUT := func(reqs []artifactoryRequest) bool {
		for _, req := range reqs {
			if req.method == http.MethodPut {
				return true
			}
		}
		return false
	}

	t.Run("rclass remote rejects with uploads need a local repository", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, nil)
		srv.rclass = "remote"
		_, err := transformer().Transform(t.Context(), step(srv.URL))
		r.ErrorContains(err, "uploads need a local repository")
		r.False(hasPUT(srv.recorded()))
	})

	t.Run("unsupported packageType npm", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, nil)
		srv.packageType = "npm"
		_, err := transformer().Transform(t.Context(), step(srv.URL))
		r.ErrorContains(err, "supported: helm, generic")
		r.False(hasPUT(srv.recorded()))
	})

	t.Run("detection returns 403", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, nil)
		srv.detectionStatus = http.StatusForbidden
		_, err := transformer().Transform(t.Context(), step(srv.URL))
		r.ErrorContains(err, "returned status 403")
		r.False(hasPUT(srv.recorded()))
	})

	t.Run("target credential error prevents any request", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, nil)
		helmType := helmidentityv1.Type.String()
		certCreds := credentialsByType{helmType: &helmcredsv1.HelmHTTPCredentials{
			Type: runtime.NewVersionedType(helmcredsv1.HelmHTTPCredentialsType, helmcredsv1.Version), CertFile: "/cert.pem", KeyFile: "/key.pem",
		}}
		chartTGZ, _ := os.ReadFile("../../helm/testdata/mychart-0.1.0.tgz")
		repo := &chartResourceRepo{chart: chartTGZ}
		tr := &ArtifactoryUpload{repositoryUploader{
			Scheme:             scheme,
			Charts:             &chartarchive.Source{ResourceRepository: repo},
			ResourceRepository: repo,
			CredentialProvider: certCreds,
		}}
		_, err := tr.Transform(t.Context(), step(srv.URL))
		r.ErrorContains(err, "HelmHTTPCredentials certFile/keyFile are not supported")
		r.Empty(srv.recorded(), "no request may be sent when credentials fail before detection")
	})
}

// fakeNexus emulates the parts of a Nexus Repository 3 hosted repository the uploader uses.
// For helm, it stores an uploaded chart under <name>-<version> from the chart; for raw, it
// stores the uploaded file at the path. Chart recognition is a lookup of the content digest in
// charts.
type fakeNexus struct {
	*httptest.Server
	charts    map[string][2]string // sha256 -> name, version
	basePath  string
	allowOnce bool

	// format and typ control the GET /service/rest/v1/repositories/<repo> response.
	format string
	typ    string
	// detectionStatus overrides the response status (0 means 200).
	detectionStatus int

	mu       sync.Mutex
	requests []string
	stored   map[string]string // <name>-<version> or raw path -> sha256
	raw      map[string][]byte // raw path -> content
}

func newFakeNexus(t *testing.T, charts map[string][2]string, basePath string, allowOnce bool) *fakeNexus {
	t.Helper()
	f := &fakeNexus{
		charts:    charts,
		basePath:  basePath,
		allowOnce: allowOnce,
		format:    "helm",
		typ:       "hosted",
		stored:    map[string]string{},
		raw:       map[string][]byte{},
	}
	f.Server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeNexus) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)

	repoPrefix := f.basePath + "/repository/helm-hosted/"

	// Detection: GET /service/rest/v1/repositories/<repo>
	if r.Method == http.MethodGet && r.URL.Path == f.basePath+"/service/rest/v1/repositories/helm-hosted" {
		status := f.detectionStatus
		if status == 0 {
			status = http.StatusOK
		}
		if status != http.StatusOK {
			http.Error(w, "forbidden", status)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"format": f.format, "type": f.typ})
		return
	}

	// Asset search: GET /service/rest/v1/search/assets?repository=&name=
	if r.Method == http.MethodGet && r.URL.Path == f.basePath+"/service/rest/v1/search/assets" {
		q := r.URL.Query()
		repo := q.Get("repository")
		name := q.Get("name")
		type checksum struct {
			SHA256 string `json:"sha256"`
		}
		type item struct {
			Checksum checksum `json:"checksum"`
		}
		var items []item
		if repo == "helm-hosted" && name != "" {
			// Nexus indexes raw assets with a leading slash; strip it for our stored map.
			lookupName := strings.TrimPrefix(name, "/")
			if digest, ok := f.stored[lookupName]; ok {
				items = append(items, item{Checksum: checksum{SHA256: digest}})
			}
		}
		if items == nil {
			items = []item{}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
		return
	}

	switch {
	// Helm PUT
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, repoPrefix):
		sum := sha256.Sum256(body)
		digest := hex.EncodeToString(sum[:])
		// For helm, charts maps recognize the content; for raw, just store it.
		chart, isChart := f.charts[digest]
		if isChart {
			key := chart[0] + "-" + chart[1]
			if _, exists := f.stored[key]; exists && f.allowOnce {
				http.Error(w, "helm-hosted/"+key+".tgz -  cannot be updated as asset already exists and redeploy is not allowed", http.StatusConflict)
				return
			}
			f.stored[key] = digest
		} else {
			// Raw upload: store at the path relative to repo root.
			relPath := strings.TrimPrefix(r.URL.Path, repoPrefix)
			f.stored[relPath] = digest
			f.raw[relPath] = append([]byte(nil), body...)
		}
		w.WriteHeader(http.StatusOK)

	// Helm search
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

	// Raw HEAD
	case r.Method == http.MethodHead && strings.HasPrefix(r.URL.Path, repoPrefix):
		relPath := strings.TrimPrefix(r.URL.Path, repoPrefix)
		if _, ok := f.stored[relPath]; ok {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}

	default:
		http.Error(w, "unexpected request: "+r.Method+" "+r.URL.Path, http.StatusBadRequest)
	}
}

// store records content as stored under key (chart key or raw path), with its sha256.
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

func TestNexusUpload_Transform_Helm(t *testing.T) {
	chartTGZ, err := os.ReadFile("../../helm/testdata/mychart-0.1.0.tgz")
	require.NoError(t, err)
	sum := sha256.Sum256(chartTGZ)
	chartDigest := hex.EncodeToString(sum[:])
	charts := map[string][2]string{chartDigest: {"mychart", "0.1.0"}}

	scheme := runtime.NewScheme()
	scheme.MustRegisterWithAlias(&NexusUploadTransformation{}, NexusUploadVersionedType)
	scheme.MustRegisterScheme(helmaccess.Scheme)

	const (
		detectionPath = "/service/rest/v1/repositories/helm-hosted"
		putPath       = "/repository/helm-hosted/renamed-9.9.9.tgz"
		searchPath    = "/service/rest/v1/search"
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
	transform := func(t *testing.T, url string, res *descriptorv2.Resource) (*NexusUploadTransformation, error) {
		repo := &chartResourceRepo{chart: chartTGZ}
		tr := &NexusUpload{repositoryUploader{
			Scheme:                scheme,
			Charts:                &chartarchive.Source{ResourceRepository: repo},
			ResourceRepository:    repo,
			chartMetadataInterval: time.Millisecond,
		}}
		out, err := tr.Transform(t.Context(), &NexusUploadTransformation{
			Type: NexusUploadVersionedType,
			ID:   "upload",
			Spec: &RepositoryUploadSpec{
				Resource:         res,
				ComponentVersion: &RepositoryUploadComponentVersion{Component: "ocm.software/test", Version: "1.0.0"},
				URL:              url,
				Repository:       "helm-hosted",
			},
		})
		if err != nil {
			return nil, err
		}
		return out.(*NexusUploadTransformation), nil
	}
	access := func(r *require.Assertions, out *NexusUploadTransformation) helmaccessv1.Helm {
		var access helmaccessv1.Helm
		r.NoError(helmaccess.Scheme.Convert(out.Output.Resource.Access, &access))
		return access
	}

	t.Run("uploads to the repository root and publishes the chart nexus stores", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeNexus(t, charts, "", false)
		out, err := transform(t, srv.URL, source(""))
		r.NoError(err)
		r.Equal([]string{"GET " + detectionPath, "PUT " + putPath, "GET " + searchPath}, srv.recorded())
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
		r.Equal("GET "+detectionPath, got[0])
		r.Equal("PUT "+putPath, got[1])
		r.Equal("GET "+searchPath, got[2])
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
		r.Equal("GET /nexus"+detectionPath, srv.recorded()[0])
		r.Equal("PUT /nexus"+putPath, srv.recorded()[1])
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

	t.Run("helm with path is rejected", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeNexus(t, charts, "", false)
		repo := &chartResourceRepo{chart: chartTGZ}
		tr := &NexusUpload{repositoryUploader{
			Scheme:             scheme,
			Charts:             &chartarchive.Source{ResourceRepository: repo},
			ResourceRepository: repo,
		}}
		_, err := tr.Transform(t.Context(), &NexusUploadTransformation{
			Type: NexusUploadVersionedType,
			ID:   "upload",
			Spec: &RepositoryUploadSpec{
				Resource:         source(""),
				ComponentVersion: &RepositoryUploadComponentVersion{Component: "ocm.software/test", Version: "1.0.0"},
				URL:              srv.URL,
				Repository:       "helm-hosted",
				Path:             "custom/chart.tgz",
			},
		})
		r.ErrorContains(err, "path is not supported for nexus helm repositories")
	})
}

func TestNexusUpload_Transform_Raw(t *testing.T) {
	const content = "hello"
	contentSum := sha256.Sum256([]byte(content))
	contentDigest := hex.EncodeToString(contentSum[:])

	scheme := runtime.NewScheme()
	scheme.MustRegisterWithAlias(&NexusUploadTransformation{}, NexusUploadVersionedType)
	scheme.MustRegisterScheme(wgetaccess.Scheme)

	const (
		detectionPath = "/service/rest/v1/repositories/helm-hosted"
		rawPath       = "ocm.software/test/1.0.0/renamed-9.9.9"
		putPath       = "/repository/helm-hosted/" + rawPath
		searchAssets  = "/service/rest/v1/search/assets"
	)
	source := func(digest string) *descriptorv2.Resource {
		res := &descriptorv2.Resource{
			ElementMeta: descriptorv2.ElementMeta{Name: "renamed", Version: "9.9.9"},
			Type:        "blob",
			Relation:    descriptorv2.ExternalRelation,
			Access:      &runtime.Raw{Type: runtime.NewVersionedType("Wget", "v1"), Data: []byte(`{"type":"Wget/v1","url":"https://example.com/hello"}`)},
		}
		if digest != "" {
			res.Digest = &descriptorv2.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: digest}
		}
		return res
	}
	transformer := func(creds credentials.Resolver) *NexusUpload {
		repo := &chartResourceRepo{chart: []byte(content), mediaType: "text/plain"}
		return &NexusUpload{repositoryUploader{
			Scheme:             scheme,
			Charts:             &chartarchive.Source{ResourceRepository: repo},
			ResourceRepository: repo,
			CredentialProvider: creds,
		}}
	}
	step := func(url string, res *descriptorv2.Resource) *NexusUploadTransformation {
		return &NexusUploadTransformation{
			Type: NexusUploadVersionedType,
			ID:   "upload",
			Spec: &RepositoryUploadSpec{
				Resource:         res,
				ComponentVersion: &RepositoryUploadComponentVersion{Component: "ocm.software/test", Version: "1.0.0"},
				URL:              url,
				Repository:       "helm-hosted",
			},
		}
	}

	t.Run("first upload PUTs the content and publishes Wget access", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeNexus(t, nil, "", false)
		srv.format = "raw"
		out, err := transformer(nil).Transform(t.Context(), step(srv.URL, source("")))
		r.NoError(err)

		got := srv.recorded()
		r.Equal([]string{"GET " + detectionPath, "HEAD " + putPath, "PUT " + putPath}, got)

		res := out.(*NexusUploadTransformation).Output.Resource
		var access wgetaccessv1.Wget
		r.NoError(wgetaccess.Scheme.Convert(res.Access, &access))
		r.Equal(srv.URL+"/repository/helm-hosted/"+rawPath, access.URL)
		r.Equal("application/octet-stream", access.MediaType)
		r.Equal(&descriptorv2.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: contentDigest}, res.Digest)
	})

	t.Run("second upload with same digest reuses via HEAD and search", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeNexus(t, nil, "", false)
		srv.format = "raw"
		// First upload.
		_, err := transformer(nil).Transform(t.Context(), step(srv.URL, source("")))
		r.NoError(err)

		// Second upload with the known digest.
		out, err := transformer(nil).Transform(t.Context(), step(srv.URL, source(contentDigest)))
		r.NoError(err)

		got := srv.recorded()
		// After the first 3 requests (detection + HEAD + PUT), the second run should be
		// detection + HEAD + search (no PUT).
		second := got[3:]
		r.Equal("GET "+detectionPath, second[0])
		r.Equal("HEAD "+putPath, second[1])
		r.Equal("GET "+searchAssets, second[2])
		r.Len(second, 3, "no PUT on reuse")

		res := out.(*NexusUploadTransformation).Output.Resource
		var access wgetaccessv1.Wget
		r.NoError(wgetaccess.Scheme.Convert(res.Access, &access))
		r.Equal(srv.URL+"/repository/helm-hosted/"+rawPath, access.URL)
	})

	t.Run("different file at the path is never overwritten", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeNexus(t, nil, "", false)
		srv.format = "raw"
		// Pre-store a different file at the same path.
		srv.store(rawPath, strings.Repeat("ab", 32))
		_, err := transformer(nil).Transform(t.Context(), step(srv.URL, source(contentDigest)))
		r.ErrorContains(err, "never overwrites files in raw repositories")
		for _, req := range srv.recorded() {
			r.NotContains(req, "PUT", "nothing may be written")
		}
	})
}

func TestNexusUpload_Transform_DetectionErrors(t *testing.T) {
	scheme := runtime.NewScheme()
	scheme.MustRegisterWithAlias(&NexusUploadTransformation{}, NexusUploadVersionedType)

	source := func() *descriptorv2.Resource {
		return &descriptorv2.Resource{
			ElementMeta: descriptorv2.ElementMeta{Name: "renamed", Version: "9.9.9"},
			Type:        "blob",
			Relation:    descriptorv2.ExternalRelation,
			Access:      &runtime.Raw{Type: runtime.NewVersionedType("Wget", "v1"), Data: []byte(`{"type":"Wget/v1","url":"https://example.com/hello"}`)},
		}
	}
	step := func(url string) *NexusUploadTransformation {
		return &NexusUploadTransformation{
			Type: NexusUploadVersionedType,
			ID:   "upload",
			Spec: &RepositoryUploadSpec{
				Resource:         source(),
				ComponentVersion: &RepositoryUploadComponentVersion{Component: "ocm.software/test", Version: "1.0.0"},
				URL:              url,
				Repository:       "helm-hosted",
			},
		}
	}
	transformer := func() *NexusUpload {
		repo := &chartResourceRepo{chart: []byte("hello"), mediaType: "text/plain"}
		return &NexusUpload{repositoryUploader{
			Scheme:             scheme,
			Charts:             &chartarchive.Source{ResourceRepository: repo},
			ResourceRepository: repo,
		}}
	}

	t.Run("type proxy rejects with uploads need a hosted repository", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeNexus(t, nil, "", false)
		srv.typ = "proxy"
		_, err := transformer().Transform(t.Context(), step(srv.URL))
		r.ErrorContains(err, "uploads need a hosted repository")
	})

	t.Run("unsupported format npm", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeNexus(t, nil, "", false)
		srv.format = "npm"
		_, err := transformer().Transform(t.Context(), step(srv.URL))
		r.ErrorContains(err, "supported: helm, raw")
	})

	t.Run("detection returns 403", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeNexus(t, nil, "", false)
		srv.detectionStatus = http.StatusForbidden
		_, err := transformer().Transform(t.Context(), step(srv.URL))
		r.ErrorContains(err, "returned status 403")
	})

	t.Run("target credential error prevents any request", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeNexus(t, nil, "", false)
		helmType := helmidentityv1.Type.String()
		certCreds := credentialsByType{helmType: &helmcredsv1.HelmHTTPCredentials{
			Type: runtime.NewVersionedType(helmcredsv1.HelmHTTPCredentialsType, helmcredsv1.Version), CertFile: "/cert.pem", KeyFile: "/key.pem",
		}}
		repo := &chartResourceRepo{chart: []byte("hello"), mediaType: "text/plain"}
		tr := &NexusUpload{repositoryUploader{
			Scheme:             scheme,
			Charts:             &chartarchive.Source{ResourceRepository: repo},
			ResourceRepository: repo,
			CredentialProvider: certCreds,
		}}
		_, err := tr.Transform(t.Context(), step(srv.URL))
		r.ErrorContains(err, "HelmHTTPCredentials certFile/keyFile are not supported")
		r.Empty(srv.recorded(), "no request may be sent when credentials fail before detection")
	})
}
