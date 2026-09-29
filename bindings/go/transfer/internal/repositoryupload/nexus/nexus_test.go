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
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

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
	charts map[string][2]string // sha256 -> name, version
	// basePath is the context path the server is served under.
	basePath string
	// allowOnce rejects redeploying a stored chart or npm package, like the Nexus "allow once"
	// deployment policy.
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
	// assetPageSize, if set, pages the asset search with continuation tokens, listing sibling
	// assets first, like a name with search wildcards that puts the exact asset on a later page.
	assetPageSize int

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

func newFakeNexus(t *testing.T, charts, npm map[string][2]string) *fakeNexus {
	t.Helper()
	f := &fakeNexus{
		charts: charts,
		npm:    npm,
		format: "helm",
		typ:    "hosted",
		stored: map[string]string{},
		raw:    map[string][]byte{},
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
		if f.assetPageSize > 0 {
			slices.Reverse(items)
			start, _ := strconv.Atoi(q.Get("continuationToken"))
			end := min(start+f.assetPageSize, len(items))
			page := map[string]any{"items": items[start:end]}
			if end < len(items) {
				page["continuationToken"] = strconv.Itoa(end)
			}
			_ = json.NewEncoder(w).Encode(page)
			return
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

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// uploadRun is what a TestTransform row checks beyond its columns.
type uploadRun struct {
	srv  *fakeNexus
	reqs []string // of the last transfer
}

func (u uploadRun) hasRequest(prefix string) bool {
	return slices.ContainsFunc(u.reqs, func(req string) bool { return strings.HasPrefix(req, prefix) })
}

func TestTransform(t *testing.T) {
	chartTGZ, err := os.ReadFile("../../../../helm/testdata/mychart-0.1.0.tgz")
	require.NoError(t, err)
	chartDigest := sha256Hex(chartTGZ)
	notAChart := []byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xff, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	hello, jar, npmTarball := []byte("hello"), []byte("jar bytes"), []byte("npm tarball")
	helloDigest, jarDigest, npmDigest := sha256Hex(hello), sha256Hex(jar), sha256Hex(npmTarball)
	otherDigest := strings.Repeat("ab", 32)

	scheme := runtime.NewScheme()
	scheme.MustRegisterScheme(uploadv1alpha1.Scheme)
	scheme.MustRegisterScheme(helmaccess.Scheme)
	scheme.MustRegisterScheme(wgetaccess.Scheme)

	const (
		repo         = "/repository/helm-hosted/"
		detect       = "GET /service/rest/v1/repositories/helm-hosted"
		chartPut     = "PUT " + repo + "renamed-9.9.9.tgz"
		search       = "GET /service/rest/v1/search"
		searchAssets = "GET /service/rest/v1/search/assets"
		components   = "POST /service/rest/v1/components"
		rawPath      = "ocm.software/test/1.0.0/renamed-9.9.9"
		mavenPath    = "com/example/demo/1.0.0/demo-1.0.0-sources.jar"
		npmStored    = "@acme/demo/-/demo-2.0.0.tgz"
	)
	resource := func(name, version, digest string) func(*descriptorv2.Resource) {
		return func(res *descriptorv2.Resource) {
			res.Name, res.Version = name, version
			if digest != "" {
				res.Digest = &descriptorv2.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: digest}
			}
		}
	}
	withDigest := func(digest string) func(*descriptorv2.Resource) { return resource("renamed", "9.9.9", digest) }
	mavenSource, npmSource := resource("demo", "1.0.0", jarDigest), resource("demo", "2.0.0", "")
	store := func(key, digest string) func(*fakeNexus) {
		return func(srv *fakeNexus) { srv.store(key, digest) }
	}
	nothingWritten := func(r *require.Assertions, u uploadRun) {
		r.False(u.hasRequest("PUT "), "nothing may be written")
		r.False(u.hasRequest(components), "nothing may be written")
	}

	type access struct {
		helmChart string // Helm/v1 access in the Nexus Helm repository
		url       string // else Wget/v1 access on srv.URL+url
		mediaType string
	}
	tests := []struct {
		name     string
		repoType string // repository format; default helm
		// content is served by the source repository (default chartTGZ) with mediaType.
		content   []byte
		mediaType string
		resource  func(*descriptorv2.Resource)
		path      string
		creds     uploadtest.CredentialsByType
		seed      func(*fakeNexus)
		transfers int // requests and output of the last transfer are checked; default 1
		wantErr   string
		// wantAccess and wantDigest (genericBlobDigest/v1 value) describe the published resource.
		wantAccess   access
		wantDigest   string
		wantRequests []string // method and path, where the request order is the behavior
		check        func(*require.Assertions, uploadRun)
	}{
		{
			name:         "helm uploads to the repository root and publishes the chart nexus stores",
			wantAccess:   access{helmChart: "mychart:0.1.0"},
			wantDigest:   chartDigest,
			wantRequests: []string{detect, chartPut, search},
		},
		{
			name:       "helm content already stored is not uploaded again",
			resource:   withDigest(chartDigest),
			seed:       store("mychart-0.1.0", chartDigest),
			wantAccess: access{helmChart: "mychart:0.1.0"},
			wantDigest: chartDigest,
			check:      nothingWritten,
		},
		{
			name:         "helm redeploy rejection of the content already stored succeeds",
			seed:         func(srv *fakeNexus) { srv.allowOnce = true; srv.store("mychart-0.1.0", chartDigest) },
			wantAccess:   access{helmChart: "mychart:0.1.0"},
			wantRequests: []string{detect, chartPut, search, search},
		},
		{
			name:    "helm redeploy rejection of different content under the same chart version fails",
			seed:    func(srv *fakeNexus) { srv.allowOnce = true; srv.store("mychart-0.1.0", otherDigest) },
			wantErr: "returned status 409",
		},
		{
			name:         "helm context path is kept",
			seed:         func(srv *fakeNexus) { srv.basePath = "/nexus" },
			wantAccess:   access{helmChart: "mychart:0.1.0"},
			wantRequests: []string{"GET /nexus/service/rest/v1/repositories/helm-hosted", "PUT /nexus" + repo + "renamed-9.9.9.tgz", "GET /nexus/service/rest/v1/search"},
		},
		{
			name:     "source digest mismatch fails after the upload and deletes nothing",
			resource: withDigest(otherDigest),
			wantErr:  "digest mismatch: expected sha256:" + otherDigest + ", got sha256:" + chartDigest,
			check:    func(r *require.Assertions, u uploadRun) { r.False(u.hasRequest(http.MethodDelete)) },
		},
		{
			name:    "helm content nexus does not recognize as a chart fails",
			content: notAChart,
			wantErr: "content of resource name=renamed,version=9.9.9 is not a helm chart: nexus recorded no chart name and version for {url}" + repo + "renamed-9.9.9.tgz",
		},
		{
			name:    "helm path is rejected",
			path:    "custom/chart.tgz",
			wantErr: "path is not supported for nexus helm repositories",
		},
		{
			name:         "raw first upload PUTs the content and publishes a Wget access",
			repoType:     "raw",
			content:      hello,
			mediaType:    "text/plain",
			wantAccess:   access{url: repo + rawPath, mediaType: "text/plain"},
			wantDigest:   helloDigest,
			wantRequests: []string{detect, "HEAD " + repo + rawPath, "PUT " + repo + rawPath},
		},
		{
			name:     "raw SHA-512 source digest is verified after the upload",
			repoType: "raw",
			content:  hello,
			resource: func(res *descriptorv2.Resource) {
				res.Digest = &descriptorv2.Digest{HashAlgorithm: "SHA-512", NormalisationAlgorithm: "genericBlobDigest/v1", Value: digest.SHA512.FromBytes(hello).Encoded()}
			},
			wantAccess:   access{url: repo + rawPath},
			wantRequests: []string{detect, "HEAD " + repo + rawPath, "PUT " + repo + rawPath},
		},
		{
			name:         "raw re-transfer reuses the file once the search finds it",
			repoType:     "raw",
			content:      hello,
			resource:     withDigest(helloDigest),
			seed:         func(srv *fakeNexus) { srv.searchLag = 2 },
			transfers:    2,
			wantAccess:   access{url: repo + rawPath},
			wantRequests: []string{detect, "HEAD " + repo + rawPath, searchAssets, searchAssets, searchAssets},
		},
		{
			name:         "raw file with the same content on a later search page is reused",
			repoType:     "raw",
			content:      hello,
			resource:     withDigest(helloDigest),
			seed:         func(srv *fakeNexus) { srv.store(rawPath, helloDigest); srv.assetPageSize = 1 },
			wantAccess:   access{url: repo + rawPath},
			wantRequests: []string{detect, "HEAD " + repo + rawPath, searchAssets, searchAssets},
		},
		{
			name:     "raw file with other content at the path is never overwritten",
			repoType: "raw",
			content:  hello,
			resource: withDigest(helloDigest),
			seed:     store(rawPath, otherDigest),
			wantErr:  `nexus repository "helm-hosted" already stores a different file at {url}` + repo + rawPath + "; the uploader never overwrites files in raw repositories, configure a different path",
			check:    nothingWritten,
		},
		{
			name:         "raw custom path",
			repoType:     "raw",
			content:      hello,
			path:         "files/notes.txt",
			wantAccess:   access{url: repo + "files/notes.txt"},
			wantRequests: []string{detect, "HEAD " + repo + "files/notes.txt", "PUT " + repo + "files/notes.txt"},
		},
		{
			name:     "raw unexpected HEAD status fails before uploading",
			repoType: "raw",
			content:  hello,
			resource: withDigest(helloDigest),
			// Not a 5xx, which the HTTP client retries with backoff.
			seed:    func(srv *fakeNexus) { srv.headStatus = http.StatusForbidden },
			wantErr: "returned status 403",
			check:   nothingWritten,
		},
		{
			name:     "target credentials are sent on every request",
			repoType: "raw",
			content:  hello,
			creds: uploadtest.CredentialsByType{wgetidentityv1.Type.String(): &wgetcredsv1.WgetCredentials{
				Type: wgetcredsv1.WgetCredentialsVersionedType, Username: "u", Password: "p",
			}},
			check: func(r *require.Assertions, u uploadRun) {
				r.NotEmpty(u.srv.auth)
				for _, auth := range u.srv.auth {
					r.Equal("Basic dTpw", auth)
				}
			},
		},
		{
			name: "target credential error prevents any request",
			creds: uploadtest.CredentialsByType{helmidentityv1.Type.String(): &helmcredsv1.HelmHTTPCredentials{
				Type: runtime.NewVersionedType(helmcredsv1.HelmHTTPCredentialsType, helmcredsv1.Version), CertFile: "/cert.pem", KeyFile: "/key.pem",
			}},
			wantErr: "HelmHTTPCredentials certFile/keyFile are not supported",
			check:   func(r *require.Assertions, u uploadRun) { r.Empty(u.reqs) },
		},
		{
			name:    "proxy repositories are rejected",
			seed:    func(srv *fakeNexus) { srv.typ = "proxy" },
			wantErr: `nexus repository "helm-hosted" is a proxy repository; uploads need a hosted repository`,
		},
		{
			name:     "unsupported format",
			repoType: "pypi",
			wantErr:  `nexus repository "helm-hosted" has format "pypi"; supported: helm, raw, maven2, npm`,
		},
		{
			name:    "forbidden repository detection",
			seed:    func(srv *fakeNexus) { srv.detectionStatus = http.StatusForbidden },
			wantErr: `failed detecting the type of nexus repository "helm-hosted": GET {url}/service/rest/v1/repositories/helm-hosted returned status 403`,
		},
		{
			name:    "invalid repository settings",
			seed:    func(srv *fakeNexus) { srv.detectionBody = "not json" },
			wantErr: `failed detecting the type of nexus repository "helm-hosted": failed decoding response of GET`,
		},
		{
			name:       "maven uploads through the components API and publishes the Maven layout URL",
			repoType:   "maven2",
			content:    jar,
			resource:   mavenSource,
			path:       mavenPath,
			wantAccess: access{url: repo + mavenPath},
			check: func(r *require.Assertions, u uploadRun) {
				r.Equal(components, u.reqs[len(u.reqs)-1])
				r.Equal(map[string][]string{
					"maven2.groupId":           {"com.example"},
					"maven2.artifactId":        {"demo"},
					"maven2.version":           {"1.0.0"},
					"maven2.generate-pom":      {"false"},
					"maven2.asset1.extension":  {"jar"},
					"maven2.asset1.classifier": {"sources"},
				}, u.srv.mavenForms[0])
				r.Equal(jar, u.srv.raw[mavenPath])
			},
		},
		{
			name:     "maven reuses a stored file with the same content",
			repoType: "maven2",
			content:  jar,
			resource: mavenSource,
			path:     mavenPath,
			seed:     store(mavenPath, jarDigest),
			check:    nothingWritten,
		},
		{
			name:     "maven never overwrites a stored file with other content",
			repoType: "maven2",
			content:  jar,
			resource: mavenSource,
			path:     mavenPath,
			seed:     store(mavenPath, otherDigest),
			wantErr:  "the uploader never overwrites files in maven2 repositories",
			check:    nothingWritten,
		},
		{
			name:       "maven uploads a POM declaring the coordinates of its path",
			repoType:   "maven2",
			content:    []byte(`<project><parent><groupId>com.example</groupId></parent><artifactId>demo</artifactId><version>1.0.0</version></project>`),
			resource:   resource("demo", "1.0.0", ""),
			path:       "com/example/demo/1.0.0/demo-1.0.0.pom",
			wantAccess: access{url: repo + "com/example/demo/1.0.0/demo-1.0.0.pom"},
		},
		{
			name:     "maven rejects a POM declaring other coordinates than its path",
			repoType: "maven2",
			content:  []byte(`<project><groupId>org.example</groupId><artifactId>actual</artifactId><version>2.0</version></project>`),
			resource: resource("demo", "1.0.0", ""),
			path:     "com/example/demo/1.0.0/demo-1.0.0.pom",
			wantErr:  `POM declares org.example:actual:2.0, but path "com/example/demo/1.0.0/demo-1.0.0.pom" is com.example:demo:1.0.0`,
			check:    nothingWritten,
		},
		{
			name:     "maven stores snapshots with a plain PUT",
			repoType: "maven2",
			content:  jar,
			resource: mavenSource,
			path:     "com/example/demo/1.0.0-SNAPSHOT/demo-1.0.0-SNAPSHOT.jar",
			check: func(r *require.Assertions, u uploadRun) {
				r.Contains(u.reqs, "PUT "+repo+"com/example/demo/1.0.0-SNAPSHOT/demo-1.0.0-SNAPSHOT.jar")
				r.NotContains(u.reqs, components)
			},
		},
		{name: "maven fails without a path", repoType: "maven2", content: jar, resource: mavenSource, wantErr: "Maven repository layout", check: nothingWritten},
		{name: "maven fails with a path outside the Maven layout", repoType: "maven2", content: jar, resource: mavenSource, path: "files/demo.jar", wantErr: "Maven repository layout", check: nothingWritten},
		{name: "maven fails with a file not named after the artifact", repoType: "maven2", content: jar, resource: mavenSource, path: "com/example/demo/1.0.0/other-1.0.0.jar", wantErr: "Maven repository layout", check: nothingWritten},
		{
			name:     "maven components API errors are returned",
			repoType: "maven2",
			content:  jar,
			resource: mavenSource,
			path:     mavenPath,
			seed: func(srv *fakeNexus) {
				srv.componentStatus, srv.componentBody = http.StatusBadRequest, `[{"id":"*","message":"Version policy mismatch"}]`
			},
			wantErr: "returned status 400: [{\"id\":\"*\",\"message\":\"Version policy mismatch\"}]",
		},
		{
			name:       "maven context path is kept",
			repoType:   "maven2",
			content:    jar,
			resource:   mavenSource,
			path:       mavenPath,
			seed:       func(srv *fakeNexus) { srv.basePath = "/nexus" },
			wantAccess: access{url: "/nexus" + repo + mavenPath},
			check:      func(r *require.Assertions, u uploadRun) { r.Contains(u.reqs, "POST /nexus/service/rest/v1/components") },
		},
		{
			name:       "npm uploads through the components API and publishes the stored tarball",
			repoType:   "npm",
			content:    npmTarball,
			resource:   npmSource,
			seed:       func(srv *fakeNexus) { srv.searchLag = 1 },
			wantAccess: access{url: repo + npmStored},
			wantDigest: npmDigest,
			check:      func(r *require.Assertions, u uploadRun) { r.Contains(u.reqs, components) },
		},
		{
			name:       "npm reuses a stored package with the same content",
			repoType:   "npm",
			content:    npmTarball,
			resource:   resource("demo", "2.0.0", npmDigest),
			seed:       store(npmStored, npmDigest),
			wantAccess: access{url: repo + npmStored},
			check:      nothingWritten,
		},
		{
			name:     "npm content that is not a package fails",
			repoType: "npm",
			content:  npmTarball,
			resource: npmSource,
			seed:     func(srv *fakeNexus) { srv.npm = nil },
			wantErr:  "Name and version are mandatory fields",
		},
		{
			name:     "npm rejects a path",
			repoType: "npm",
			content:  npmTarball,
			resource: npmSource,
			path:     "packages/demo.tgz",
			wantErr:  "path is not supported for nexus npm repositories",
		},
		{
			name:     "npm fails when the search never finds the uploaded tarball",
			repoType: "npm",
			content:  npmTarball,
			resource: npmSource,
			seed:     func(srv *fakeNexus) { srv.searchLag = 1000 },
			wantErr:  `nexus repository "helm-hosted" stored the npm package sha256:` + npmDigest + ", but its search does not find it",
			check: func(r *require.Assertions, u uploadRun) {
				upload := slices.Index(u.reqs, components)
				r.NotEqual(-1, upload)
				r.Equal(slices.Repeat([]string{searchAssets}, repositoryupload.PollAttempts), u.reqs[upload+1:], "the search is polled after the upload")
			},
		},
		{
			name:     "npm redeploy rejection is returned",
			repoType: "npm",
			content:  npmTarball,
			resource: npmSource,
			seed:     func(srv *fakeNexus) { srv.allowOnce = true; srv.store(npmStored, otherDigest) },
			wantErr:  "redeploy is not allowed",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			srv := newFakeNexus(t, map[string][2]string{chartDigest: {"mychart", "0.1.0"}}, map[string][2]string{npmDigest: {"@acme/demo", "2.0.0"}})
			if tc.repoType != "" {
				srv.format = tc.repoType
			}
			if tc.seed != nil {
				tc.seed(srv)
			}
			content := tc.content
			if content == nil {
				content = chartTGZ
			}
			res := &descriptorv2.Resource{
				ElementMeta: descriptorv2.ElementMeta{Name: "renamed", Version: "9.9.9"},
				Type:        "blob",
				Relation:    descriptorv2.ExternalRelation,
				Access:      &runtime.Raw{Type: runtime.NewVersionedType("Wget", "v1"), Data: []byte(`{"type":"Wget/v1","url":"https://example.com/content"}`)},
			}
			if tc.resource != nil {
				tc.resource(res)
			}
			tr := &Transformer{repositoryupload.Uploader{
				Scheme:             scheme,
				ResourceRepository: &uploadtest.ResourceRepo{Content: content, MediaType: tc.mediaType},
				PollInterval:       time.Millisecond,
			}}
			if tc.creds != nil {
				tr.CredentialProvider = tc.creds
			}
			step := &uploadv1alpha1.NexusUpload{Type: uploadv1alpha1.NexusUploadV1alpha1, ID: "upload", Spec: &uploadv1alpha1.RepositoryUploadSpec{
				Resource:         res,
				ComponentVersion: &uploadv1alpha1.RepositoryUploadComponentVersion{Component: "ocm.software/test", Version: "1.0.0"},
				URL:              srv.URL + srv.basePath,
				Repository:       "helm-hosted",
				Path:             tc.path,
			}}

			var out runtime.Typed
			var err error
			var reqs []string
			for i := range max(tc.transfers, 1) {
				before := len(srv.recorded())
				out, err = tr.Transform(t.Context(), step)
				reqs = srv.recorded()[before:]
				if i < tc.transfers-1 {
					r.NoError(err)
				}
			}

			if tc.wantErr != "" {
				r.ErrorContains(err, strings.ReplaceAll(tc.wantErr, "{url}", srv.URL))
			} else {
				r.NoError(err)
				published := out.(*uploadv1alpha1.NexusUpload).Output.Resource
				switch {
				case tc.wantAccess.helmChart != "":
					var access helmaccessv1.Helm
					r.NoError(helmaccess.Scheme.Convert(published.Access, &access))
					r.Equal(srv.URL+srv.basePath+"/repository/helm-hosted", access.HelmRepository)
					r.Equal(tc.wantAccess.helmChart, access.HelmChart, "name and version come from nexus, not from the resource")
				case tc.wantAccess.url != "":
					var access wgetaccessv1.Wget
					r.NoError(wgetaccess.Scheme.Convert(published.Access, &access))
					r.Equal(srv.URL+tc.wantAccess.url, access.URL)
					if tc.wantAccess.mediaType != "" {
						r.Equal(tc.wantAccess.mediaType, access.MediaType)
					}
				}
				if tc.wantDigest != "" {
					r.Equal(&descriptorv2.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: tc.wantDigest}, published.Digest)
				}
			}
			if tc.wantRequests != nil {
				r.Equal(tc.wantRequests, reqs)
			}
			if tc.check != nil {
				tc.check(r, uploadRun{srv: srv, reqs: reqs})
			}
		})
	}
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
