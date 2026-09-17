package download_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/npm/internal/download"
	accessv1 "ocm.software/open-component-model/bindings/go/npm/spec/access/v1"
	credv1 "ocm.software/open-component-model/bindings/go/npm/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	pkg     = "yargs"
	scoped  = "@types/node"
	version = "17.7.2"
)

// tgz builds a gzipped tar holding a single package.json, so the fixture is a
// tarball a real npm client would accept rather than arbitrary bytes.
func tgz(t *testing.T, name, ver string) []byte {
	t.Helper()

	manifest := fmt.Sprintf(`{"name":%q,"version":%q}`, name, ver)

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: "package/package.json",
		Mode: 0o644,
		Size: int64(len(manifest)),
	}))
	_, err := tw.Write([]byte(manifest))
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())

	return buf.Bytes()
}

// integrityOf returns the Subresource Integrity string npm publishes for data.
func integrityOf(data []byte) string {
	sum := sha512.Sum512(data)
	return "sha512-" + base64.StdEncoding.EncodeToString(sum[:])
}

// shasumOf returns the hex SHA-1 npm publishes as dist.shasum.
func shasumOf(data []byte) string {
	sum := sha1.Sum(data) //nolint:gosec // G401: mirrors the checksum npm publishes
	return hex.EncodeToString(sum[:])
}

// dist is the distribution section of the fixture metadata.
type dist struct {
	Integrity string `json:"integrity,omitempty"`
	Shasum    string `json:"shasum,omitempty"`
	Tarball   string `json:"tarball"`
}

func versionDoc(name, ver string, d dist) []byte {
	body, _ := json.Marshal(map[string]any{"name": name, "version": ver, "dist": d})
	return body
}

func packument(name, ver string, d dist) []byte {
	body, _ := json.Marshal(map[string]any{
		"name":     name,
		"versions": map[string]any{ver: map[string]any{"name": name, "version": ver, "dist": d}},
	})
	return body
}

// request is what a handler saw, so a test can assert on the path order and on
// the headers the download sent.
type request struct {
	path   string
	header http.Header
}

// registry is a fake npm registry. Handlers are registered per path, and the
// server URL is known to them so metadata can point at the server itself.
type registry struct {
	*httptest.Server
	mux      *http.ServeMux
	requests []request
	auth     func(*http.Request) bool
}

func newRegistry(t *testing.T) *registry {
	t.Helper()

	reg := &registry{mux: http.NewServeMux()}
	reg.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reg.requests = append(reg.requests, request{path: r.URL.Path, header: r.Header.Clone()})
		if reg.auth != nil && !reg.auth(r) {
			w.Header().Set("WWW-Authenticate", `Basic realm="npm"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		reg.mux.ServeHTTP(w, r)
	}))
	t.Cleanup(reg.Close)

	return reg
}

func (reg *registry) handle(path string, handler http.HandlerFunc) {
	reg.mux.HandleFunc(path, handler)
}

func (reg *registry) serveJSON(path string, body []byte) {
	reg.handle(path, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
}

func (reg *registry) serveStatus(path string, status int) {
	reg.handle(path, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, http.StatusText(status), status)
	})
}

// publish serves tarball under the registry and returns the dist section
// pointing at it, carrying both checksums npm publishes.
func (reg *registry) publish(name string, tarball []byte) dist {
	path := "/" + name + "/-/tarball.tgz"
	reg.handle(path, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(tarball)
	})

	return dist{Integrity: integrityOf(tarball), Shasum: shasumOf(tarball), Tarball: reg.URL + path}
}

func (reg *registry) serveVersionDoc(name, ver string, d dist) {
	reg.serveJSON("/"+name+"/"+ver, versionDoc(name, ver, d))
}

func (reg *registry) servePackument(name, ver string, d dist) {
	reg.serveJSON("/"+name, packument(name, ver, d))
}

func (reg *registry) paths() []string {
	paths := make([]string, 0, len(reg.requests))
	for _, req := range reg.requests {
		paths = append(paths, req.path)
	}

	return paths
}

// headerFor returns the headers of the first request to path.
func (reg *registry) headerFor(path string) http.Header {
	for _, req := range reg.requests {
		if req.path == path {
			return req.header
		}
	}

	return nil
}

// standard wires the version document and the tarball, which is what npmjs.com
// and Verdaccio serve.
func standard(t *testing.T, reg *registry, name string) []byte {
	t.Helper()

	tarball := tgz(t, name, version)
	reg.serveVersionDoc(name, version, reg.publish(name, tarball))

	return tarball
}

func read(t *testing.T, b *filesystem.Blob) []byte {
	t.Helper()

	rc, err := b.ReadCloser()
	require.NoError(t, err)
	defer func() { require.NoError(t, rc.Close()) }()

	data, err := io.ReadAll(rc)
	require.NoError(t, err)

	return data
}

func npmAccess(registry, name, ver string) *accessv1.NPM {
	return &accessv1.NPM{
		Type:     runtime.NewVersionedType(accessv1.Type, accessv1.Version),
		Registry: registry,
		Package:  name,
		Version:  ver,
	}
}

func basicAuth(user, password string) func(*http.Request) bool {
	return func(req *http.Request) bool {
		gotUser, gotPassword, ok := req.BasicAuth()
		return ok && gotUser == user && gotPassword == password
	}
}

func bearer(token string) func(*http.Request) bool {
	return func(req *http.Request) bool {
		return req.Header.Get("Authorization") == "Bearer "+token
	}
}

// noPackage is the setup of a case that fails before any request is made.
func noPackage(*testing.T, *registry) []byte { return nil }

// downloadCase is one run against a fresh fake registry. setup wires it and
// returns the tarball the download has to produce; a case with wantErr set
// asserts on the error instead.
type downloadCase struct {
	name    string
	setup   func(t *testing.T, reg *registry) []byte
	access  func(reg *registry) *accessv1.NPM
	creds   *credv1.NPMCredentials
	opts    func(tarball []byte) download.Options
	wantErr string
	check   func(r *require.Assertions, reg *registry)
}

func (tc downloadCase) run(t *testing.T) {
	r := require.New(t)

	reg := newRegistry(t)
	tarball := tc.setup(t, reg)

	access := npmAccess(reg.URL, pkg, version)
	if tc.access != nil {
		access = tc.access(reg)
	}

	var opts download.Options
	if tc.opts != nil {
		opts = tc.opts(tarball)
	}

	b, err := download.Download(t.Context(), access, tc.creds, opts)
	if tc.wantErr != "" {
		r.ErrorContains(err, tc.wantErr)
	} else {
		r.NoError(err)
		r.Equal(tarball, read(t, b))
	}

	if tc.check != nil {
		tc.check(r, reg)
	}
}

// integrityCase builds a success case for one Subresource Integrity algorithm.
func integrityCase(name string, integrity func(data []byte) string) downloadCase {
	return downloadCase{
		name: "integrity " + name,
		setup: func(t *testing.T, reg *registry) []byte {
			tarball := tgz(t, pkg, version)
			d := reg.publish(pkg, tarball)
			reg.serveVersionDoc(pkg, version, dist{Integrity: integrity(tarball), Tarball: d.Tarball})
			return tarball
		},
	}
}

// fallbackCase builds a case where the version endpoint answers with status and
// only the packument carries the version. Nexus answers 404 for a plain name and
// 400 for a scoped one (https://github.com/sonatype/nexus-public/issues/224).
func fallbackCase(status int) downloadCase {
	return downloadCase{
		name:   fmt.Sprintf("packument fallback on %d", status),
		access: func(reg *registry) *accessv1.NPM { return npmAccess(reg.URL, scoped, version) },
		setup: func(t *testing.T, reg *registry) []byte {
			tarball := tgz(t, scoped, version)
			reg.serveStatus("/"+scoped+"/"+version, status)
			reg.servePackument(scoped, version, reg.publish(scoped, tarball))
			return tarball
		},
	}
}

func TestDownload(t *testing.T) {
	cases := []downloadCase{{
		name:  "version document",
		setup: func(t *testing.T, reg *registry) []byte { return standard(t, reg, pkg) },
		check: func(r *require.Assertions, reg *registry) {
			r.Equal("identity", reg.headerFor("/"+pkg+"/-/tarball.tgz").Get("Accept-Encoding"),
				"transparent gzip decompression would leave bytes the published checksum does not cover")
		},
	}, {
		name:   "scoped package",
		access: func(reg *registry) *accessv1.NPM { return npmAccess(reg.URL, scoped, version) },
		setup:  func(t *testing.T, reg *registry) []byte { return standard(t, reg, scoped) },
		check: func(r *require.Assertions, reg *registry) {
			// the scope stays literal in the request path, as OCM v1 and the registries expect
			r.Contains(reg.paths(), "/"+scoped+"/"+version)
		},
	}, {
		name:   "trailing slash registry",
		access: func(reg *registry) *accessv1.NPM { return npmAccess(reg.URL+"/", pkg, version) },
		setup:  func(t *testing.T, reg *registry) []byte { return standard(t, reg, pkg) },
		check: func(r *require.Assertions, reg *registry) {
			r.NotContains(strings.Join(reg.paths(), " "), "//")
		},
	}, {
		name: "packument fallback",
		setup: func(t *testing.T, reg *registry) []byte {
			tarball := tgz(t, pkg, version)
			reg.servePackument(pkg, version, reg.publish(pkg, tarball))
			return tarball
		},
		check: func(r *require.Assertions, reg *registry) {
			r.Equal([]string{"/" + pkg + "/" + version, "/" + pkg, "/" + pkg + "/-/tarball.tgz"}, reg.paths())
			// the abbreviated packument spares the registry megabytes of readme prose
			accept := reg.headerFor("/" + pkg).Get("Accept")
			r.Contains(accept, "application/vnd.npm.install-v1+json")
			r.Contains(accept, "application/json", "an older registry must still be able to answer")
		},
	}, {
		name: "version document without a tarball falls back",
		setup: func(t *testing.T, reg *registry) []byte {
			tarball := tgz(t, pkg, version)
			reg.serveJSON("/"+pkg+"/"+version, []byte(`{"name":"`+pkg+`","version":"`+version+`"}`))
			reg.servePackument(pkg, version, reg.publish(pkg, tarball))
			return tarball
		},
	}, {
		// a registry behind an auth proxy answers 200 with an HTML login page
		name: "undecodable version document falls back",
		setup: func(t *testing.T, reg *registry) []byte {
			tarball := tgz(t, pkg, version)
			reg.handle("/"+pkg+"/"+version, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte("<html><body>please log in</body></html>"))
			})
			reg.servePackument(pkg, version, reg.publish(pkg, tarball))
			return tarball
		},
	},
		fallbackCase(http.StatusBadRequest),
		fallbackCase(http.StatusNotFound),
		fallbackCase(http.StatusMethodNotAllowed),
		{
			// the version endpoint resolves dist-tags, so it can answer with another
			// version whose checksums would verify its own tarball perfectly
			name: "version endpoint resolving to another version",
			setup: func(t *testing.T, reg *registry) []byte {
				wanted, other := tgz(t, pkg, version), tgz(t, pkg, "99.0.0")
				reg.serveJSON("/"+pkg+"/"+version, versionDoc(pkg, "99.0.0", reg.publish(pkg+"/other", other)))
				reg.servePackument(pkg, version, reg.publish(pkg, wanted))
				return wanted
			},
		}, {
			// a package old enough to publish only a shasum
			name: "shasum only",
			setup: func(t *testing.T, reg *registry) []byte {
				tarball := tgz(t, pkg, version)
				d := reg.publish(pkg, tarball)
				reg.serveVersionDoc(pkg, version, dist{Shasum: d.Shasum, Tarball: d.Tarball})
				return tarball
			},
		}, {
			// npm's ssri does the same: dist.integrity is the checksum, a shasum left
			// over from an older publish is not ANDed onto it
			name: "integrity wins over a stale shasum",
			setup: func(t *testing.T, reg *registry) []byte {
				tarball := tgz(t, pkg, version)
				d := reg.publish(pkg, tarball)
				d.Shasum = shasumOf([]byte("a digest from an older publish"))
				reg.serveVersionDoc(pkg, version, d)
				return tarball
			},
		}, {
			name: "unknown integrity algorithm beside a known one",
			setup: func(t *testing.T, reg *registry) []byte {
				tarball := tgz(t, pkg, version)
				d := reg.publish(pkg, tarball)
				d.Integrity = "sha3512-" + base64.StdEncoding.EncodeToString([]byte("unknown")) + " " + d.Integrity
				reg.serveVersionDoc(pkg, version, d)
				return tarball
			},
		}, {
			// mirrors npm's pickAlgorithm: the strongest algorithm is verified and a
			// stale weak digest beside it does not fail a package npm installs fine
			name: "strongest integrity wins",
			setup: func(t *testing.T, reg *registry) []byte {
				tarball := tgz(t, pkg, version)
				stale := sha1.Sum([]byte("a digest from an older publish")) //nolint:gosec // G401: mirrors a checksum npm publishes
				d := reg.publish(pkg, tarball)
				d.Integrity = "sha1-" + base64.StdEncoding.EncodeToString(stale[:]) + " " + d.Integrity
				reg.serveVersionDoc(pkg, version, d)
				return tarball
			},
		}, {
			// ssri semantics: several digests may be published for one algorithm and
			// any of them is acceptable
			name: "any digest of the chosen algorithm passes",
			setup: func(t *testing.T, reg *registry) []byte {
				tarball := tgz(t, pkg, version)
				d := reg.publish(pkg, tarball)
				d.Integrity = integrityOf([]byte("another artifact")) + " " + d.Integrity
				reg.serveVersionDoc(pkg, version, d)
				return tarball
			},
		},
		integrityCase("sha512", integrityOf),
		integrityCase("sha384", func(data []byte) string {
			sum := sha512.Sum384(data)
			return "sha384-" + base64.StdEncoding.EncodeToString(sum[:])
		}),
		integrityCase("sha256", func(data []byte) string {
			sum := sha256.Sum256(data)
			return "sha256-" + base64.StdEncoding.EncodeToString(sum[:])
		}),
		integrityCase("sha1", func(data []byte) string {
			sum := sha1.Sum(data) //nolint:gosec // G401: mirrors a checksum npm publishes
			return "sha1-" + base64.StdEncoding.EncodeToString(sum[:])
		}),
		// an entry may carry "?"-separated metadata options
		integrityCase("sha512 with options", func(data []byte) string { return integrityOf(data) + "?foo=bar" }),
		{
			// OCM v1 verifies only what is present, and so does this
			name: "no checksums published",
			setup: func(t *testing.T, reg *registry) []byte {
				tarball := tgz(t, pkg, version)
				reg.serveVersionDoc(pkg, version, dist{Tarball: reg.publish(pkg, tarball).Tarball})
				return tarball
			},
		}, {
			name: "relative tarball url",
			setup: func(t *testing.T, reg *registry) []byte {
				tarball := tgz(t, pkg, version)
				d := reg.publish(pkg, tarball)
				d.Tarball = "/" + pkg + "/-/tarball.tgz"
				reg.serveVersionDoc(pkg, version, d)
				return tarball
			},
		}, {
			name:  "tarball at the size limit",
			setup: func(t *testing.T, reg *registry) []byte { return standard(t, reg, pkg) },
			opts: func(tarball []byte) download.Options {
				return download.Options{MaxDownloadSize: int64(len(tarball))}
			},
		}, {
			name:  "tarball over the size limit",
			setup: func(t *testing.T, reg *registry) []byte { return standard(t, reg, pkg) },
			opts: func(tarball []byte) download.Options {
				return download.Options{MaxDownloadSize: int64(len(tarball)) - 1}
			},
			wantErr: "exceeds maximum allowed size",
		}, {
			// an oversized document is fatal rather than a reason to fall back: the
			// packument the fallback would ask for is the larger of the two
			name:    "metadata over the size limit",
			setup:   func(t *testing.T, reg *registry) []byte { return standard(t, reg, pkg) },
			opts:    func([]byte) download.Options { return download.Options{MaxMetadataSize: 4} },
			wantErr: "metadata document exceeds the maximum allowed size",
		}, {
			name: "version missing from the packument",
			setup: func(t *testing.T, reg *registry) []byte {
				reg.servePackument(pkg, "1.0.0", reg.publish(pkg, tgz(t, pkg, "1.0.0")))
				return nil
			},
			wantErr: `version "17.7.2" of package "yargs" not found`,
		}, {
			name: "metadata server error",
			setup: func(t *testing.T, reg *registry) []byte {
				reg.serveStatus("/"+pkg+"/"+version, http.StatusInternalServerError)
				return nil
			},
			wantErr: "returned status 500",
		}, {
			name: "integrity mismatch",
			setup: func(t *testing.T, reg *registry) []byte {
				d := reg.publish(pkg, []byte("not the published tarball"))
				d.Integrity = integrityOf(tgz(t, pkg, version))
				reg.serveVersionDoc(pkg, version, dist{Integrity: d.Integrity, Tarball: d.Tarball})
				return nil
			},
			wantErr: "dist.integrity (sha512) mismatch",
		}, {
			name: "shasum mismatch",
			setup: func(t *testing.T, reg *registry) []byte {
				d := reg.publish(pkg, tgz(t, pkg, version))
				reg.serveVersionDoc(pkg, version, dist{Shasum: shasumOf([]byte("something else")), Tarball: d.Tarball})
				return nil
			},
			wantErr: "dist.shasum (sha1) mismatch",
		}, {
			name: "malformed integrity",
			setup: func(t *testing.T, reg *registry) []byte {
				d := reg.publish(pkg, tgz(t, pkg, version))
				reg.serveVersionDoc(pkg, version, dist{Integrity: "sha512-!!!not base64!!!", Tarball: d.Tarball})
				return nil
			},
			wantErr: "cannot decode dist.integrity",
		}, {
			// an algorithm this version cannot compute must fail rather than silently
			// store unverified content
			name: "integrity in an unusable algorithm",
			setup: func(t *testing.T, reg *registry) []byte {
				d := reg.publish(pkg, tgz(t, pkg, version))
				d.Integrity = "sha3512-" + base64.StdEncoding.EncodeToString([]byte("future algorithm"))
				reg.serveVersionDoc(pkg, version, dist{Integrity: d.Integrity, Tarball: d.Tarball})
				return nil
			},
			wantErr: "dist.integrity carries no known algorithm",
		}, {
			// a registry publishing a SHA-256 in the SHA-1 field
			name: "shasum that is not a sha1",
			setup: func(t *testing.T, reg *registry) []byte {
				tarball := tgz(t, pkg, version)
				sum := sha256.Sum256(tarball)
				d := reg.publish(pkg, tarball)
				reg.serveVersionDoc(pkg, version, dist{Shasum: hex.EncodeToString(sum[:]), Tarball: d.Tarball})
				return nil
			},
			wantErr: "not a 40-character SHA-1",
		}, {
			name: "tarball url with an unsupported scheme",
			setup: func(t *testing.T, reg *registry) []byte {
				reg.serveVersionDoc(pkg, version, dist{Tarball: "file:///etc/passwd"})
				return nil
			},
			wantErr: `unsupported tarball url scheme "file"`,
		}, {
			name: "tarball not found",
			setup: func(t *testing.T, reg *registry) []byte {
				tarball := tgz(t, pkg, version)
				reg.serveVersionDoc(pkg, version, dist{
					Integrity: integrityOf(tarball),
					Tarball:   reg.URL + "/" + pkg + "/-/missing.tgz",
				})
				return nil
			},
			wantErr: "returned status 404",
		}, {
			name: "basic auth",
			setup: func(t *testing.T, reg *registry) []byte {
				reg.auth = basicAuth("user", "secret")
				return standard(t, reg, pkg)
			},
			creds: &credv1.NPMCredentials{Username: "user", Password: "secret"},
			check: func(r *require.Assertions, reg *registry) {
				// a private Artifactory or Verdaccio needs the credentials on its own tarballs too
				r.Contains(reg.paths(), "/"+pkg+"/-/tarball.tgz")
			},
		}, {
			name: "bearer token",
			setup: func(t *testing.T, reg *registry) []byte {
				reg.auth = bearer("npm-token")
				return standard(t, reg, pkg)
			},
			creds: &credv1.NPMCredentials{Token: "npm-token"},
		}, {
			// an OCM v1 configuration carrying a user name, a password and a token left
			// over from a login: v1 read with basic auth, so honouring the token
			// instead would turn a working setup into a 401
			name: "basic auth wins over a token",
			setup: func(t *testing.T, reg *registry) []byte {
				reg.auth = basicAuth("user", "secret")
				return standard(t, reg, pkg)
			},
			creds: &credv1.NPMCredentials{Username: "user", Password: "secret", Token: "a-stale-token"},
		}, {
			name:    "user name without a password",
			setup:   func(t *testing.T, reg *registry) []byte { return standard(t, reg, pkg) },
			creds:   &credv1.NPMCredentials{Username: "user"},
			wantErr: "carry no password",
		}, {
			name: "missing credentials",
			setup: func(t *testing.T, reg *registry) []byte {
				reg.auth = basicAuth("user", "secret")
				return standard(t, reg, pkg)
			},
			wantErr: "returned status 401",
		}, {
			// the packument would answer a 401 the same way, and the useful message is
			// the one about authentication
			name: "unauthorized does not fall back",
			setup: func(t *testing.T, reg *registry) []byte {
				reg.auth = func(*http.Request) bool { return false }
				return nil
			},
			wantErr: "version metadata request",
			check: func(r *require.Assertions, reg *registry) {
				r.Len(reg.requests, 1, "a 401 must not trigger the packument fallback")
			},
		}, {
			name:    "registry missing",
			setup:   noPackage,
			access:  func(*registry) *accessv1.NPM { return npmAccess("", pkg, version) },
			wantErr: "registry is required",
		}, {
			name:    "registry with an unsupported scheme",
			setup:   noPackage,
			access:  func(*registry) *accessv1.NPM { return npmAccess("ssh://registry.npmjs.org", pkg, version) },
			wantErr: "registry must use the http or https scheme",
		}, {
			name:    "package missing",
			setup:   noPackage,
			access:  func(reg *registry) *accessv1.NPM { return npmAccess(reg.URL, "", version) },
			wantErr: "package is required",
		}, {
			name:    "version is a dist-tag",
			setup:   noPackage,
			access:  func(reg *registry) *accessv1.NPM { return npmAccess(reg.URL, pkg, "latest") },
			wantErr: "must be an exact semantic version",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}

// TestDownloadTempDir covers the file the blob is backed by: it is created in the
// configured directory, outlives the download, and a rejected tarball leaves
// nothing behind.
func TestDownloadTempDir(t *testing.T) {
	t.Run("keeps the file of a verified tarball", func(t *testing.T) {
		r := require.New(t)

		reg := newRegistry(t)
		tarball := standard(t, reg, pkg)
		tempDir := t.TempDir()

		b, err := download.Download(t.Context(), npmAccess(reg.URL, pkg, version), nil, download.Options{TempDir: tempDir})
		r.NoError(err)
		r.Equal(tarball, read(t, b))

		mediaType, ok := b.MediaType()
		r.True(ok)
		r.Equal(download.MediaTypeTGZ, mediaType)

		entries, err := os.ReadDir(tempDir)
		r.NoError(err)
		r.Len(entries, 1, "the tarball is streamed into the configured temp folder")
	})

	t.Run("removes the file of a rejected tarball", func(t *testing.T) {
		r := require.New(t)

		reg := newRegistry(t)
		tempDir := t.TempDir()

		d := reg.publish(pkg, []byte("corrupted"))
		reg.serveVersionDoc(pkg, version, dist{Integrity: integrityOf(tgz(t, pkg, version)), Tarball: d.Tarball})

		_, err := download.Download(t.Context(), npmAccess(reg.URL, pkg, version), nil, download.Options{TempDir: tempDir})
		r.ErrorContains(err, "mismatch")

		entries, err := os.ReadDir(tempDir)
		r.NoError(err)
		r.Empty(entries)
	})
}

// TestDownloadCredentialHosts covers where credentials may travel: dist.tarball
// comes out of a metadata document, so a registry could otherwise point the
// download at a host it does not own, or redirect it onto a plain connection.
func TestDownloadCredentialHosts(t *testing.T) {
	creds := &credv1.NPMCredentials{Token: "npm-token"}

	t.Run("not forwarded to a foreign tarball host", func(t *testing.T) {
		r := require.New(t)

		tarball := tgz(t, pkg, version)

		var gotAuth string
		cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			gotAuth = req.Header.Get("Authorization")
			_, _ = w.Write(tarball)
		}))
		t.Cleanup(cdn.Close)

		reg := newRegistry(t)
		reg.auth = bearer("npm-token")
		reg.serveVersionDoc(pkg, version, dist{Integrity: integrityOf(tarball), Tarball: cdn.URL + "/tarball.tgz"})

		b, err := download.Download(t.Context(), npmAccess(reg.URL, pkg, version), creds, download.Options{})
		r.NoError(err)
		r.Equal(tarball, read(t, b))
		r.Empty(gotAuth, "credentials must not be sent to a tarball host other than the registry")
	})

	t.Run("not carried across an https to http redirect", func(t *testing.T) {
		r := require.New(t)

		reg := newRegistry(t)
		standard(t, reg, pkg)

		// Go drops the Authorization header on a redirect to another host but not on
		// one that only drops TLS, so the redirect has to be refused here
		tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			http.Redirect(w, req, reg.URL+req.URL.Path, http.StatusFound)
		}))
		t.Cleanup(tls.Close)

		_, err := download.Download(t.Context(), npmAccess(tls.URL, pkg, version), creds,
			download.Options{Client: tls.Client()})
		r.ErrorContains(err, "refusing to follow a redirect from https to http")
	})
}
