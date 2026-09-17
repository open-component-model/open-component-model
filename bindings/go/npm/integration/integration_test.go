// Package integration exercises the npm resource repository against a real npm
// registry (Verdaccio) started as a container.
//
// The registry is configured without uplinks, so nothing reaches npmjs.com, and
// it requires authentication for every package except the "public-" prefix. That
// covers anonymous access, basic auth and token auth against a real registry.
// Checksum-mismatch handling is covered by the unit tests, which can serve a
// corrupted tarball; a real registry always serves what it stored.
package integration_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/npm/repository"
	accessv1 "ocm.software/open-component-model/bindings/go/npm/spec/access/v1"
	credv1 "ocm.software/open-component-model/bindings/go/npm/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	verdaccioImage = "verdaccio/verdaccio:6"
	verdaccioPort  = "4873/tcp"

	user     = "ocm"
	password = "ocm-integration-password"
	email    = "ocm@example.com"

	version = "1.2.3"
)

// verdaccioConfig requires authentication for every package but the "public-"
// prefix, and defines no uplinks so the registry never proxies to npmjs.com.
const verdaccioConfig = `storage: /verdaccio/storage/data
auth:
  htpasswd:
    file: /verdaccio/storage/htpasswd
    max_users: 100
packages:
  'public-*':
    access: $all
    publish: $authenticated
  '**':
    access: $authenticated
    publish: $authenticated
log:
  type: stdout
  format: pretty
  level: warn
`

// registry is a running Verdaccio with a publishing token.
type registry struct {
	url   string
	token string
}

func startRegistry(t *testing.T, ctx context.Context) *registry {
	t.Helper()
	r := require.New(t)

	container, err := testcontainers.Run(ctx, verdaccioImage,
		testcontainers.WithExposedPorts(verdaccioPort),
		testcontainers.WithFiles(testcontainers.ContainerFile{
			Reader:            strings.NewReader(verdaccioConfig),
			ContainerFilePath: "/verdaccio/conf/config.yaml",
			FileMode:          0o644,
		}),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/-/ping").WithPort(verdaccioPort).WithStartupTimeout(2*time.Minute),
		),
	)
	testcontainers.CleanupContainer(t, container)
	r.NoError(err, "start verdaccio container")

	endpoint, err := container.PortEndpoint(ctx, verdaccioPort, "http")
	r.NoError(err, "resolve verdaccio endpoint")

	reg := &registry{url: endpoint}
	reg.token = reg.createUser(t, ctx)

	return reg
}

// createUser registers the publishing user and returns its bearer token, the
// same call "npm adduser" makes.
func (reg *registry) createUser(t *testing.T, ctx context.Context) string {
	t.Helper()
	r := require.New(t)

	body, err := json.Marshal(map[string]string{"name": user, "password": password, "email": email})
	r.NoError(err)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		reg.url+"/-/user/org.couchdb.user:"+user, bytes.NewReader(body))
	r.NoError(err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	r.NoError(err)
	defer func() { r.NoError(resp.Body.Close()) }()

	payload, err := io.ReadAll(resp.Body)
	r.NoError(err)
	r.Contains([]int{http.StatusCreated, http.StatusConflict, http.StatusOK}, resp.StatusCode,
		"create user: %s", payload)

	var created struct {
		Token string `json:"token"`
	}
	r.NoError(json.Unmarshal(payload, &created))
	r.NotEmpty(created.Token, "verdaccio returned no token")

	return created.Token
}

// tgz builds a package tarball containing a single package.json.
func tgz(t *testing.T, name string) []byte {
	t.Helper()
	r := require.New(t)

	manifest := fmt.Sprintf(`{"name":%q,"version":%q}`, name, version)

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	r.NoError(tw.WriteHeader(&tar.Header{Name: "package/package.json", Mode: 0o644, Size: int64(len(manifest))}))
	_, err := tw.Write([]byte(manifest))
	r.NoError(err)
	r.NoError(tw.Close())
	r.NoError(gz.Close())

	return buf.Bytes()
}

// publish uploads a package the way "npm publish" does: one PUT carrying the
// packument and the tarball as a base64 attachment.
func (reg *registry) publish(t *testing.T, ctx context.Context, name string) []byte {
	t.Helper()
	r := require.New(t)

	tarball := tgz(t, name)
	filename := strings.ReplaceAll(strings.TrimPrefix(name, "@"), "/", "-") + "-" + version + ".tgz"
	shasum := sha1.Sum(tarball) //nolint:gosec // G401: dist.shasum is a SHA-1 by npm's definition
	integrity := sha512.Sum512(tarball)

	body, err := json.Marshal(map[string]any{
		"_id":       name,
		"name":      name,
		"dist-tags": map[string]string{"latest": version},
		"versions": map[string]any{
			version: map[string]any{
				"name":    name,
				"version": version,
				"dist": map[string]string{
					"shasum":    hex.EncodeToString(shasum[:]),
					"integrity": "sha512-" + base64.StdEncoding.EncodeToString(integrity[:]),
					"tarball":   reg.url + "/" + name + "/-/" + filename,
				},
			},
		},
		"_attachments": map[string]any{
			filename: map[string]any{
				"content_type": "application/octet-stream",
				"data":         base64.StdEncoding.EncodeToString(tarball),
				"length":       len(tarball),
			},
		},
	})
	r.NoError(err)

	// A scoped name is escaped in the publish URL, as the npm client does.
	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		reg.url+"/"+url.PathEscape(name), bytes.NewReader(body))
	r.NoError(err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+reg.token)

	resp, err := http.DefaultClient.Do(req)
	r.NoError(err)
	defer func() { r.NoError(resp.Body.Close()) }()

	payload, err := io.ReadAll(resp.Body)
	r.NoError(err)
	r.Contains([]int{http.StatusOK, http.StatusCreated}, resp.StatusCode, "publish %s: %s", name, payload)

	return tarball
}

func resource(t *testing.T, registryURL, name string, versions ...string) *descriptor.Resource {
	t.Helper()
	r := require.New(t)

	ver := version
	if len(versions) > 0 {
		ver = versions[0]
	}

	access := &accessv1.NPM{
		Type:     runtime.NewVersionedType(accessv1.Type, accessv1.Version),
		Registry: registryURL,
		Package:  name,
		Version:  ver,
	}
	r.NoError(access.Validate())

	data, err := json.Marshal(access)
	r.NoError(err)
	raw := &runtime.Raw{}
	r.NoError(raw.UnmarshalJSON(data))

	return &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "package", Version: ver}},
		Type:        "npmPackage",
		Access:      raw,
	}
}

// Test_Integration_NPM downloads packages from a real registry, anonymously and
// with both supported credential kinds.
func Test_Integration_NPM(t *testing.T) {
	ctx := context.Background()
	reg := startRegistry(t, ctx)

	publicTarball := reg.publish(t, ctx, "public-lib")
	privateTarball := reg.publish(t, ctx, "private-lib")
	scopedTarball := reg.publish(t, ctx, "@ocm/scoped-lib")

	tempDir := t.TempDir()
	repo := repository.NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &tempDir})

	download := func(t *testing.T, name string, credentials runtime.Typed) ([]byte, error) {
		t.Helper()

		b, err := repo.DownloadResource(t.Context(), resource(t, reg.url, name), credentials)
		if err != nil {
			return nil, err
		}

		rc, err := b.ReadCloser()
		require.NoError(t, err)
		defer func() { require.NoError(t, rc.Close()) }()

		return io.ReadAll(rc)
	}

	token := &credv1.NPMCredentials{Type: credv1.NPMCredentialsVersionedType, Token: reg.token}
	basic := &credv1.NPMCredentials{Type: credv1.NPMCredentialsVersionedType, Username: user, Password: password}

	t.Run("anonymous", func(t *testing.T) {
		r := require.New(t)

		got, err := download(t, "public-lib", nil)
		r.NoError(err)
		r.Equal(publicTarball, got)
	})

	t.Run("token", func(t *testing.T) {
		r := require.New(t)

		got, err := download(t, "private-lib", token)
		r.NoError(err)
		r.Equal(privateTarball, got)
	})

	t.Run("basic auth", func(t *testing.T) {
		r := require.New(t)

		got, err := download(t, "private-lib", basic)
		r.NoError(err)
		r.Equal(privateTarball, got)
	})

	t.Run("scoped package", func(t *testing.T) {
		r := require.New(t)

		got, err := download(t, "@ocm/scoped-lib", token)
		r.NoError(err)
		r.Equal(scopedTarball, got)
	})

	t.Run("without credentials", func(t *testing.T) {
		r := require.New(t)

		_, err := download(t, "private-lib", nil)
		r.Error(err, "a package that requires authentication must not be downloadable anonymously")
	})

	t.Run("unknown version", func(t *testing.T) {
		r := require.New(t)

		_, err := repo.DownloadResource(t.Context(), resource(t, reg.url, "public-lib", "9.9.9"), nil)
		r.ErrorContains(err, "not found")
	})

	t.Run("digest", func(t *testing.T) {
		r := require.New(t)

		res, err := repo.ProcessResourceDigest(t.Context(), resource(t, reg.url, "public-lib"), nil)
		r.NoError(err)
		r.NotNil(res.Digest)
		r.Equal("SHA-256", res.Digest.HashAlgorithm)
		r.Equal("genericBlobDigest/v1", res.Digest.NormalisationAlgorithm)
	})
}
