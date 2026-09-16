package repository_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/npm/repository"
	accessv1 "ocm.software/open-component-model/bindings/go/npm/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	pkg     = "yargs"
	version = "17.7.2"
)

// tgz builds a gzipped tar holding a single package.json.
func tgz(t *testing.T) []byte {
	t.Helper()

	manifest := fmt.Sprintf(`{"name":%q,"version":%q}`, pkg, version)

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "package/package.json", Mode: 0o644, Size: int64(len(manifest))}))
	_, err := tw.Write([]byte(manifest))
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())

	return buf.Bytes()
}

// newRegistry serves the version document and tarball of a single package.
func newRegistry(t *testing.T) (string, []byte) {
	t.Helper()

	tarball := tgz(t)
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	sum := sha512.Sum512(tarball)
	body, err := json.Marshal(map[string]any{
		"name":    pkg,
		"version": version,
		"dist": map[string]string{
			"integrity": "sha512-" + base64.StdEncoding.EncodeToString(sum[:]),
			"tarball":   srv.URL + "/" + pkg + "/-/tarball.tgz",
		},
	})
	require.NoError(t, err)

	mux.HandleFunc("/"+pkg+"/"+version, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	})
	mux.HandleFunc("/"+pkg+"/-/tarball.tgz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(tarball)
	})

	return srv.URL, tarball
}

func resource(t *testing.T, access *accessv1.NPM) *descriptor.Resource {
	t.Helper()

	raw := &runtime.Raw{}
	data, err := json.Marshal(access)
	require.NoError(t, err)
	require.NoError(t, raw.UnmarshalJSON(data))

	return &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: pkg, Version: version}},
		Type:        "npmPackage",
		Access:      raw,
	}
}

func npmAccess(registry, name, ver string) *accessv1.NPM {
	return &accessv1.NPM{
		Type:     runtime.NewVersionedType(accessv1.Type, accessv1.Version),
		Registry: registry,
		Package:  name,
		Version:  ver,
	}
}

func newRepository(t *testing.T) *repository.ResourceRepository {
	t.Helper()

	tempDir := t.TempDir()
	return repository.NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &tempDir})
}

func TestDownloadResource(t *testing.T) {
	r := require.New(t)

	registryURL, tarball := newRegistry(t)
	repo := newRepository(t)

	b, err := repo.DownloadResource(t.Context(), resource(t, npmAccess(registryURL, pkg, version)), nil)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(b.(io.Closer).Close()) })

	rc, err := b.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(rc.Close()) }()

	data, err := io.ReadAll(rc)
	r.NoError(err)
	r.Equal(tarball, data)

	mediaType, ok := b.(blob.MediaTypeAware).MediaType()
	r.True(ok)
	r.Equal("application/x-tgz", mediaType)
}

func TestGetResourceCredentialConsumerIdentity(t *testing.T) {
	r := require.New(t)

	repo := newRepository(t)

	identity, err := repo.GetResourceCredentialConsumerIdentity(t.Context(),
		resource(t, npmAccess("https://registry.npmjs.org", "@types/node", "20.11.5")))
	r.NoError(err)
	r.Equal("NpmRegistry", identity["type"])
	r.Equal("registry.npmjs.org", identity["hostname"])
	r.Equal("https", identity["scheme"])
	r.Equal("@types/node", identity["path"])

	// the digest processor resolves credentials through the same identity
	digestIdentity, err := repo.GetResourceDigestProcessorCredentialConsumerIdentity(t.Context(),
		resource(t, npmAccess("https://registry.npmjs.org", "@types/node", "20.11.5")))
	r.NoError(err)
	r.Equal(identity, digestIdentity)
}

func TestInvalidResources(t *testing.T) {
	repo := newRepository(t)

	t.Run("nil resource", func(t *testing.T) {
		r := require.New(t)

		_, err := repo.DownloadResource(t.Context(), nil, nil)
		r.ErrorContains(err, "resource is required")
	})

	t.Run("no access", func(t *testing.T) {
		r := require.New(t)

		_, err := repo.DownloadResource(t.Context(), &descriptor.Resource{}, nil)
		r.ErrorContains(err, "resource access is required")
	})

	t.Run("invalid access spec", func(t *testing.T) {
		r := require.New(t)

		_, err := repo.DownloadResource(t.Context(), resource(t, npmAccess("https://registry.npmjs.org", pkg, "latest")), nil)
		r.ErrorContains(err, "invalid npm access spec")
	})
}

func TestUploadResourceIsNotSupported(t *testing.T) {
	r := require.New(t)

	repo := newRepository(t)

	_, err := repo.UploadResource(t.Context(), resource(t, npmAccess("https://registry.npmjs.org", pkg, version)), nil, nil)
	r.ErrorContains(err, "upload is not supported")
}

func TestProcessResourceDigest(t *testing.T) {
	registryURL, tarball := newRegistry(t)
	repo := newRepository(t)

	want := sha256.Sum256(tarball)
	wantHex := hex.EncodeToString(want[:])

	t.Run("computes the digest", func(t *testing.T) {
		r := require.New(t)

		res, err := repo.ProcessResourceDigest(t.Context(), resource(t, npmAccess(registryURL, pkg, version)), nil)
		r.NoError(err)
		r.NotNil(res.Digest)
		r.Equal("SHA-256", res.Digest.HashAlgorithm)
		r.Equal("genericBlobDigest/v1", res.Digest.NormalisationAlgorithm)
		r.Equal(wantHex, res.Digest.Value)
	})

	t.Run("verifies a matching digest", func(t *testing.T) {
		r := require.New(t)

		res := resource(t, npmAccess(registryURL, pkg, version))
		res.Digest = &descriptor.Digest{
			HashAlgorithm:          "SHA-256",
			NormalisationAlgorithm: "genericBlobDigest/v1",
			Value:                  wantHex,
		}

		got, err := repo.ProcessResourceDigest(t.Context(), res, nil)
		r.NoError(err)
		r.Equal(wantHex, got.Digest.Value)
	})

	t.Run("rejects a mismatching digest", func(t *testing.T) {
		r := require.New(t)

		res := resource(t, npmAccess(registryURL, pkg, version))
		res.Digest = &descriptor.Digest{
			HashAlgorithm:          "SHA-256",
			NormalisationAlgorithm: "genericBlobDigest/v1",
			Value:                  "0000000000000000000000000000000000000000000000000000000000000000",
		}

		_, err := repo.ProcessResourceDigest(t.Context(), res, nil)
		r.ErrorContains(err, "digest value mismatch")
	})

	t.Run("rejects another hash algorithm", func(t *testing.T) {
		r := require.New(t)

		res := resource(t, npmAccess(registryURL, pkg, version))
		res.Digest = &descriptor.Digest{
			HashAlgorithm:          "SHA-512",
			NormalisationAlgorithm: "genericBlobDigest/v1",
			Value:                  wantHex,
		}

		_, err := repo.ProcessResourceDigest(t.Context(), res, nil)
		r.ErrorContains(err, "hash algorithm mismatch")
	})

	t.Run("accepts a digest that only states the value", func(t *testing.T) {
		r := require.New(t)

		res := resource(t, npmAccess(registryURL, pkg, version))
		res.Digest = &descriptor.Digest{Value: wantHex}

		got, err := repo.ProcessResourceDigest(t.Context(), res, nil)
		r.NoError(err)
		// the accepted spellings are canonicalised so descriptors do not vary by author
		r.Equal("SHA-256", got.Digest.HashAlgorithm)
		r.Equal("genericBlobDigest/v1", got.Digest.NormalisationAlgorithm)
		r.Equal(wantHex, got.Digest.Value)
	})

	t.Run("accepts another spelling of the algorithms and value", func(t *testing.T) {
		r := require.New(t)

		res := resource(t, npmAccess(registryURL, pkg, version))
		res.Digest = &descriptor.Digest{
			HashAlgorithm:          "sha-256",
			NormalisationAlgorithm: "genericblobdigest/v1",
			Value:                  strings.ToUpper(wantHex),
		}

		got, err := repo.ProcessResourceDigest(t.Context(), res, nil)
		r.NoError(err)
		r.Equal("SHA-256", got.Digest.HashAlgorithm)
		r.Equal("genericBlobDigest/v1", got.Digest.NormalisationAlgorithm)
		r.Equal(wantHex, got.Digest.Value)
	})

	t.Run("rejects another normalisation algorithm", func(t *testing.T) {
		r := require.New(t)

		res := resource(t, npmAccess(registryURL, pkg, version))
		res.Digest = &descriptor.Digest{
			HashAlgorithm:          "SHA-256",
			NormalisationAlgorithm: "ociArtifactDigest/v1",
			Value:                  wantHex,
		}

		_, err := repo.ProcessResourceDigest(t.Context(), res, nil)
		r.ErrorContains(err, "normalisation algorithm mismatch")
	})
}

func TestGetResourceRepositoryScheme(t *testing.T) {
	r := require.New(t)

	repo := newRepository(t)

	// every type name the OCM v1 npm access type registered must resolve
	for _, typ := range []string{"NPM/v1", "NPM", "npm", "npm/v1"} {
		parsed, err := runtime.TypeFromString(typ)
		r.NoError(err)

		obj, err := repo.GetResourceRepositoryScheme().NewObject(parsed)
		r.NoError(err)
		r.IsType(&accessv1.NPM{}, obj)
	}
}
