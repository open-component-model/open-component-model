package artifactory

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
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/uploadtest"
	wgetaccess "ocm.software/open-component-model/bindings/go/wget/spec/access"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
	wgetcredsv1 "ocm.software/open-component-model/bindings/go/wget/spec/credentials/v1"
	wgetidentityv1 "ocm.software/open-component-model/bindings/go/wget/spec/identity/v1"
)

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
	// npm maps the sha256 of content Artifactory recognizes as an npm package to its name and
	// version.
	npm map[string][2]string

	// packageType and rclass control the GET /artifactory/api/repositories/<repo> response.
	packageType string
	rclass      string
	// detectionStatus overrides the response status of the detection endpoint (0 means 200).
	detectionStatus int
	// detectionBody, if set, is written verbatim as the 200 detection response.
	detectionBody string
	// storedPath maps a deploy path to the path the file is stored under, like Artifactory
	// storing a Maven -SNAPSHOT file under its unique version. nil stores files as requested.
	storedPath func(path string) string

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
		if f.detectionBody != "" {
			_, _ = io.WriteString(w, f.detectionBody)
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
		if pkg, ok := f.npm[f.paths[stored]]; ok {
			all["npm.name"], all["npm.version"] = []string{pkg[0]}, []string{pkg[1]}
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
		if f.storedPath != nil {
			stored = f.storedPath(stored)
		}
		created := func() {
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"repo": "helm-local", "path": "/" + stored})
		}
		if req.deploy {
			// Deploy by checksum succeeds only for content Artifactory already stores.
			if !f.contents[req.checksum] {
				http.NotFound(w, r)
				return
			}
			f.store(stored, req.checksum, params)
			created()
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
		created()
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

func TestTransform_Helm(t *testing.T) {
	chartTGZ, err := os.ReadFile("../../../../helm/testdata/mychart-0.1.0.tgz")
	require.NoError(t, err)
	sum := sha256.Sum256(chartTGZ)
	chartDigest := hex.EncodeToString(sum[:])
	charts := map[string][2]string{chartDigest: {"mychart", "0.1.0"}}

	scheme := runtime.NewScheme()
	scheme.MustRegisterWithAlias(&Transformation{}, VersionedType)
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
	step := func(url string, res *descriptorv2.Resource) *Transformation {
		return &Transformation{
			Type: VersionedType,
			ID:   "upload",
			Spec: &repositoryupload.Spec{
				Resource:         res,
				ComponentVersion: &repositoryupload.ComponentVersion{Component: "ocm.software/test", Version: "1.0.0"},
				URL:              url,
				Repository:       "helm-local",
			},
		}
	}
	transformerFor := func(content []byte, creds credentials.Resolver) *Transformer {
		repo := &uploadtest.ResourceRepo{Content: content}
		return &Transformer{repositoryupload.Uploader{
			Scheme:             scheme,
			Charts:             &chartarchive.Source{ResourceRepository: repo},
			ResourceRepository: repo,
			CredentialProvider: creds,
			PollInterval:       time.Millisecond,
		}}
	}
	transformer := func(creds credentials.Resolver) *Transformer { return transformerFor(chartTGZ, creds) }
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
		r.NoError(helmaccess.Scheme.Convert(out.(*Transformation).Output.Resource.Access, &access))
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

		res := out.(*Transformation).Output.Resource
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
		r.Len(got, 4+repositoryupload.PollAttempts, "detection, location check, upload, properties polled, then delete")
		r.Equal("DELETE "+putPath, got[len(got)-1])
		r.False(srv.stored(chartPath))
	})

	credTests := []struct {
		name      string
		creds     uploadtest.CredentialsByType
		wantBasic []string
		wantAuth  string
		wantErr   string
	}{
		{name: "HelmChartRepository HelmHTTPCredentials use basic auth", creds: uploadtest.CredentialsByType{helmType: helmCreds}, wantBasic: []string{"helm-user", "helm-pass"}},
		{name: "Wget WgetCredentials use a bearer token", creds: uploadtest.CredentialsByType{wgetType: wgetCreds}, wantAuth: "Bearer wget-token"},
		{name: "HelmChartRepository credentials win over Wget", creds: uploadtest.CredentialsByType{helmType: helmCreds, wgetType: wgetCreds}, wantBasic: []string{"helm-user", "helm-pass"}},
		{name: "HelmHTTPCredentials client certificates are rejected", creds: uploadtest.CredentialsByType{helmType: &helmcredsv1.HelmHTTPCredentials{
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
		r.Equal(res.Digest, out.(*Transformation).Output.Resource.Digest)

		// The upload location now holds the chart, so a second transfer writes nothing.
		out, err = transformer(nil).Transform(t.Context(), step(srv.URL, res))
		r.NoError(err)
		got = srv.recorded()[5:]
		r.Equal([]string{"GET " + detectionPath, "GET " + storagePath, "GET " + chartProps}, methods(got))
		r.Equal("mychart:0.1.0", helmChart(r, out))
		r.Equal(res.Digest, out.(*Transformation).Output.Resource.Digest)
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
		tr := transformer(uploadtest.CredentialsByType{})
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

func TestTransform_File(t *testing.T) {
	const content = "hello"
	contentSum := sha256.Sum256([]byte(content))
	contentDigest := hex.EncodeToString(contentSum[:])

	scheme := runtime.NewScheme()
	scheme.MustRegisterWithAlias(&Transformation{}, VersionedType)
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
	step := func(url string, res *descriptorv2.Resource) *Transformation {
		return &Transformation{
			Type: VersionedType,
			ID:   "upload",
			Spec: &repositoryupload.Spec{
				Resource:         res,
				ComponentVersion: &repositoryupload.ComponentVersion{Component: "ocm.software/test", Version: "1.0.0"},
				URL:              url,
				Repository:       "helm-local",
			},
		}
	}
	transformer := func() *Transformer {
		repo := &uploadtest.ResourceRepo{Content: []byte(content), MediaType: "text/plain"}
		return &Transformer{repositoryupload.Uploader{
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

	for _, packageType := range []string{"generic", "maven"} {
		t.Run(packageType+" repository stores the content and publishes a Wget access", func(t *testing.T) {
			r := require.New(t)
			srv := newFakeArtifactory(t, nil)
			srv.packageType = packageType
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

			res := out.(*Transformation).Output.Resource
			r.Equal("renamed", res.Name)
			var access wgetaccessv1.Wget
			r.NoError(wgetaccess.Scheme.Convert(res.Access, &access))
			r.Equal(srv.URL+"/artifactory/helm-local/"+genericPath, access.URL)
			r.Equal("application/octet-stream", access.MediaType)
			r.Equal(&descriptorv2.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: contentDigest}, res.Digest)
		})
	}

	t.Run("access points at the file Artifactory stored", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, nil)
		srv.packageType = "maven"
		srv.storedPath = func(path string) string {
			return strings.Replace(path, "renamed-1.0.0-SNAPSHOT", "renamed-1.0.0-20260928.182907-1", 1)
		}
		res := source()
		res.Version = "1.0.0-SNAPSHOT"
		res.Digest = &descriptorv2.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: contentDigest}
		want := srv.URL + "/artifactory/helm-local/ocm.software/test/1.0.0/renamed-1.0.0-20260928.182907-1"

		for _, run := range []string{"upload", "deploy by checksum"} {
			out, err := transformer().Transform(t.Context(), step(srv.URL, res))
			r.NoError(err, run)
			var access wgetaccessv1.Wget
			r.NoError(wgetaccess.Scheme.Convert(out.(*Transformation).Output.Resource.Access, &access))
			r.Equal(want, access.URL, run)
		}
		got := srv.recorded()
		last := got[len(got)-1]
		r.True(last.deploy, "the second transfer reuses the stored content")
	})

	t.Run("npm repository publishes a package Artifactory recognizes", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, nil)
		srv.packageType = "npm"
		srv.npm = map[string][2]string{contentDigest: {"renamed", "9.9.9"}}
		out, err := transformer().Transform(t.Context(), step(srv.URL, source()))
		r.NoError(err)

		got := methods(srv.recorded())
		r.Equal("PUT "+putPath+".tgz", got[len(got)-2], "the default file name ends in .tgz so Artifactory indexes it")
		r.Equal("GET "+storagePath+".tgz?properties=npm.name,npm.version", got[len(got)-1])
		var access wgetaccessv1.Wget
		r.NoError(wgetaccess.Scheme.Convert(out.(*Transformation).Output.Resource.Access, &access))
		r.Equal(srv.URL+"/artifactory/helm-local/"+genericPath+".tgz", access.URL)
	})

	t.Run("npm repository removes content that is not a package", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, nil)
		srv.packageType = "npm"
		tr := transformer()
		tr.PollInterval = time.Millisecond
		_, err := tr.Transform(t.Context(), step(srv.URL, source()))
		r.ErrorContains(err, "is not an npm package")
		r.False(srv.stored(genericPath+".tgz"), "the stored file is removed again")
	})

	t.Run("npm repository needs a .tgz path", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, nil)
		srv.packageType = "npm"
		s := step(srv.URL, source())
		s.Spec.Path = "packages/renamed"
		_, err := transformer().Transform(t.Context(), s)
		r.ErrorContains(err, `path "packages/renamed" must end in .tgz`)
		for _, req := range srv.recorded() {
			r.NotEqual(http.MethodPut, req.method)
		}
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

	withDigest := func(value string) *descriptorv2.Resource {
		res := source()
		res.Digest = &descriptorv2.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: value}
		return res
	}
	bodyPUTs := func(reqs []artifactoryRequest) int {
		n := 0
		for _, req := range reqs {
			if req.method == http.MethodPut && !req.deploy {
				n++
			}
		}
		return n
	}

	t.Run("generic reuses content artifactory already stores by checksum", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, nil)
		srv.packageType = "generic"
		srv.contents[contentDigest] = true
		out, err := transformer().Transform(t.Context(), step(srv.URL, withDigest(contentDigest)))
		r.NoError(err)

		got := srv.recorded()
		r.Equal([]string{"GET " + detectionPath, "GET " + storagePath, "PUT " + putPath}, methods(got))
		r.True(got[2].deploy)
		r.Empty(got[2].body)
		r.Equal(owner, got[2].properties)
		var access wgetaccessv1.Wget
		r.NoError(wgetaccess.Scheme.Convert(out.(*Transformation).Output.Resource.Access, &access))
		r.Equal(srv.URL+"/artifactory/helm-local/"+genericPath, access.URL)
	})

	t.Run("generic never overwrites a file stored for another resource", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, nil)
		srv.packageType = "generic"
		srv.store(genericPath, strings.Repeat("ab", 32), map[string]string{"ocm.component.name": "other"})
		_, err := transformer().Transform(t.Context(), step(srv.URL, source()))
		r.ErrorContains(err, "refusing to overwrite it, configure a different path")
		r.Zero(bodyPUTs(srv.recorded()))
	})

	t.Run("generic upload with a wrong source digest is rejected", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, nil)
		srv.packageType = "generic"
		otherSum := sha256.Sum256([]byte("other"))
		_, err := transformer().Transform(t.Context(), step(srv.URL, withDigest(hex.EncodeToString(otherSum[:]))))
		r.ErrorContains(err, "returned status 409")
		r.False(srv.stored(genericPath))
	})

	t.Run("generic uses target credentials of the repository URL", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, nil)
		srv.packageType = "generic"
		tr := transformer()
		tr.CredentialProvider = uploadtest.CredentialsByType{wgetidentityv1.Type.String(): &wgetcredsv1.WgetCredentials{
			Type: wgetcredsv1.WgetCredentialsVersionedType, IdentityToken: "tok",
		}}
		_, err := tr.Transform(t.Context(), step(srv.URL, source()))
		r.NoError(err)
		got := srv.recorded()
		r.NotEmpty(got)
		for _, req := range got {
			r.Equal("Bearer tok", req.authorization, req.method+" "+req.path)
		}
	})

	t.Run("npm re-transfer reuses the stored tarball", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, nil)
		srv.packageType = "npm"
		srv.npm = map[string][2]string{contentDigest: {"renamed", "9.9.9"}}
		_, err := transformer().Transform(t.Context(), step(srv.URL, withDigest(contentDigest)))
		r.NoError(err)
		first := len(srv.recorded())

		_, err = transformer().Transform(t.Context(), step(srv.URL, withDigest(contentDigest)))
		r.NoError(err)
		second := srv.recorded()[first:]
		r.Zero(bodyPUTs(second), "the stored tarball is not uploaded again")
		got := methods(second)
		r.Equal("GET "+storagePath+".tgz?properties=npm.name,npm.version", got[len(got)-1])
	})

	t.Run("npm custom .tgz path", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, nil)
		srv.packageType = "npm"
		srv.npm = map[string][2]string{contentDigest: {"renamed", "9.9.9"}}
		s := step(srv.URL, source())
		s.Spec.Path = "packages/renamed-9.9.9.tgz"
		out, err := transformer().Transform(t.Context(), s)
		r.NoError(err)

		var puts []string
		for _, req := range srv.recorded() {
			if req.method == http.MethodPut && !req.deploy {
				puts = append(puts, req.path)
			}
		}
		r.Equal([]string{"/artifactory/helm-local/packages/renamed-9.9.9.tgz"}, puts)
		var access wgetaccessv1.Wget
		r.NoError(wgetaccess.Scheme.Convert(out.(*Transformation).Output.Resource.Access, &access))
		r.Equal(srv.URL+"/artifactory/helm-local/packages/renamed-9.9.9.tgz", access.URL)
	})
}

func TestTransform_DetectionErrors(t *testing.T) {
	scheme := runtime.NewScheme()
	scheme.MustRegisterWithAlias(&Transformation{}, VersionedType)
	scheme.MustRegisterScheme(helmaccess.Scheme)
	scheme.MustRegisterScheme(wgetaccess.Scheme)

	source := func() *descriptorv2.Resource {
		return &descriptorv2.Resource{
			ElementMeta: descriptorv2.ElementMeta{Name: "renamed", Version: "9.9.9"},
			Type:        "helmChart",
			Relation:    descriptorv2.ExternalRelation,
			Access:      &runtime.Raw{Type: runtime.NewVersionedType("Wget", "v1"), Data: []byte(`{"type":"Wget/v1","url":"https://charts.example/mychart-0.1.0.tgz"}`)},
		}
	}
	step := func(url string) *Transformation {
		return &Transformation{
			Type: VersionedType,
			ID:   "upload",
			Spec: &repositoryupload.Spec{
				Resource:         source(),
				ComponentVersion: &repositoryupload.ComponentVersion{Component: "ocm.software/test", Version: "1.0.0"},
				URL:              url,
				Repository:       "helm-local",
			},
		}
	}
	transformer := func() *Transformer {
		chartTGZ, _ := os.ReadFile("../../../../helm/testdata/mychart-0.1.0.tgz")
		repo := &uploadtest.ResourceRepo{Content: chartTGZ}
		return &Transformer{repositoryupload.Uploader{
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

	for _, packageType := range []string{"docker", "pypi"} {
		t.Run("unsupported packageType "+packageType, func(t *testing.T) {
			r := require.New(t)
			srv := newFakeArtifactory(t, nil)
			srv.packageType = packageType
			_, err := transformer().Transform(t.Context(), step(srv.URL))
			r.ErrorContains(err, "supported: helm, generic, maven, npm")
			r.False(hasPUT(srv.recorded()))
		})
	}

	t.Run("detection returns 403", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, nil)
		srv.detectionStatus = http.StatusForbidden
		_, err := transformer().Transform(t.Context(), step(srv.URL))
		r.ErrorContains(err, "returned status 403")
		r.False(hasPUT(srv.recorded()))
	})

	t.Run("federated repository is accepted", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, nil)
		srv.rclass = "federated"
		srv.packageType = "generic"
		_, err := transformer().Transform(t.Context(), step(srv.URL))
		r.NoError(err)
	})

	t.Run("package type is case-insensitive", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, nil)
		srv.packageType = "Maven"
		_, err := transformer().Transform(t.Context(), step(srv.URL))
		r.NoError(err)
		r.True(hasPUT(srv.recorded()))
	})

	t.Run("invalid repository configuration", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, nil)
		srv.detectionBody = "not json"
		_, err := transformer().Transform(t.Context(), step(srv.URL))
		r.ErrorContains(err, `failed decoding the configuration of artifactory repository "helm-local"`)
		r.False(hasPUT(srv.recorded()))
	})

	t.Run("target credential error prevents any request", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeArtifactory(t, nil)
		helmType := helmidentityv1.Type.String()
		certCreds := uploadtest.CredentialsByType{helmType: &helmcredsv1.HelmHTTPCredentials{
			Type: runtime.NewVersionedType(helmcredsv1.HelmHTTPCredentialsType, helmcredsv1.Version), CertFile: "/cert.pem", KeyFile: "/key.pem",
		}}
		chartTGZ, _ := os.ReadFile("../../../../helm/testdata/mychart-0.1.0.tgz")
		repo := &uploadtest.ResourceRepo{Content: chartTGZ}
		tr := &Transformer{repositoryupload.Uploader{
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
