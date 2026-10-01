// Package nexustest provides an in-memory Sonatype Nexus Repository 3 hosted repository for
// tests of the Nexus uploader. A [FakeRepository] is an [http.Handler]; serve it with
// [httptest.NewServer]:
//
//	repo := &nexustest.FakeRepository{Format: "raw"}
//	srv := httptest.NewServer(repo)
//	defer srv.Close()
package nexustest

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"maps"
	"mime"
	"mime/multipart"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Key is the name of the repository a [FakeRepository] serves.
const Key = "helm-hosted"

// Package is the name and version Nexus reads from content it recognizes as a package.
type Package struct {
	Name, Version string
}

// Request is a request a [FakeRepository] received.
type Request struct {
	Method, Path, Query, Authorization string
}

// String returns the method and path of the request, e.g. "GET /service/rest/v1/search".
func (r Request) String() string { return r.Method + " " + r.Path }

// FakeRepository emulates the endpoints of a Nexus hosted repository the uploader uses. It
// stores an uploaded chart under <name>-<version>, an npm package under
// <name>/-/<name>-<version>.tgz, a maven2 component asset at its Maven layout path and any
// other PUT at its path. Package recognition is a lookup of the SHA-256 of the content in Charts
// and NPM.
//
// The zero value is a helm hosted repository at the server root that recognizes no content.
// Configure the exported fields before serving the first request.
type FakeRepository struct {
	// Charts maps the hex SHA-256 of content recognized as a Helm chart to the chart.
	Charts map[string]Package
	// NPM maps the hex SHA-256 of content recognized as an npm package to the package.
	NPM map[string]Package
	// BasePath is the context path the repository is served under, e.g. /nexus.
	BasePath string
	// AllowOnce rejects redeploying a stored chart or npm package, like the "allow once"
	// deployment policy.
	AllowOnce bool
	// Format and Type are reported by GET /service/rest/v1/repositories/<key>; empty means helm
	// and hosted.
	Format, Type string
	// DetectionStatus, if set to a non-200 status, fails the repository settings request.
	DetectionStatus int
	// DetectionBody, if set, is written verbatim as the repository settings.
	DetectionBody string
	// HeadStatus, if set, answers every HEAD of a file.
	HeadStatus int
	// ComponentStatus and ComponentBody, if the status is set, answer every components upload.
	ComponentStatus int
	ComponentBody   string
	// SearchLag is the number of asset searches that find nothing yet, like Nexus indexing
	// stored assets for search shortly after the upload.
	SearchLag int
	// AssetPageSize, if set, pages the asset search with continuation tokens, listing sibling
	// assets first, like a name with search wildcards that puts the exact asset on a later page.
	AssetPageSize int

	mu         sync.Mutex
	requests   []Request
	files      map[string]file // <name>-<version> for charts, else path -> file
	mavenForms []map[string][]string
}

type file struct {
	sha256  string
	content []byte
}

// Store records content of the hex SHA-256 at path, or for a chart at <name>-<version>, as if
// it had been uploaded.
func (f *FakeRepository) Store(path, sha256Hex string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.store(path, file{sha256: sha256Hex})
}

func (f *FakeRepository) store(path string, stored file) {
	if f.files == nil {
		f.files = map[string]file{}
	}
	f.files[path] = stored
}

// Requests returns the requests received so far.
func (f *FakeRepository) Requests() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.requests)
}

// MavenForms returns the form values of the maven2 components uploads received so far.
func (f *FakeRepository) MavenForms() []map[string][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.mavenForms)
}

// Content returns the content of the file uploaded to path, nil when none was.
func (f *FakeRepository) Content(path string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.files[path].content
}

func (f *FakeRepository) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, Request{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Authorization: r.Header.Get("Authorization")})

	rest, repoPrefix := f.BasePath+"/service/rest/v1", f.BasePath+"/repository/"+Key+"/"
	switch {
	case r.Method == http.MethodGet && r.URL.Path == rest+"/repositories/"+Key:
		f.serveSettings(w)
	case r.Method == http.MethodGet && r.URL.Path == rest+"/search/assets":
		f.serveAssets(w, r)
	case r.Method == http.MethodGet && r.URL.Path == rest+"/search":
		f.serveComponents(w, r)
	case r.Method == http.MethodPost && r.URL.Path == rest+"/components":
		f.uploadComponent(w, r, body)
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, repoPrefix):
		f.put(w, strings.TrimPrefix(r.URL.Path, repoPrefix), body)
	case r.Method == http.MethodHead && strings.HasPrefix(r.URL.Path, repoPrefix):
		_, ok := f.files[strings.TrimPrefix(r.URL.Path, repoPrefix)]
		switch {
		case f.HeadStatus != 0:
			w.WriteHeader(f.HeadStatus)
		case ok:
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	default:
		http.Error(w, "unexpected request: "+r.Method+" "+r.URL.Path, http.StatusBadRequest)
	}
}

func (f *FakeRepository) serveSettings(w http.ResponseWriter) {
	switch {
	case f.DetectionStatus != 0 && f.DetectionStatus != http.StatusOK:
		http.Error(w, http.StatusText(f.DetectionStatus), f.DetectionStatus)
	case f.DetectionBody != "":
		_, _ = io.WriteString(w, f.DetectionBody)
	default:
		writeJSON(w, map[string]string{"format": cmp.Or(f.Format, "helm"), "type": cmp.Or(f.Type, "hosted")})
	}
}

type asset struct {
	Path     string            `json:"path"`
	Checksum map[string]string `json:"checksum"`
}

// serveAssets answers the asset search by sha256, by name, or by Maven coordinates; the latter
// also finds a sibling checksum file.
func (f *FakeRepository) serveAssets(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	name := q.Get("name")
	if artifactID := q.Get("maven.artifactId"); artifactID != "" {
		file := artifactID + "-" + q.Get("maven.baseVersion")
		if classifier := q.Get("maven.classifier"); classifier != "" {
			file += "-" + classifier
		}
		name = "/" + strings.ReplaceAll(q.Get("maven.groupId"), ".", "/") + "/" + artifactID + "/" + q.Get("maven.baseVersion") + "/" + file + "." + q.Get("maven.extension")
	}
	assets := []asset{}
	switch sha := q.Get("sha256"); {
	case f.SearchLag > 0:
		f.SearchLag--
	case q.Get("repository") != Key:
	case sha != "":
		for _, path := range slices.Sorted(maps.Keys(f.files)) {
			if f.files[path].sha256 == sha {
				assets = append(assets, asset{Path: "/" + path, Checksum: map[string]string{"sha256": sha}})
			}
		}
	case name != "":
		if stored, ok := f.files[strings.TrimPrefix(name, "/")]; ok {
			assets = append(assets,
				asset{Path: name, Checksum: map[string]string{"sha256": stored.sha256}},
				asset{Path: name + ".sha1", Checksum: map[string]string{"sha256": "other"}})
		}
	}
	if f.AssetPageSize == 0 {
		writeJSON(w, map[string]any{"items": assets})
		return
	}
	slices.Reverse(assets)
	start, _ := strconv.Atoi(q.Get("continuationToken"))
	end := min(start+f.AssetPageSize, len(assets))
	page := map[string]any{"items": assets[start:end]}
	if end < len(assets) {
		page["continuationToken"] = strconv.Itoa(end)
	}
	writeJSON(w, page)
}

// serveComponents answers the helm component search by sha256.
func (f *FakeRepository) serveComponents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	components := []Package{}
	if q.Get("repository") == Key && q.Get("format") == "helm" {
		for path, stored := range f.files {
			if chart, ok := f.Charts[stored.sha256]; ok && stored.sha256 == q.Get("sha256") && path == chart.Name+"-"+chart.Version {
				components = append(components, chart)
			}
		}
	}
	items := make([]map[string]string, 0, len(components))
	for _, c := range components {
		items = append(items, map[string]string{"format": "helm", "name": c.Name, "version": c.Version})
	}
	writeJSON(w, map[string]any{"items": items, "continuationToken": nil})
}

// put stores a chart under <name>-<version>, and anything else at path.
func (f *FakeRepository) put(w http.ResponseWriter, path string, body []byte) {
	sha256Hex := sha256Of(body)
	if chart, ok := f.Charts[sha256Hex]; ok {
		path = chart.Name + "-" + chart.Version
		if _, exists := f.files[path]; exists && f.AllowOnce {
			http.Error(w, Key+"/"+path+".tgz -  cannot be updated as asset already exists and redeploy is not allowed", http.StatusConflict)
			return
		}
	}
	f.store(path, file{sha256: sha256Hex, content: body})
	w.WriteHeader(http.StatusOK)
}

// uploadComponent stores the single asset of an npm or maven2 components upload.
func (f *FakeRepository) uploadComponent(w http.ResponseWriter, r *http.Request, body []byte) {
	if f.ComponentStatus != 0 {
		http.Error(w, f.ComponentBody, f.ComponentStatus)
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
	asset := "maven2.asset1"
	if len(form.File["npm.asset"]) == 1 {
		asset = "npm.asset"
	}
	if len(form.File[asset]) != 1 {
		http.Error(w, "missing asset "+asset, http.StatusBadRequest)
		return
	}
	upload, err := form.File[asset][0].Open()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	content, _ := io.ReadAll(upload)
	sha256Hex := sha256Of(content)

	var path string
	if asset == "npm.asset" {
		pkg, ok := f.NPM[sha256Hex]
		if !ok {
			http.Error(w, `[{"id":"*","message":"Name and version are mandatory fields"}]`, http.StatusBadRequest)
			return
		}
		path = pkg.Name + "/-/" + pkg.Name[strings.LastIndex(pkg.Name, "/")+1:] + "-" + pkg.Version + ".tgz"
		if _, exists := f.files[path]; exists && f.AllowOnce {
			http.Error(w, Key+"/"+path+" -  cannot be updated as asset already exists and redeploy is not allowed", http.StatusConflict)
			return
		}
	} else {
		f.mavenForms = append(f.mavenForms, form.Value)
		name := field("maven2.artifactId") + "-" + field("maven2.version")
		if classifier := field("maven2.asset1.classifier"); classifier != "" {
			name += "-" + classifier
		}
		path = strings.ReplaceAll(field("maven2.groupId"), ".", "/") + "/" + field("maven2.artifactId") + "/" + field("maven2.version") + "/" + name + "." + field("maven2.asset1.extension")
	}
	f.store(path, file{sha256: sha256Hex, content: content})
	w.WriteHeader(http.StatusNoContent)
}

func sha256Of(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func writeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
