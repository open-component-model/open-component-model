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

	"ocm.software/open-component-model/bindings/go/npm/internal/download"
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

// registry is a fake npm registry. Handlers are registered per path, and the
// server URL is available to them so metadata can point at the server itself.
type registry struct {
	*httptest.Server
	mux      *http.ServeMux
	requests []string
	auth     func(*http.Request) bool
}

func newRegistry(t *testing.T) *registry {
	t.Helper()

	reg := &registry{mux: http.NewServeMux()}
	reg.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reg.requests = append(reg.requests, r.URL.Path)
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

func (reg *registry) serveJSON(path string, body func() []byte) {
	reg.handle(path, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body())
	})
}

func (reg *registry) serveBytes(path string, body []byte) {
	reg.handle(path, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	})
}

// standard wires a registry that serves the version document and the tarball,
// which is what npmjs.com and Verdaccio do.
func standard(t *testing.T, name string) (*registry, []byte) {
	t.Helper()

	reg := newRegistry(t)
	tarball := tgz(t, name, version)

	reg.serveBytes("/"+name+"/-/tarball.tgz", tarball)
	reg.serveJSON("/"+name+"/"+version, func() []byte {
		return versionDoc(name, version, dist{
			Integrity: integrityOf(tarball),
			Shasum:    shasumOf(tarball),
			Tarball:   reg.URL + "/" + name + "/-/tarball.tgz",
		})
	})

	return reg, tarball
}

func read(t *testing.T, b *download.Blob) []byte {
	t.Helper()

	rc, err := b.ReadCloser()
	require.NoError(t, err)
	defer func() { require.NoError(t, rc.Close()) }()

	data, err := io.ReadAll(rc)
	require.NoError(t, err)

	return data
}

func TestDownload(t *testing.T) {
	r := require.New(t)

	reg, tarball := standard(t, pkg)

	b, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version})
	r.NoError(err)
	t.Cleanup(func() { r.NoError(b.Close()) })

	r.Equal(tarball, read(t, b))

	mediaType, ok := b.MediaType()
	r.True(ok)
	r.Equal("application/x-tgz", mediaType)
}

func TestDownloadScopedPackage(t *testing.T) {
	r := require.New(t)

	reg, tarball := standard(t, scoped)

	b, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: scoped, Version: version})
	r.NoError(err)
	t.Cleanup(func() { r.NoError(b.Close()) })

	r.Equal(tarball, read(t, b))
	// the scope stays literal in the request path, as OCM v1 and the registries expect
	r.Contains(reg.requests, "/@types/node/"+version)
}

func TestDownloadTrailingSlashRegistry(t *testing.T) {
	r := require.New(t)

	reg, tarball := standard(t, pkg)

	b, err := download.Download(t.Context(), download.Request{Registry: reg.URL + "/", Package: pkg, Version: version})
	r.NoError(err)
	t.Cleanup(func() { r.NoError(b.Close()) })

	r.Equal(tarball, read(t, b))
	r.NotContains(strings.Join(reg.requests, " "), "//")
}

// TestDownloadPackumentFallback covers Nexus, which answers the version URL with
// 404 and only serves the full packument (https://github.com/sonatype/nexus-public/issues/224).
func TestDownloadPackumentFallback(t *testing.T) {
	r := require.New(t)

	reg := newRegistry(t)
	tarball := tgz(t, pkg, version)

	reg.serveBytes("/"+pkg+"/-/tarball.tgz", tarball)
	reg.serveJSON("/"+pkg, func() []byte {
		return packument(pkg, version, dist{
			Integrity: integrityOf(tarball),
			Shasum:    shasumOf(tarball),
			Tarball:   reg.URL + "/" + pkg + "/-/tarball.tgz",
		})
	})

	b, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version})
	r.NoError(err)
	t.Cleanup(func() { r.NoError(b.Close()) })

	r.Equal(tarball, read(t, b))
	r.Equal([]string{"/" + pkg + "/" + version, "/" + pkg, "/" + pkg + "/-/tarball.tgz"}, reg.requests)
}

// TestDownloadFallbackOnTarballlessVersionDocument covers a registry that answers
// the version URL with 200 but without a dist.tarball.
func TestDownloadFallbackOnTarballlessVersionDocument(t *testing.T) {
	r := require.New(t)

	reg := newRegistry(t)
	tarball := tgz(t, pkg, version)

	reg.serveBytes("/"+pkg+"/-/tarball.tgz", tarball)
	reg.serveJSON("/"+pkg+"/"+version, func() []byte {
		return []byte(`{"name":"` + pkg + `","version":"` + version + `"}`)
	})
	reg.serveJSON("/"+pkg, func() []byte {
		return packument(pkg, version, dist{
			Integrity: integrityOf(tarball),
			Tarball:   reg.URL + "/" + pkg + "/-/tarball.tgz",
		})
	})

	b, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version})
	r.NoError(err)
	t.Cleanup(func() { r.NoError(b.Close()) })

	r.Equal(tarball, read(t, b))
}

func TestDownloadVersionMissingFromPackument(t *testing.T) {
	r := require.New(t)

	reg := newRegistry(t)
	tarball := tgz(t, pkg, version)

	reg.serveJSON("/"+pkg, func() []byte {
		return packument(pkg, "1.0.0", dist{Integrity: integrityOf(tarball), Tarball: reg.URL + "/t.tgz"})
	})

	_, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version})
	r.ErrorContains(err, `version "17.7.2" of package "yargs" not found`)
}

func TestDownloadMetadataServerError(t *testing.T) {
	r := require.New(t)

	reg := newRegistry(t)
	reg.handle("/"+pkg+"/"+version, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})

	_, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version})
	r.ErrorContains(err, "returned status 500")
}

func TestDownloadIntegrityMismatch(t *testing.T) {
	r := require.New(t)

	reg := newRegistry(t)
	tarball := tgz(t, pkg, version)

	reg.serveBytes("/"+pkg+"/-/tarball.tgz", []byte("not the published tarball"))
	reg.serveJSON("/"+pkg+"/"+version, func() []byte {
		return versionDoc(pkg, version, dist{
			Integrity: integrityOf(tarball),
			Tarball:   reg.URL + "/" + pkg + "/-/tarball.tgz",
		})
	})

	_, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version})
	r.ErrorContains(err, "dist.integrity (sha512) mismatch")
}

// TestDownloadShasumMismatch covers a package old enough to publish only a
// shasum, which is then the checksum the tarball is verified against.
func TestDownloadShasumMismatch(t *testing.T) {
	r := require.New(t)

	reg := newRegistry(t)
	tarball := tgz(t, pkg, version)

	reg.serveBytes("/"+pkg+"/-/tarball.tgz", tarball)
	reg.serveJSON("/"+pkg+"/"+version, func() []byte {
		return versionDoc(pkg, version, dist{
			Shasum:  shasumOf([]byte("something else")),
			Tarball: reg.URL + "/" + pkg + "/-/tarball.tgz",
		})
	})

	_, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version})
	r.ErrorContains(err, "dist.shasum (sha1) mismatch")
}

// TestDownloadShasumOnly verifies a package that publishes only a shasum.
func TestDownloadShasumOnly(t *testing.T) {
	r := require.New(t)

	reg := newRegistry(t)
	tarball := tgz(t, pkg, version)

	reg.serveBytes("/"+pkg+"/-/tarball.tgz", tarball)
	reg.serveJSON("/"+pkg+"/"+version, func() []byte {
		return versionDoc(pkg, version, dist{
			Shasum:  shasumOf(tarball),
			Tarball: reg.URL + "/" + pkg + "/-/tarball.tgz",
		})
	})

	b, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version})
	r.NoError(err)
	t.Cleanup(func() { r.NoError(b.Close()) })

	r.Equal(tarball, read(t, b))
}

// TestDownloadIntegrityWinsOverStaleShasum mirrors npm: dist.integrity is the
// checksum, and a shasum left over from an older publish is not ANDed onto it.
// npm's own ssri documents exactly this case.
func TestDownloadIntegrityWinsOverStaleShasum(t *testing.T) {
	r := require.New(t)

	reg := newRegistry(t)
	tarball := tgz(t, pkg, version)

	reg.serveBytes("/"+pkg+"/-/tarball.tgz", tarball)
	reg.serveJSON("/"+pkg+"/"+version, func() []byte {
		return versionDoc(pkg, version, dist{
			Integrity: integrityOf(tarball),
			Shasum:    shasumOf([]byte("a digest from an older publish")),
			Tarball:   reg.URL + "/" + pkg + "/-/tarball.tgz",
		})
	})

	b, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version})
	r.NoError(err)
	t.Cleanup(func() { r.NoError(b.Close()) })

	r.Equal(tarball, read(t, b))
}

// TestDownloadUnknownIntegrityAlgorithm covers a registry that publishes a digest
// in an algorithm this version does not know: the download succeeds as long as
// one known checksum verifies.
func TestDownloadUnknownIntegrityAlgorithm(t *testing.T) {
	r := require.New(t)

	reg := newRegistry(t)
	tarball := tgz(t, pkg, version)

	reg.serveBytes("/"+pkg+"/-/tarball.tgz", tarball)
	reg.serveJSON("/"+pkg+"/"+version, func() []byte {
		return versionDoc(pkg, version, dist{
			Integrity: "sha3512-" + base64.StdEncoding.EncodeToString([]byte("unknown")) + " " + integrityOf(tarball),
			Tarball:   reg.URL + "/" + pkg + "/-/tarball.tgz",
		})
	})

	b, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version})
	r.NoError(err)
	t.Cleanup(func() { r.NoError(b.Close()) })

	r.Equal(tarball, read(t, b))
}

func TestDownloadMalformedIntegrity(t *testing.T) {
	r := require.New(t)

	reg := newRegistry(t)
	tarball := tgz(t, pkg, version)

	reg.serveBytes("/"+pkg+"/-/tarball.tgz", tarball)
	reg.serveJSON("/"+pkg+"/"+version, func() []byte {
		return versionDoc(pkg, version, dist{
			Integrity: "sha512-!!!not base64!!!",
			Tarball:   reg.URL + "/" + pkg + "/-/tarball.tgz",
		})
	})

	_, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version})
	r.ErrorContains(err, "cannot decode dist.integrity")
}

// TestDownloadWithoutChecksums covers a registry that publishes no checksum at
// all: the download succeeds, matching OCM v1, which verifies only what is present.
func TestDownloadWithoutChecksums(t *testing.T) {
	r := require.New(t)

	reg := newRegistry(t)
	tarball := tgz(t, pkg, version)

	reg.serveBytes("/"+pkg+"/-/tarball.tgz", tarball)
	reg.serveJSON("/"+pkg+"/"+version, func() []byte {
		return versionDoc(pkg, version, dist{Tarball: reg.URL + "/" + pkg + "/-/tarball.tgz"})
	})

	b, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version})
	r.NoError(err)
	t.Cleanup(func() { r.NoError(b.Close()) })

	r.Equal(tarball, read(t, b))
}

func TestDownloadRejectsNonHTTPTarball(t *testing.T) {
	r := require.New(t)

	reg := newRegistry(t)
	reg.serveJSON("/"+pkg+"/"+version, func() []byte {
		return versionDoc(pkg, version, dist{Tarball: "file:///etc/passwd"})
	})

	_, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version})
	r.ErrorContains(err, `unsupported tarball url scheme "file"`)
}

func TestDownloadTarballNotFound(t *testing.T) {
	r := require.New(t)

	reg := newRegistry(t)
	tarball := tgz(t, pkg, version)

	reg.serveJSON("/"+pkg+"/"+version, func() []byte {
		return versionDoc(pkg, version, dist{
			Integrity: integrityOf(tarball),
			Tarball:   reg.URL + "/" + pkg + "/-/missing.tgz",
		})
	})

	_, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version})
	r.ErrorContains(err, "returned status 404")
}

func TestDownloadMaxDownloadSize(t *testing.T) {
	reg, tarball := standard(t, pkg)

	t.Run("tarball too large", func(t *testing.T) {
		r := require.New(t)

		_, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version},
			download.WithMaxDownloadSize(int64(len(tarball)-1)))
		r.ErrorContains(err, "exceeds maximum allowed size")
	})

	t.Run("tarball fits", func(t *testing.T) {
		r := require.New(t)

		b, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version},
			download.WithMaxDownloadSize(int64(len(tarball))))
		r.NoError(err)
		t.Cleanup(func() { r.NoError(b.Close()) })

		r.Equal(tarball, read(t, b))
	})
}

func TestDownloadMaxMetadataSize(t *testing.T) {
	r := require.New(t)

	reg, _ := standard(t, pkg)

	_, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version},
		download.WithMaxMetadataSize(4))
	// an oversized document is fatal rather than a reason to fall back: the
	// packument the fallback would ask for is the larger of the two
	r.ErrorContains(err, "metadata document exceeds the maximum allowed size")
}

func TestDownloadRequiredFields(t *testing.T) {
	for _, tc := range []struct {
		name    string
		req     download.Request
		wantErr string
	}{
		{"no registry", download.Request{Package: pkg, Version: version}, "registry is required"},
		{"no package", download.Request{Registry: "https://registry.npmjs.org", Version: version}, "package is required"},
		{"no version", download.Request{Registry: "https://registry.npmjs.org", Package: pkg}, "version is required"},
		{"registry scheme", download.Request{Registry: "ssh://registry.npmjs.org", Package: pkg, Version: version}, "unsupported registry url scheme"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			_, err := download.Download(t.Context(), tc.req)
			r.ErrorContains(err, tc.wantErr)
		})
	}
}

func TestDownloadWithBasicAuth(t *testing.T) {
	r := require.New(t)

	reg, tarball := standard(t, pkg)
	reg.auth = func(req *http.Request) bool {
		user, password, ok := req.BasicAuth()
		return ok && user == "user" && password == "secret"
	}

	_, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version})
	r.ErrorContains(err, "returned status 401")

	b, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version},
		download.WithCredentials(&credv1.NPMCredentials{
			Type:     credv1.NPMCredentialsVersionedType,
			Username: "user",
			Password: "secret",
		}))
	r.NoError(err)
	t.Cleanup(func() { r.NoError(b.Close()) })

	r.Equal(tarball, read(t, b))
}

func TestDownloadWithToken(t *testing.T) {
	r := require.New(t)

	reg, tarball := standard(t, pkg)
	reg.auth = func(req *http.Request) bool {
		return req.Header.Get("Authorization") == "Bearer npm-token"
	}

	b, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version},
		download.WithCredentials(&credv1.NPMCredentials{
			Type:  credv1.NPMCredentialsVersionedType,
			Token: "npm-token",
		}))
	r.NoError(err)
	t.Cleanup(func() { r.NoError(b.Close()) })

	r.Equal(tarball, read(t, b))
}

// TestDownloadWithGenericCredentials covers a legacy .ocmconfig that configures
// npm credentials as generic Credentials/v1 properties.
func TestDownloadWithGenericCredentials(t *testing.T) {
	r := require.New(t)

	reg, tarball := standard(t, pkg)
	reg.auth = func(req *http.Request) bool {
		user, password, ok := req.BasicAuth()
		return ok && user == "user" && password == "secret"
	}

	raw := &runtime.Raw{}
	r.NoError(raw.UnmarshalJSON([]byte(`{"type":"Credentials/v1","properties":{"username":"user","password":"secret"}}`)))

	b, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version},
		download.WithCredentials(raw))
	r.NoError(err)
	t.Cleanup(func() { r.NoError(b.Close()) })

	r.Equal(tarball, read(t, b))
}

// TestDownloadDoesNotForwardCredentialsToForeignTarballHost guards the case of a
// registry pointing dist.tarball at a host it does not own: the credentials
// configured for the registry must not be sent there.
func TestDownloadDoesNotForwardCredentialsToForeignTarballHost(t *testing.T) {
	r := require.New(t)

	tarball := tgz(t, pkg, version)

	var gotAuth string
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotAuth = req.Header.Get("Authorization")
		_, _ = w.Write(tarball)
	}))
	t.Cleanup(cdn.Close)

	reg := newRegistry(t)
	reg.auth = func(req *http.Request) bool {
		return req.Header.Get("Authorization") == "Bearer npm-token"
	}
	reg.serveJSON("/"+pkg+"/"+version, func() []byte {
		return versionDoc(pkg, version, dist{
			Integrity: integrityOf(tarball),
			Tarball:   cdn.URL + "/" + pkg + "/-/tarball.tgz",
		})
	})

	b, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version},
		download.WithCredentials(&credv1.NPMCredentials{
			Type:  credv1.NPMCredentialsVersionedType,
			Token: "npm-token",
		}))
	r.NoError(err)
	t.Cleanup(func() { r.NoError(b.Close()) })

	r.Equal(tarball, read(t, b))
	r.Empty(gotAuth, "credentials must not be sent to a tarball host other than the registry")
}

// TestDownloadForwardsCredentialsToRegistryTarball is the counterpart: a tarball
// served by the registry itself is fetched with the registry credentials, which
// is what a private Artifactory or Verdaccio requires.
func TestDownloadForwardsCredentialsToRegistryTarball(t *testing.T) {
	r := require.New(t)

	reg, tarball := standard(t, pkg)
	reg.auth = func(req *http.Request) bool {
		return req.Header.Get("Authorization") == "Bearer npm-token"
	}

	b, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version},
		download.WithCredentials(&credv1.NPMCredentials{
			Type:  credv1.NPMCredentialsVersionedType,
			Token: "npm-token",
		}))
	r.NoError(err)
	t.Cleanup(func() { r.NoError(b.Close()) })

	r.Equal(tarball, read(t, b))
	r.Contains(reg.requests, "/"+pkg+"/-/tarball.tgz")
}

func TestDownloadTempDirAndCleanup(t *testing.T) {
	r := require.New(t)

	reg, _ := standard(t, pkg)
	tempDir := t.TempDir()

	b, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version},
		download.WithTempDir(tempDir))
	r.NoError(err)

	entries, err := os.ReadDir(tempDir)
	r.NoError(err)
	r.Len(entries, 1, "the tarball is buffered into the configured temp folder")

	r.NoError(b.Close())

	entries, err = os.ReadDir(tempDir)
	r.NoError(err)
	r.Empty(entries, "closing the blob removes its file")
}

// TestDownloadRemovesFileOnVerificationFailure makes sure a rejected tarball does
// not stay behind in the temp folder.
func TestDownloadRemovesFileOnVerificationFailure(t *testing.T) {
	r := require.New(t)

	reg := newRegistry(t)
	tarball := tgz(t, pkg, version)
	tempDir := t.TempDir()

	reg.serveBytes("/"+pkg+"/-/tarball.tgz", []byte("corrupted"))
	reg.serveJSON("/"+pkg+"/"+version, func() []byte {
		return versionDoc(pkg, version, dist{
			Integrity: integrityOf(tarball),
			Tarball:   reg.URL + "/" + pkg + "/-/tarball.tgz",
		})
	})

	_, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version},
		download.WithTempDir(tempDir))
	r.ErrorContains(err, "mismatch")

	entries, err := os.ReadDir(tempDir)
	r.NoError(err)
	r.Empty(entries)
}

// TestDownloadNexusScopedFallback covers Nexus, which answers the version URL
// with 400 for a scoped package rather than 404
// (https://github.com/sonatype/nexus-public/issues/224). Without a fallback on
// that status a scoped package is unreachable there.
func TestDownloadNexusScopedFallback(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			r := require.New(t)

			reg := newRegistry(t)
			tarball := tgz(t, scoped, version)

			reg.serveBytes("/"+scoped+"/-/tarball.tgz", tarball)
			reg.handle("/"+scoped+"/"+version, func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "not a version endpoint", status)
			})
			reg.serveJSON("/"+scoped, func() []byte {
				return packument(scoped, version, dist{
					Integrity: integrityOf(tarball),
					Tarball:   reg.URL + "/" + scoped + "/-/tarball.tgz",
				})
			})

			b, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: scoped, Version: version})
			r.NoError(err)
			t.Cleanup(func() { r.NoError(b.Close()) })

			r.Equal(tarball, read(t, b))
		})
	}
}

// TestDownloadUnauthorizedDoesNotFallBack keeps a 401 from being reported as the
// packument's error: the packument would answer the same way, and the useful
// message is the one about authentication.
func TestDownloadUnauthorizedDoesNotFallBack(t *testing.T) {
	r := require.New(t)

	reg := newRegistry(t)
	reg.auth = func(*http.Request) bool { return false }

	_, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version})
	r.ErrorContains(err, "version metadata request")
	r.ErrorContains(err, "401")
	r.Len(reg.requests, 1, "a 401 must not trigger the packument fallback")
}

// TestDownloadUndecodableVersionDocumentFallsBack covers a registry behind an
// auth proxy that answers 200 with an HTML login page.
func TestDownloadUndecodableVersionDocumentFallsBack(t *testing.T) {
	r := require.New(t)

	reg := newRegistry(t)
	tarball := tgz(t, pkg, version)

	reg.serveBytes("/"+pkg+"/-/tarball.tgz", tarball)
	reg.handle("/"+pkg+"/"+version, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>please log in</body></html>"))
	})
	reg.serveJSON("/"+pkg, func() []byte {
		return packument(pkg, version, dist{
			Integrity: integrityOf(tarball),
			Tarball:   reg.URL + "/" + pkg + "/-/tarball.tgz",
		})
	})

	b, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version})
	r.NoError(err)
	t.Cleanup(func() { r.NoError(b.Close()) })

	r.Equal(tarball, read(t, b))
}

// TestDownloadRejectsWrongVersionDocument covers the version endpoint resolving a
// dist-tag: the checksums come out of the same document, so a tarball for another
// version would verify and has to be rejected on the version instead.
func TestDownloadRejectsWrongVersionDocument(t *testing.T) {
	r := require.New(t)

	reg := newRegistry(t)
	wanted := tgz(t, pkg, version)
	other := tgz(t, pkg, "99.0.0")

	reg.serveBytes("/"+pkg+"/-/wanted.tgz", wanted)
	reg.serveBytes("/"+pkg+"/-/other.tgz", other)
	// the version endpoint answers with a different version, as it does for a dist-tag
	reg.serveJSON("/"+pkg+"/"+version, func() []byte {
		return versionDoc(pkg, "99.0.0", dist{
			Integrity: integrityOf(other),
			Tarball:   reg.URL + "/" + pkg + "/-/other.tgz",
		})
	})
	reg.serveJSON("/"+pkg, func() []byte {
		return packument(pkg, version, dist{
			Integrity: integrityOf(wanted),
			Tarball:   reg.URL + "/" + pkg + "/-/wanted.tgz",
		})
	})

	b, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version})
	r.NoError(err)
	t.Cleanup(func() { r.NoError(b.Close()) })

	r.Equal(wanted, read(t, b), "the pinned version must win over whatever the version endpoint resolved")
}

// TestDownloadIdentityEncoding guards the tarball request against transparent
// gzip decompression: a tarball is already gzip, so a stripped transfer encoding
// would leave bytes the published checksum does not cover.
func TestDownloadIdentityEncoding(t *testing.T) {
	r := require.New(t)

	reg := newRegistry(t)
	tarball := tgz(t, pkg, version)

	var acceptEncoding string
	reg.handle("/"+pkg+"/-/encoded.tgz", func(w http.ResponseWriter, req *http.Request) {
		acceptEncoding = req.Header.Get("Accept-Encoding")
		// a registry that labels an already-gzip tarball as gzip-encoded
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(tarball)
	})
	reg.serveJSON("/"+pkg+"/"+version, func() []byte {
		return versionDoc(pkg, version, dist{
			Integrity: integrityOf(tarball),
			Tarball:   reg.URL + "/" + pkg + "/-/encoded.tgz",
		})
	})

	b, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version})
	r.NoError(err)
	t.Cleanup(func() { r.NoError(b.Close()) })

	r.Equal("identity", acceptEncoding)
	r.Equal(tarball, read(t, b), "the stored bytes must be the ones the checksum covers")
}

// TestDownloadAbbreviatedPackument checks the documented Accept header is sent,
// so a registry serving the abbreviated document does not hand over megabytes of
// readme prose per download.
func TestDownloadAbbreviatedPackument(t *testing.T) {
	r := require.New(t)

	reg, tarball := standard(t, pkg)

	var accept string
	reg.handle("/"+pkg, func(w http.ResponseWriter, req *http.Request) {
		accept = req.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/vnd.npm.install-v1+json")
		_, _ = w.Write(packument(pkg, version, dist{
			Integrity: integrityOf(tarball),
			Tarball:   reg.URL + "/" + pkg + "/-/tarball.tgz",
		}))
	})

	b, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: "nope"})
	r.Error(err)
	if b != nil {
		t.Cleanup(func() { r.NoError(b.Close()) })
	}

	r.Contains(accept, "application/vnd.npm.install-v1+json")
	r.Contains(accept, "application/json", "an older registry must still be able to answer")
}

func TestDownloadIntegrityAlgorithms(t *testing.T) {
	for _, tc := range []struct {
		name      string
		integrity func(data []byte) string
	}{
		{"sha512", integrityOf},
		{"sha384", func(data []byte) string {
			sum := sha512.Sum384(data)
			return "sha384-" + base64.StdEncoding.EncodeToString(sum[:])
		}},
		{"sha256", func(data []byte) string {
			sum := sha256.Sum256(data)
			return "sha256-" + base64.StdEncoding.EncodeToString(sum[:])
		}},
		{"sha1", func(data []byte) string {
			sum := sha1.Sum(data) //nolint:gosec // G401: mirrors a checksum npm publishes
			return "sha1-" + base64.StdEncoding.EncodeToString(sum[:])
		}},
		// an entry may carry "?"-separated metadata options
		{"sha512 with options", func(data []byte) string { return integrityOf(data) + "?foo=bar" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			reg := newRegistry(t)
			tarball := tgz(t, pkg, version)

			reg.serveBytes("/"+pkg+"/-/tarball.tgz", tarball)
			reg.serveJSON("/"+pkg+"/"+version, func() []byte {
				return versionDoc(pkg, version, dist{
					Integrity: tc.integrity(tarball),
					Tarball:   reg.URL + "/" + pkg + "/-/tarball.tgz",
				})
			})

			b, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version})
			r.NoError(err)
			t.Cleanup(func() { r.NoError(b.Close()) })

			r.Equal(tarball, read(t, b))
		})
	}
}

// TestDownloadStrongestIntegrityWins mirrors npm's pickAlgorithm: with digests in
// several algorithms the strongest is verified, and a stale weak one beside it
// does not fail a package npm installs fine.
func TestDownloadStrongestIntegrityWins(t *testing.T) {
	r := require.New(t)

	reg := newRegistry(t)
	tarball := tgz(t, pkg, version)
	staleSHA1 := sha1.Sum([]byte("a digest from an older publish")) //nolint:gosec // G401: mirrors a checksum npm publishes

	reg.serveBytes("/"+pkg+"/-/tarball.tgz", tarball)
	reg.serveJSON("/"+pkg+"/"+version, func() []byte {
		return versionDoc(pkg, version, dist{
			Integrity: "sha1-" + base64.StdEncoding.EncodeToString(staleSHA1[:]) + " " + integrityOf(tarball),
			Tarball:   reg.URL + "/" + pkg + "/-/tarball.tgz",
		})
	})

	b, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version})
	r.NoError(err)
	t.Cleanup(func() { r.NoError(b.Close()) })

	r.Equal(tarball, read(t, b))
}

// TestDownloadAnyDigestOfTheChosenAlgorithmPasses covers ssri semantics: several
// digests may be published for one algorithm and any of them is acceptable.
func TestDownloadAnyDigestOfTheChosenAlgorithmPasses(t *testing.T) {
	r := require.New(t)

	reg := newRegistry(t)
	tarball := tgz(t, pkg, version)

	reg.serveBytes("/"+pkg+"/-/tarball.tgz", tarball)
	reg.serveJSON("/"+pkg+"/"+version, func() []byte {
		return versionDoc(pkg, version, dist{
			Integrity: integrityOf([]byte("another artifact")) + " " + integrityOf(tarball),
			Tarball:   reg.URL + "/" + pkg + "/-/tarball.tgz",
		})
	})

	b, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version})
	r.NoError(err)
	t.Cleanup(func() { r.NoError(b.Close()) })

	r.Equal(tarball, read(t, b))
}

// TestDownloadUnusableIntegrityIsFatal makes sure an integrity string in an
// algorithm this version cannot compute fails the download instead of silently
// storing unverified content.
func TestDownloadUnusableIntegrityIsFatal(t *testing.T) {
	r := require.New(t)

	reg := newRegistry(t)
	tarball := tgz(t, pkg, version)

	reg.serveBytes("/"+pkg+"/-/tarball.tgz", tarball)
	reg.serveJSON("/"+pkg+"/"+version, func() []byte {
		return versionDoc(pkg, version, dist{
			Integrity: "sha3512-" + base64.StdEncoding.EncodeToString([]byte("future algorithm")),
			Tarball:   reg.URL + "/" + pkg + "/-/tarball.tgz",
		})
	})

	_, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version})
	r.ErrorContains(err, "dist.integrity carries no known algorithm")
}

func TestDownloadRejectsShortShasum(t *testing.T) {
	r := require.New(t)

	reg := newRegistry(t)
	tarball := tgz(t, pkg, version)

	reg.serveBytes("/"+pkg+"/-/tarball.tgz", tarball)
	reg.serveJSON("/"+pkg+"/"+version, func() []byte {
		// a registry publishing a SHA-256 in the SHA-1 field
		sum := sha256.Sum256(tarball)
		return versionDoc(pkg, version, dist{
			Shasum:  hex.EncodeToString(sum[:]),
			Tarball: reg.URL + "/" + pkg + "/-/tarball.tgz",
		})
	})

	_, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version})
	r.ErrorContains(err, "not a 40-character SHA-1")
}

// TestDownloadRelativeTarball covers a registry that publishes dist.tarball as a
// path rather than an absolute URL.
func TestDownloadRelativeTarball(t *testing.T) {
	r := require.New(t)

	reg := newRegistry(t)
	tarball := tgz(t, pkg, version)

	reg.serveBytes("/"+pkg+"/-/tarball.tgz", tarball)
	reg.serveJSON("/"+pkg+"/"+version, func() []byte {
		return versionDoc(pkg, version, dist{
			Integrity: integrityOf(tarball),
			Tarball:   "/" + pkg + "/-/tarball.tgz",
		})
	})

	b, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version})
	r.NoError(err)
	t.Cleanup(func() { r.NoError(b.Close()) })

	r.Equal(tarball, read(t, b))
}

// TestDownloadBasicAuthWinsOverToken covers an OCM v1 configuration carrying a
// user name, a password and a token left over from a login: v1 read with basic
// auth, so honouring the token instead would turn a working setup into a 401.
func TestDownloadBasicAuthWinsOverToken(t *testing.T) {
	r := require.New(t)

	reg, tarball := standard(t, pkg)
	reg.auth = func(req *http.Request) bool {
		user, password, ok := req.BasicAuth()
		return ok && user == "user" && password == "secret"
	}

	b, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version},
		download.WithCredentials(&credv1.NPMCredentials{
			Type:     credv1.NPMCredentialsVersionedType,
			Username: "user",
			Password: "secret",
			Token:    "a-stale-token",
		}))
	r.NoError(err)
	t.Cleanup(func() { r.NoError(b.Close()) })

	r.Equal(tarball, read(t, b))
}

func TestDownloadUsernameWithoutPassword(t *testing.T) {
	r := require.New(t)

	reg, _ := standard(t, pkg)

	_, err := download.Download(t.Context(), download.Request{Registry: reg.URL, Package: pkg, Version: version},
		download.WithCredentials(&credv1.NPMCredentials{
			Type:     credv1.NPMCredentialsVersionedType,
			Username: "user",
		}))
	r.ErrorContains(err, "carry no password")
}

// TestDownloadRefusesCredentialsOverDowngrade guards the scheme half of the
// same-host rule: Go drops the Authorization header on a redirect to another host
// but not on one that only drops TLS, so the redirect has to be refused here.
func TestDownloadRefusesCredentialsOverDowngrade(t *testing.T) {
	r := require.New(t)

	reg, _ := standard(t, pkg)

	// an https registry whose metadata request is redirected to plain http
	tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, reg.URL+req.URL.Path, http.StatusFound)
	}))
	t.Cleanup(tls.Close)

	_, err := download.Download(t.Context(), download.Request{Registry: tls.URL, Package: pkg, Version: version},
		download.WithClient(tls.Client()),
		download.WithCredentials(&credv1.NPMCredentials{
			Type:  credv1.NPMCredentialsVersionedType,
			Token: "npm-token",
		}))
	r.ErrorContains(err, "refusing to follow a redirect from https to http")
}
