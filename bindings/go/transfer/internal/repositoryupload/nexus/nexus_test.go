package nexus

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/credentials"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	helmaccess "ocm.software/open-component-model/bindings/go/helm/spec/access"
	helmaccessv1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	helmidentityv1 "ocm.software/open-component-model/bindings/go/helm/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/uploadtest"
	uploadv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/transformation/spec/v1alpha1"
	wgetaccess "ocm.software/open-component-model/bindings/go/wget/spec/access"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
	wgetcredsv1 "ocm.software/open-component-model/bindings/go/wget/spec/credentials/v1"
	wgetidentityv1 "ocm.software/open-component-model/bindings/go/wget/spec/identity/v1"
)

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
	// detectionBody, if set, is written verbatim as the 200 detection response.
	detectionBody string
	// headStatus, if set, is the response status of every raw HEAD.
	headStatus int
	// componentStatus and componentBody, if the status is set, answer every components API upload.
	componentStatus int
	componentBody   string
	// searchLag is the number of asset searches that find nothing yet, like Nexus indexing
	// stored assets for search shortly after the upload.
	searchLag int

	mu       sync.Mutex
	requests []string
	// auth is the Authorization header of every request.
	auth   []string
	stored map[string]string // <name>-<version> or raw path -> sha256
	raw    map[string][]byte // raw path -> content
	// mavenForms are the form values of the components API uploads.
	mavenForms []map[string][]string
	// npm maps the sha256 of content Nexus recognizes as an npm package to its name and version.
	npm map[string][2]string
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
	f.auth = append(f.auth, r.Header.Get("Authorization"))

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
		if f.detectionBody != "" {
			_, _ = io.WriteString(w, f.detectionBody)
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
			Path     string   `json:"path"`
			Checksum checksum `json:"checksum"`
		}
		if artifactID := q.Get("maven.artifactId"); artifactID != "" {
			// Maven assets are found by coordinates; the search also returns sibling files.
			file := artifactID + "-" + q.Get("maven.baseVersion")
			if classifier := q.Get("maven.classifier"); classifier != "" {
				file += "-" + classifier
			}
			name = "/" + strings.ReplaceAll(q.Get("maven.groupId"), ".", "/") + "/" + artifactID + "/" + q.Get("maven.baseVersion") + "/" + file + "." + q.Get("maven.extension")
		}
		var items []item
		if f.searchLag > 0 {
			f.searchLag--
		} else if sha := q.Get("sha256"); repo == "helm-hosted" && sha != "" {
			for path, digest := range f.stored {
				if digest == sha {
					items = append(items, item{Path: "/" + path, Checksum: checksum{SHA256: digest}})
				}
			}
		} else if repo == "helm-hosted" && name != "" {
			// Nexus reports asset paths with a leading slash; strip it for our stored map.
			lookupName := strings.TrimPrefix(name, "/")
			if digest, ok := f.stored[lookupName]; ok {
				items = append(items, item{Path: name, Checksum: checksum{SHA256: digest}}, item{Path: name + ".sha1", Checksum: checksum{SHA256: "other"}})
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

	// Components API: a single maven2 asset, stored at its Maven layout path.
	case r.Method == http.MethodPost && r.URL.Path == f.basePath+"/service/rest/v1/components":
		if f.componentStatus != 0 {
			http.Error(w, f.componentBody, f.componentStatus)
			return
		}
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		form, err := multipart.NewReader(bytes.NewReader(body), params["boundary"]).ReadForm(1 << 20)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		field := func(name string) string {
			if v := form.Value[name]; len(v) == 1 {
				return v[0]
			}
			return ""
		}
		if files := form.File["npm.asset"]; len(files) == 1 {
			file, _ := files[0].Open()
			content, _ := io.ReadAll(file)
			sum := sha256.Sum256(content)
			digest := hex.EncodeToString(sum[:])
			pkg, ok := f.npm[digest]
			if !ok {
				http.Error(w, `[{"id":"*","message":"Name and version are mandatory fields"}]`, http.StatusBadRequest)
				return
			}
			base := pkg[0][strings.LastIndex(pkg[0], "/")+1:]
			key := pkg[0] + "/-/" + base + "-" + pkg[1] + ".tgz"
			if _, exists := f.stored[key]; exists && f.allowOnce {
				http.Error(w, "helm-hosted/"+key+" -  cannot be updated as asset already exists and redeploy is not allowed", http.StatusConflict)
				return
			}
			f.stored[key] = digest
			w.WriteHeader(http.StatusNoContent)
			return
		}
		f.mavenForms = append(f.mavenForms, form.Value)
		file, err := form.File["maven2.asset1"][0].Open()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		content, _ := io.ReadAll(file)
		name := field("maven2.artifactId") + "-" + field("maven2.version")
		if classifier := field("maven2.asset1.classifier"); classifier != "" {
			name += "-" + classifier
		}
		relPath := strings.ReplaceAll(field("maven2.groupId"), ".", "/") + "/" + field("maven2.artifactId") + "/" + field("maven2.version") + "/" + name + "." + field("maven2.asset1.extension")
		sum := sha256.Sum256(content)
		f.stored[relPath] = hex.EncodeToString(sum[:])
		f.raw[relPath] = content
		w.WriteHeader(http.StatusNoContent)

	// Raw HEAD
	case r.Method == http.MethodHead && strings.HasPrefix(r.URL.Path, repoPrefix):
		relPath := strings.TrimPrefix(r.URL.Path, repoPrefix)
		_, ok := f.stored[relPath]
		switch {
		case f.headStatus != 0:
			w.WriteHeader(f.headStatus)
		case ok:
			w.WriteHeader(http.StatusOK)
		default:
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

func TestTransform_Helm(t *testing.T) {
	chartTGZ, err := os.ReadFile("../../../../helm/testdata/mychart-0.1.0.tgz")
	require.NoError(t, err)
	sum := sha256.Sum256(chartTGZ)
	chartDigest := hex.EncodeToString(sum[:])
	charts := map[string][2]string{chartDigest: {"mychart", "0.1.0"}}

	scheme := runtime.NewScheme()
	scheme.MustRegisterScheme(uploadv1alpha1.Scheme)
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
	transform := func(t *testing.T, url string, res *descriptorv2.Resource) (*uploadv1alpha1.NexusUpload, error) {
		repo := &uploadtest.ResourceRepo{Content: chartTGZ}
		tr := &Transformer{repositoryupload.Uploader{
			Scheme:             scheme,
			ResourceRepository: repo,
			PollInterval:       time.Millisecond,
		}}
		out, err := tr.Transform(t.Context(), &uploadv1alpha1.NexusUpload{
			Type: uploadv1alpha1.NexusUploadV1alpha1,
			ID:   "upload",
			Spec: &uploadv1alpha1.RepositoryUploadSpec{
				Resource:         res,
				ComponentVersion: &uploadv1alpha1.RepositoryUploadComponentVersion{Component: "ocm.software/test", Version: "1.0.0"},
				URL:              url,
				Repository:       "helm-hosted",
			},
		})
		if err != nil {
			return nil, err
		}
		return out.(*uploadv1alpha1.NexusUpload), nil
	}
	access := func(r *require.Assertions, out *uploadv1alpha1.NexusUpload) helmaccessv1.Helm {
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
		repo := &uploadtest.ResourceRepo{Content: chartTGZ}
		tr := &Transformer{repositoryupload.Uploader{
			Scheme:             scheme,
			ResourceRepository: repo,
		}}
		_, err := tr.Transform(t.Context(), &uploadv1alpha1.NexusUpload{
			Type: uploadv1alpha1.NexusUploadV1alpha1,
			ID:   "upload",
			Spec: &uploadv1alpha1.RepositoryUploadSpec{
				Resource:         source(""),
				ComponentVersion: &uploadv1alpha1.RepositoryUploadComponentVersion{Component: "ocm.software/test", Version: "1.0.0"},
				URL:              srv.URL,
				Repository:       "helm-hosted",
				Path:             "custom/chart.tgz",
			},
		})
		r.ErrorContains(err, "path is not supported for nexus helm repositories")
	})
}

func TestTransform_Raw(t *testing.T) {
	const content = "hello"
	contentSum := sha256.Sum256([]byte(content))
	contentDigest := hex.EncodeToString(contentSum[:])

	scheme := runtime.NewScheme()
	scheme.MustRegisterScheme(uploadv1alpha1.Scheme)
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
	transformer := func(creds credentials.Resolver) *Transformer {
		repo := &uploadtest.ResourceRepo{Content: []byte(content), MediaType: "text/plain"}
		return &Transformer{repositoryupload.Uploader{
			Scheme:             scheme,
			ResourceRepository: repo,
			CredentialProvider: creds,
			PollInterval:       time.Millisecond,
		}}
	}
	step := func(url string, res *descriptorv2.Resource) *uploadv1alpha1.NexusUpload {
		return &uploadv1alpha1.NexusUpload{
			Type: uploadv1alpha1.NexusUploadV1alpha1,
			ID:   "upload",
			Spec: &uploadv1alpha1.RepositoryUploadSpec{
				Resource:         res,
				ComponentVersion: &uploadv1alpha1.RepositoryUploadComponentVersion{Component: "ocm.software/test", Version: "1.0.0"},
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

		res := out.(*uploadv1alpha1.NexusUpload).Output.Resource
		var access wgetaccessv1.Wget
		r.NoError(wgetaccess.Scheme.Convert(res.Access, &access))
		r.Equal(srv.URL+"/repository/helm-hosted/"+rawPath, access.URL)
		r.Equal("text/plain", access.MediaType, "the media type the source blob reports")
		r.Equal(&descriptorv2.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: contentDigest}, res.Digest)
	})

	t.Run("second upload with same digest reuses once the search finds the stored file", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeNexus(t, nil, "", false)
		srv.format = "raw"
		// First upload.
		_, err := transformer(nil).Transform(t.Context(), step(srv.URL, source("")))
		r.NoError(err)

		// Second upload with the known digest, before Nexus indexed the first one for search.
		srv.searchLag = 2
		out, err := transformer(nil).Transform(t.Context(), step(srv.URL, source(contentDigest)))
		r.NoError(err)

		got := srv.recorded()
		// After the first 3 requests (detection + HEAD + PUT), the second run is detection,
		// HEAD and the search polled until it finds the file; nothing is uploaded.
		r.Equal([]string{"GET " + detectionPath, "HEAD " + putPath, "GET " + searchAssets, "GET " + searchAssets, "GET " + searchAssets}, got[3:])

		res := out.(*uploadv1alpha1.NexusUpload).Output.Resource
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

	t.Run("custom path", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeNexus(t, nil, "", false)
		srv.format = "raw"
		s := step(srv.URL, source(""))
		s.Spec.Path = "files/notes.txt"
		out, err := transformer(nil).Transform(t.Context(), s)
		r.NoError(err)
		r.Equal([]string{"GET " + detectionPath, "HEAD /repository/helm-hosted/files/notes.txt", "PUT /repository/helm-hosted/files/notes.txt"}, srv.recorded())
		var access wgetaccessv1.Wget
		r.NoError(wgetaccess.Scheme.Convert(out.(*uploadv1alpha1.NexusUpload).Output.Resource.Access, &access))
		r.Equal(srv.URL+"/repository/helm-hosted/files/notes.txt", access.URL)
	})

	t.Run("unexpected HEAD status fails before uploading", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeNexus(t, nil, "", false)
		srv.format = "raw"
		// Not a 5xx, which the HTTP client retries with backoff.
		srv.headStatus = http.StatusForbidden
		_, err := transformer(nil).Transform(t.Context(), step(srv.URL, source(contentDigest)))
		r.ErrorContains(err, "returned status 403")
		r.NotContains(srv.recorded(), "PUT "+putPath)
	})

	t.Run("wrong source digest fails after the upload", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeNexus(t, nil, "", false)
		srv.format = "raw"
		otherSum := sha256.Sum256([]byte("other"))
		_, err := transformer(nil).Transform(t.Context(), step(srv.URL, source(hex.EncodeToString(otherSum[:]))))
		r.ErrorContains(err, "digest mismatch:")
		r.ErrorContains(err, "nexus keeps the uploaded file at")
	})

	t.Run("sends target credentials on every request", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeNexus(t, nil, "", false)
		srv.format = "raw"
		creds := uploadtest.CredentialsByType{wgetidentityv1.Type.String(): &wgetcredsv1.WgetCredentials{
			Type: wgetcredsv1.WgetCredentialsVersionedType, Username: "u", Password: "p",
		}}
		_, err := transformer(creds).Transform(t.Context(), step(srv.URL, source("")))
		r.NoError(err)
		r.NotEmpty(srv.auth)
		for _, auth := range srv.auth {
			r.Equal("Basic dTpw", auth)
		}
	})
}

func TestTransform_DetectionErrors(t *testing.T) {
	scheme := runtime.NewScheme()
	scheme.MustRegisterScheme(uploadv1alpha1.Scheme)

	source := func() *descriptorv2.Resource {
		return &descriptorv2.Resource{
			ElementMeta: descriptorv2.ElementMeta{Name: "renamed", Version: "9.9.9"},
			Type:        "blob",
			Relation:    descriptorv2.ExternalRelation,
			Access:      &runtime.Raw{Type: runtime.NewVersionedType("Wget", "v1"), Data: []byte(`{"type":"Wget/v1","url":"https://example.com/hello"}`)},
		}
	}
	step := func(url string) *uploadv1alpha1.NexusUpload {
		return &uploadv1alpha1.NexusUpload{
			Type: uploadv1alpha1.NexusUploadV1alpha1,
			ID:   "upload",
			Spec: &uploadv1alpha1.RepositoryUploadSpec{
				Resource:         source(),
				ComponentVersion: &uploadv1alpha1.RepositoryUploadComponentVersion{Component: "ocm.software/test", Version: "1.0.0"},
				URL:              url,
				Repository:       "helm-hosted",
			},
		}
	}
	transformer := func() *Transformer {
		repo := &uploadtest.ResourceRepo{Content: []byte("hello"), MediaType: "text/plain"}
		return &Transformer{repositoryupload.Uploader{
			Scheme:             scheme,
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

	t.Run("unsupported format pypi", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeNexus(t, nil, "", false)
		srv.format = "pypi"
		_, err := transformer().Transform(t.Context(), step(srv.URL))
		r.ErrorContains(err, "supported: helm, raw, maven2, npm")
	})

	t.Run("detection returns 403", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeNexus(t, nil, "", false)
		srv.detectionStatus = http.StatusForbidden
		_, err := transformer().Transform(t.Context(), step(srv.URL))
		r.ErrorContains(err, "returned status 403")
	})

	t.Run("invalid repository settings", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeNexus(t, nil, "", false)
		srv.detectionBody = "not json"
		_, err := transformer().Transform(t.Context(), step(srv.URL))
		r.ErrorContains(err, `failed decoding the settings of nexus repository "helm-hosted"`)
	})

	t.Run("target credential error prevents any request", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeNexus(t, nil, "", false)
		helmType := helmidentityv1.Type.String()
		certCreds := uploadtest.CredentialsByType{helmType: &helmcredsv1.HelmHTTPCredentials{
			Type: runtime.NewVersionedType(helmcredsv1.HelmHTTPCredentialsType, helmcredsv1.Version), CertFile: "/cert.pem", KeyFile: "/key.pem",
		}}
		repo := &uploadtest.ResourceRepo{Content: []byte("hello"), MediaType: "text/plain"}
		tr := &Transformer{repositoryupload.Uploader{
			Scheme:             scheme,
			ResourceRepository: repo,
			CredentialProvider: certCreds,
		}}
		_, err := tr.Transform(t.Context(), step(srv.URL))
		r.ErrorContains(err, "HelmHTTPCredentials certFile/keyFile are not supported")
		r.Empty(srv.recorded(), "no request may be sent when credentials fail before detection")
	})
}

func TestTransform_Maven(t *testing.T) {
	const content = "jar bytes"
	contentSum := sha256.Sum256([]byte(content))
	contentDigest := hex.EncodeToString(contentSum[:])

	scheme := runtime.NewScheme()
	scheme.MustRegisterScheme(uploadv1alpha1.Scheme)
	scheme.MustRegisterScheme(wgetaccess.Scheme)

	const (
		mavenPath  = "com/example/demo/1.0.0/demo-1.0.0-sources.jar"
		components = "/service/rest/v1/components"
	)
	source := &descriptorv2.Resource{
		ElementMeta: descriptorv2.ElementMeta{Name: "demo", Version: "1.0.0"},
		Type:        "blob",
		Relation:    descriptorv2.ExternalRelation,
		Access:      &runtime.Raw{Type: runtime.NewVersionedType("Wget", "v1"), Data: []byte(`{"type":"Wget/v1","url":"https://example.com/demo.jar"}`)},
		Digest:      &descriptorv2.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: contentDigest},
	}
	transform := func(t *testing.T, srv *fakeNexus, path string) (runtime.Typed, error) {
		repo := &uploadtest.ResourceRepo{Content: []byte(content), MediaType: "application/java-archive"}
		tr := &Transformer{repositoryupload.Uploader{
			Scheme:             scheme,
			ResourceRepository: repo,
			PollInterval:       time.Millisecond,
		}}
		return tr.Transform(t.Context(), &uploadv1alpha1.NexusUpload{
			Type: uploadv1alpha1.NexusUploadV1alpha1,
			ID:   "upload",
			Spec: &uploadv1alpha1.RepositoryUploadSpec{
				Resource:         source,
				ComponentVersion: &uploadv1alpha1.RepositoryUploadComponentVersion{Component: "ocm.software/test", Version: "1.0.0"},
				URL:              srv.URL + srv.basePath,
				Repository:       "helm-hosted",
				Path:             path,
			},
		})
	}
	newServer := func(t *testing.T) *fakeNexus {
		srv := newFakeNexus(t, nil, "", false)
		srv.format = "maven2"
		return srv
	}

	t.Run("uploads through the components API and publishes the Maven layout URL", func(t *testing.T) {
		r := require.New(t)
		srv := newServer(t)
		out, err := transform(t, srv, mavenPath)
		r.NoError(err)

		r.Equal("POST "+components, srv.recorded()[len(srv.recorded())-1])
		r.Equal(map[string][]string{
			"maven2.groupId":           {"com.example"},
			"maven2.artifactId":        {"demo"},
			"maven2.version":           {"1.0.0"},
			"maven2.generate-pom":      {"false"},
			"maven2.asset1.extension":  {"jar"},
			"maven2.asset1.classifier": {"sources"},
		}, srv.mavenForms[0])
		r.Equal([]byte(content), srv.raw[mavenPath])

		var access wgetaccessv1.Wget
		r.NoError(wgetaccess.Scheme.Convert(out.(*uploadv1alpha1.NexusUpload).Output.Resource.Access, &access))
		r.Equal(srv.URL+"/repository/helm-hosted/"+mavenPath, access.URL)
	})

	t.Run("reuses a stored file with the same content", func(t *testing.T) {
		r := require.New(t)
		srv := newServer(t)
		srv.store(mavenPath, contentDigest)
		_, err := transform(t, srv, mavenPath)
		r.NoError(err)
		r.NotContains(srv.recorded(), "POST "+components)
	})

	t.Run("never overwrites a stored file with other content", func(t *testing.T) {
		r := require.New(t)
		srv := newServer(t)
		srv.store(mavenPath, strings.Repeat("ab", 32))
		_, err := transform(t, srv, mavenPath)
		r.ErrorContains(err, "never overwrites files in maven2 repositories")
		r.NotContains(srv.recorded(), "POST "+components)
	})

	t.Run("stores snapshots with a plain PUT", func(t *testing.T) {
		r := require.New(t)
		srv := newServer(t)
		const snapshot = "com/example/demo/1.0.0-SNAPSHOT/demo-1.0.0-SNAPSHOT.jar"
		_, err := transform(t, srv, snapshot)
		r.NoError(err)
		r.Contains(srv.recorded(), "PUT /repository/helm-hosted/"+snapshot)
		r.NotContains(srv.recorded(), "POST "+components)
	})

	for name, path := range map[string]string{
		"without a path":                   "",
		"with a path outside Maven layout": "files/demo.jar",
		"with a file not named after it":   "com/example/demo/1.0.0/other-1.0.0.jar",
	} {
		t.Run("fails "+name, func(t *testing.T) {
			r := require.New(t)
			srv := newServer(t)
			_, err := transform(t, srv, path)
			r.ErrorContains(err, "Maven repository layout")
			r.NotContains(srv.recorded(), "POST "+components)
		})
	}

	t.Run("components API errors are returned", func(t *testing.T) {
		r := require.New(t)
		srv := newServer(t)
		srv.componentStatus = http.StatusBadRequest
		srv.componentBody = `[{"id":"*","message":"Version policy mismatch"}]`
		_, err := transform(t, srv, mavenPath)
		r.ErrorContains(err, "returned status 400")
		r.ErrorContains(err, "Version policy mismatch")
	})

	t.Run("context path is kept", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeNexus(t, nil, "/nexus", false)
		srv.format = "maven2"
		out, err := transform(t, srv, mavenPath)
		r.NoError(err)
		r.Contains(srv.recorded(), "POST /nexus"+components)
		var access wgetaccessv1.Wget
		r.NoError(wgetaccess.Scheme.Convert(out.(*uploadv1alpha1.NexusUpload).Output.Resource.Access, &access))
		r.Equal(srv.URL+"/nexus/repository/helm-hosted/"+mavenPath, access.URL)
	})
}

func TestParseMavenPath(t *testing.T) {
	for path, want := range map[string]mavenCoordinates{
		"com/example/demo/1.0.0/demo-1.0.0.jar":         {groupID: "com.example", artifactID: "demo", version: "1.0.0", extension: "jar"},
		"com/example/demo/1.0.0/demo-1.0.0.pom":         {groupID: "com.example", artifactID: "demo", version: "1.0.0", extension: "pom"},
		"com/example/demo/1.0.0/demo-1.0.0-sources.jar": {groupID: "com.example", artifactID: "demo", version: "1.0.0", classifier: "sources", extension: "jar"},
		"org/demo/2.0/demo-2.0.tar.gz":                  {groupID: "org", artifactID: "demo", version: "2.0", extension: "tar.gz"},
	} {
		t.Run(path, func(t *testing.T) {
			r := require.New(t)
			got, err := parseMavenPath(path)
			r.NoError(err)
			r.Equal(want, got)
		})
	}
	for _, path := range []string{"demo/1.0.0/demo-1.0.0.jar", "com/example/demo/1.0.0/demo-1.0.0", "com/example/demo/1.0.0/demo-1.0.0-.jar", "com/example/demo/1.0.0/demo-1.0.0-sources"} {
		t.Run("invalid "+path, func(t *testing.T) {
			_, err := parseMavenPath(path)
			require.ErrorContains(t, err, "Maven repository layout")
		})
	}
}

func TestTransform_Npm(t *testing.T) {
	const content = "npm tarball"
	contentSum := sha256.Sum256([]byte(content))
	contentDigest := hex.EncodeToString(contentSum[:])
	const stored = "/@acme/demo/-/demo-2.0.0.tgz"

	scheme := runtime.NewScheme()
	scheme.MustRegisterScheme(uploadv1alpha1.Scheme)
	scheme.MustRegisterScheme(wgetaccess.Scheme)

	transform := func(t *testing.T, srv *fakeNexus, digest, path string) (runtime.Typed, error) {
		res := &descriptorv2.Resource{
			ElementMeta: descriptorv2.ElementMeta{Name: "demo", Version: "2.0.0"},
			Type:        "npmPackage",
			Relation:    descriptorv2.ExternalRelation,
			Access:      &runtime.Raw{Type: runtime.NewVersionedType("Wget", "v1"), Data: []byte(`{"type":"Wget/v1","url":"https://example.com/demo.tgz"}`)},
		}
		if digest != "" {
			res.Digest = &descriptorv2.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: digest}
		}
		repo := &uploadtest.ResourceRepo{Content: []byte(content)}
		tr := &Transformer{repositoryupload.Uploader{
			Scheme:             scheme,
			ResourceRepository: repo,
			PollInterval:       time.Millisecond,
		}}
		return tr.Transform(t.Context(), &uploadv1alpha1.NexusUpload{
			Type: uploadv1alpha1.NexusUploadV1alpha1,
			ID:   "upload",
			Spec: &uploadv1alpha1.RepositoryUploadSpec{
				Resource:         res,
				ComponentVersion: &uploadv1alpha1.RepositoryUploadComponentVersion{Component: "ocm.software/test", Version: "1.0.0"},
				URL:              srv.URL,
				Repository:       "helm-hosted",
				Path:             path,
			},
		})
	}
	newServer := func(t *testing.T) *fakeNexus {
		srv := newFakeNexus(t, nil, "", false)
		srv.format = "npm"
		srv.npm = map[string][2]string{contentDigest: {"@acme/demo", "2.0.0"}}
		return srv
	}
	accessURL := func(r *require.Assertions, out runtime.Typed) string {
		var access wgetaccessv1.Wget
		r.NoError(wgetaccess.Scheme.Convert(out.(*uploadv1alpha1.NexusUpload).Output.Resource.Access, &access))
		return access.URL
	}

	t.Run("uploads through the components API and publishes the stored tarball", func(t *testing.T) {
		r := require.New(t)
		srv := newServer(t)
		srv.searchLag = 1
		out, err := transform(t, srv, "", "")
		r.NoError(err)
		r.Contains(srv.recorded(), "POST /service/rest/v1/components")
		r.Equal(srv.URL+"/repository/helm-hosted"+stored, accessURL(r, out))
	})

	t.Run("reuses a stored package with the same content", func(t *testing.T) {
		r := require.New(t)
		srv := newServer(t)
		srv.store(strings.TrimPrefix(stored, "/"), contentDigest)
		out, err := transform(t, srv, contentDigest, "")
		r.NoError(err)
		r.NotContains(srv.recorded(), "POST /service/rest/v1/components")
		r.Equal(srv.URL+"/repository/helm-hosted"+stored, accessURL(r, out))
	})

	t.Run("fails for content that is not an npm package", func(t *testing.T) {
		r := require.New(t)
		srv := newServer(t)
		srv.npm = nil
		_, err := transform(t, srv, "", "")
		r.ErrorContains(err, "Name and version are mandatory fields")
	})

	t.Run("rejects a path", func(t *testing.T) {
		r := require.New(t)
		srv := newServer(t)
		_, err := transform(t, srv, "", "packages/demo.tgz")
		r.ErrorContains(err, "path is not supported for nexus npm repositories")
	})

	t.Run("fails when the search never finds the uploaded tarball", func(t *testing.T) {
		r := require.New(t)
		srv := newServer(t)
		srv.searchLag = 1000
		_, err := transform(t, srv, "", "")
		r.ErrorContains(err, "but its search does not find it by SHA-256")
		got := srv.recorded()
		upload := slices.Index(got, "POST /service/rest/v1/components")
		r.NotEqual(-1, upload)
		searches := 0
		for _, req := range got[upload+1:] {
			r.Equal("GET /service/rest/v1/search/assets", req)
			searches++
		}
		r.Equal(repositoryupload.PollAttempts, searches, "the search is polled after the upload")
	})

	t.Run("wrong source digest fails after the upload", func(t *testing.T) {
		r := require.New(t)
		srv := newServer(t)
		otherSum := sha256.Sum256([]byte("other"))
		_, err := transform(t, srv, hex.EncodeToString(otherSum[:]), "")
		r.ErrorContains(err, "digest mismatch:")
		r.ErrorContains(err, "nexus keeps the uploaded package in repository helm-hosted")
	})

	t.Run("redeploy rejection is returned", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeNexus(t, nil, "", true)
		srv.format = "npm"
		srv.npm = map[string][2]string{contentDigest: {"@acme/demo", "2.0.0"}}
		srv.store(strings.TrimPrefix(stored, "/"), strings.Repeat("ab", 32))
		_, err := transform(t, srv, "", "")
		r.ErrorContains(err, "returned status 409")
		r.ErrorContains(err, "redeploy is not allowed")
	})
}
