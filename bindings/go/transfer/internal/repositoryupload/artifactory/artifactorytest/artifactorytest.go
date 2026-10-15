// Package artifactorytest provides an in-memory JFrog Artifactory repository for tests of the
// Artifactory uploader. A [FakeRepository] is an [http.Handler]; serve it with [httptest.NewServer]:
//
//	repo := &artifactorytest.FakeRepository{Charts: map[string]artifactorytest.Package{sha256Hex: {Name: "mychart", Version: "0.1.0"}}}
//	srv := httptest.NewServer(repo)
//	defer srv.Close()
package artifactorytest

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// Key is the key of the repository a [FakeRepository] serves.
const Key = "helm-local"

// Package is the name and version Artifactory records for content it recognizes as a package.
type Package struct {
	Name, Version string
}

// Request is a request a [FakeRepository] received.
type Request struct {
	Method, Path, Query, ContentType string
	Username, Password               string
	Basic                            bool
	Authorization                    string
	Checksum                         string
	Deploy                           bool
	// Properties are the deploy matrix parameters of a PUT.
	Properties map[string]string
	Body       []byte
}

// String returns the method, path and query of the request, e.g.
// "GET /artifactory/api/storage/helm-local/a.tgz?properties=chart.name".
func (r Request) String() string {
	if r.Query == "" {
		return r.Method + " " + r.Path
	}
	return r.Method + " " + r.Path + "?" + r.Query
}

// FakeRepository emulates the endpoints of an Artifactory repository the uploader uses. Like
// Artifactory, it records package properties for deployed content it recognizes; here,
// recognition is a lookup of the SHA-256 of the content in Charts and NPM. Matrix parameters of a
// deploy are stored as properties of the file.
//
// The zero value is a local helm repository that recognizes no content. Configure the exported
// fields before serving the first request.
type FakeRepository struct {
	// Charts maps the hex SHA-256 of content recognized as a Helm chart to the chart.
	Charts map[string]Package
	// NPM maps the hex SHA-256 of content recognized as an npm package to the package.
	NPM map[string]Package
	// PackageType and RClass are reported by GET /artifactory/api/repositories/<key>; empty
	// means helm and local.
	PackageType, RClass string
	// DetectionStatus, if set to a non-200 status, fails the repository configuration request.
	DetectionStatus int
	// DetectionBody, if set, is written verbatim as the repository configuration.
	DetectionBody string
	// StoredPath maps a deploy path to the path the file is stored under, like Artifactory
	// storing a Maven -SNAPSHOT file under its unique version. nil stores files as requested.
	StoredPath func(path string) string

	mu       sync.Mutex
	requests []Request
	contents map[string]bool // hex SHA-256 of all content ever deployed
	files    map[string]file // repository path -> file
}

type file struct {
	sha256     string
	properties map[string]string
}

// Store records a file with content of the hex SHA-256 at the repository path with properties,
// as if it had been deployed.
func (f *FakeRepository) Store(path, sha256Hex string, properties map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.store(path, sha256Hex, properties)
}

func (f *FakeRepository) store(path, sha256Hex string, properties map[string]string) {
	if f.files == nil {
		f.files = map[string]file{}
	}
	f.files[path] = file{sha256: sha256Hex, properties: properties}
}

// Requests returns the requests received so far.
func (f *FakeRepository) Requests() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Request(nil), f.requests...)
}

// Stored reports whether the repository stores a file at path.
func (f *FakeRepository) Stored(path string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.files[path]
	return ok
}

func (f *FakeRepository) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	path, params := splitMatrixParams(r.URL.EscapedPath())
	req := Request{
		Method: r.Method, Path: path, Query: r.URL.RawQuery, ContentType: r.Header.Get("Content-Type"), Authorization: r.Header.Get("Authorization"),
		Checksum: r.Header.Get("X-Checksum-Sha256"), Deploy: r.Header.Get("X-Checksum-Deploy") == "true", Properties: params, Body: body,
	}
	req.Username, req.Password, req.Basic = r.BasicAuth()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)

	const repoPrefix, storagePrefix = "/artifactory/" + Key + "/", "/artifactory/api/storage/" + Key + "/"
	switch {
	case r.Method == http.MethodGet && path == "/artifactory/api/repositories/"+Key:
		f.serveConfiguration(w)
	case r.Method == http.MethodGet && strings.HasPrefix(path, storagePrefix) && !r.URL.Query().Has("properties"):
		stored, ok := f.files[strings.TrimPrefix(path, storagePrefix)]
		if !ok {
			http.Error(w, `{"errors":[{"status":404,"message":"Unable to find item"}]}`, http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]any{"checksums": map[string]string{"sha256": stored.sha256}})
	case r.Method == http.MethodGet && strings.HasPrefix(path, storagePrefix):
		f.serveProperties(w, f.files[strings.TrimPrefix(path, storagePrefix)], strings.Split(r.URL.Query().Get("properties"), ","))
	case r.Method == http.MethodDelete && strings.HasPrefix(path, repoPrefix):
		delete(f.files, strings.TrimPrefix(path, repoPrefix))
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPut && strings.HasPrefix(path, repoPrefix):
		f.deploy(w, req, strings.TrimPrefix(path, repoPrefix))
	default:
		http.Error(w, "unexpected request", http.StatusBadRequest)
	}
}

func (f *FakeRepository) serveConfiguration(w http.ResponseWriter) {
	switch {
	case f.DetectionStatus != 0 && f.DetectionStatus != http.StatusOK:
		http.Error(w, http.StatusText(f.DetectionStatus), f.DetectionStatus)
	case f.DetectionBody != "":
		_, _ = io.WriteString(w, f.DetectionBody)
	default:
		writeJSON(w, map[string]string{"packageType": cmp.Or(f.PackageType, "helm"), "rclass": cmp.Or(f.RClass, "local")})
	}
}

// serveProperties answers a properties request with the requested keys among the owner
// properties and the package properties recorded for the file; none of them yields 404.
func (f *FakeRepository) serveProperties(w http.ResponseWriter, stored file, keys []string) {
	all := map[string][]string{}
	for key, value := range stored.properties {
		all[key] = []string{value}
	}
	for prefix, packages := range map[string]map[string]Package{"chart": f.Charts, "npm": f.NPM} {
		if pkg, ok := packages[stored.sha256]; ok && stored.sha256 != "" {
			all[prefix+".name"], all[prefix+".version"] = []string{pkg.Name}, []string{pkg.Version}
		}
	}
	props := map[string][]string{}
	for _, key := range keys {
		if v, ok := all[key]; ok {
			props[key] = v
		}
	}
	if len(props) == 0 {
		http.Error(w, `{"errors":[{"status":404,"message":"No properties could be found."}]}`, http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"properties": props})
}

// deploy stores a PUT at path. A deploy by checksum succeeds only for content the repository
// already stores; a body that does not match the announced checksum is rejected.
func (f *FakeRepository) deploy(w http.ResponseWriter, req Request, path string) {
	if f.StoredPath != nil {
		path = f.StoredPath(path)
	}
	sha256Hex := req.Checksum
	if !req.Deploy {
		sum := sha256.Sum256(req.Body)
		sha256Hex = hex.EncodeToString(sum[:])
	}
	switch {
	case req.Deploy && !f.contents[sha256Hex]:
		http.Error(w, "not found", http.StatusNotFound)
		return
	case req.Checksum != "" && req.Checksum != sha256Hex:
		http.Error(w, "checksum mismatch", http.StatusConflict)
		return
	}
	if f.contents == nil {
		f.contents = map[string]bool{}
	}
	f.contents[sha256Hex] = true
	f.store(path, sha256Hex, req.Properties)
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]string{"repo": Key, "path": "/" + path})
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

func writeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
