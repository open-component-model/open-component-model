// Package artifactorytest holds a fake Artifactory server for the Artifactory uploader tests.
package artifactorytest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// Repository is the key of the only repository the fake serves.
const Repository = "helm-local"

// Request is a request the fake received.
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

// Server emulates the parts of an Artifactory repository the uploader uses. Like Artifactory,
// it records chart name and version properties for deployed content it recognizes as a chart;
// here, recognition is a lookup of the content digest in Charts. Matrix parameters of a deploy
// are stored as properties of the file.
type Server struct {
	*httptest.Server
	// Charts maps the sha256 of content Artifactory recognizes as a chart to its name and version.
	Charts map[string][2]string
	// NPM maps the sha256 of content Artifactory recognizes as an npm package to its name and
	// version.
	NPM map[string][2]string

	// PackageType and RClass control the GET /artifactory/api/repositories/<repo> response.
	PackageType string
	RClass      string
	// DetectionStatus overrides the response status of the detection endpoint (0 means 200).
	DetectionStatus int
	// DetectionBody, if set, is written verbatim as the 200 detection response.
	DetectionBody string
	// StoredPath maps a deploy path to the path the file is stored under, like Artifactory
	// storing a Maven -SNAPSHOT file under its unique version. nil stores files as requested.
	StoredPath func(path string) string

	mu         sync.Mutex
	requests   []Request
	contents   map[string]bool              // sha256 of stored content
	paths      map[string]string            // repository path -> sha256
	properties map[string]map[string]string // repository path -> properties
}

// New starts a fake helm repository that recognizes charts, see [Server.Charts].
func New(t *testing.T, charts map[string][2]string) *Server {
	t.Helper()
	f := &Server{
		Charts:      charts,
		PackageType: "helm",
		RClass:      "local",
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

// Store records content with digest at the repository path with properties, replacing earlier
// ones, as if it had been deployed.
func (f *Server) Store(path, digest string, props map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.store(path, digest, props)
}

func (f *Server) store(path, digest string, props map[string]string) {
	f.paths[path] = digest
	f.properties[path] = props
}

func (f *Server) handle(w http.ResponseWriter, r *http.Request) {
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

	const repoPrefix, storagePrefix = "/artifactory/" + Repository + "/", "/artifactory/api/storage/" + Repository + "/"

	if r.Method == http.MethodGet && path == "/artifactory/api/repositories/"+Repository {
		switch {
		case f.DetectionStatus != 0 && f.DetectionStatus != http.StatusOK:
			http.Error(w, "forbidden", f.DetectionStatus)
		case f.DetectionBody != "":
			_, _ = io.WriteString(w, f.DetectionBody)
		default:
			writeJSON(w, map[string]string{"packageType": f.PackageType, "rclass": f.RClass})
		}
		return
	}

	switch {
	case r.Method == http.MethodGet && strings.HasPrefix(path, storagePrefix) && !r.URL.Query().Has("properties"):
		digest, ok := f.paths[strings.TrimPrefix(path, storagePrefix)]
		if !ok {
			http.Error(w, `{"errors":[{"status":404,"message":"Unable to find item"}]}`, http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]any{"checksums": map[string]string{"sha256": digest}})
	case r.Method == http.MethodGet && strings.HasPrefix(path, storagePrefix):
		stored := strings.TrimPrefix(path, storagePrefix)
		all := map[string][]string{}
		if chart, ok := f.Charts[f.paths[stored]]; ok {
			all["chart.name"], all["chart.version"] = []string{chart[0]}, []string{chart[1]}
		}
		if pkg, ok := f.NPM[f.paths[stored]]; ok {
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
		writeJSON(w, map[string]any{"properties": props})
	case r.Method == http.MethodDelete && strings.HasPrefix(path, repoPrefix):
		delete(f.paths, strings.TrimPrefix(path, repoPrefix))
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPut && strings.HasPrefix(path, repoPrefix):
		stored := strings.TrimPrefix(path, repoPrefix)
		if f.StoredPath != nil {
			stored = f.StoredPath(stored)
		}
		created := func() {
			w.WriteHeader(http.StatusCreated)
			writeJSON(w, map[string]string{"repo": Repository, "path": "/" + stored})
		}
		if req.Deploy {
			// Deploy by checksum succeeds only for content Artifactory already stores.
			if !f.contents[req.Checksum] {
				http.NotFound(w, r)
				return
			}
			f.store(stored, req.Checksum, params)
			created()
			return
		}
		sum := sha256.Sum256(body)
		digest := hex.EncodeToString(sum[:])
		// Like Artifactory, reject a body that does not match the announced checksum.
		if req.Checksum != "" && req.Checksum != digest {
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

// Recorded returns the requests received so far.
func (f *Server) Recorded() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Request(nil), f.requests...)
}

// Stored reports whether the repository stores a file at path.
func (f *Server) Stored(path string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.paths[path]
	return ok
}

func writeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
