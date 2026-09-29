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
	"os"
	"strings"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/registry"
	"oras.land/oras-go/v2/content/memory"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	helmaccess "ocm.software/open-component-model/bindings/go/helm/spec/access"
	helmaccessv1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	helmidentityv1 "ocm.software/open-component-model/bindings/go/helm/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	ocitar "ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/artifactory/artifactorytest"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/uploadtest"
	uploadv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/transformation/spec/v1alpha1"
	wgetaccess "ocm.software/open-component-model/bindings/go/wget/spec/access"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
	wgetcredsv1 "ocm.software/open-component-model/bindings/go/wget/spec/credentials/v1"
	wgetidentityv1 "ocm.software/open-component-model/bindings/go/wget/spec/identity/v1"
)

// localChartRepo serves the content as a local resource and records the request.
type localChartRepo struct {
	repository.ComponentVersionRepository
	content            []byte
	component, version string
	identity           runtime.Identity
}

func (l *localChartRepo) GetLocalResource(_ context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	l.component, l.version, l.identity = component, version, identity
	return inmemory.New(bytes.NewReader(l.content), inmemory.WithSize(int64(len(l.content)))), nil, nil
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

// helmChartLayout returns the OCI layout of a helm chart OCI artifact holding chart, as
// resource repositories hand out OCI artifacts.
func helmChartLayout(t *testing.T, chart []byte) []byte {
	t.Helper()
	r := require.New(t)
	store := memory.New()
	push := func(mediaType string, data []byte) ocispec.Descriptor {
		desc := ocispec.Descriptor{MediaType: mediaType, Digest: digest.FromBytes(data), Size: int64(len(data))}
		r.NoError(store.Push(t.Context(), desc, bytes.NewReader(data)))
		return desc
	}
	manifest, err := json.Marshal(ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    push(registry.ConfigMediaType, []byte(`{"name":"mychart","version":"0.1.0","apiVersion":"v2"}`)),
		Layers:    []ocispec.Descriptor{push(registry.ChartLayerMediaType, chart)},
	})
	r.NoError(err)
	b, err := ocitar.CopyToOCILayoutInMemory(t.Context(), store, push(ocispec.MediaTypeImageManifest, manifest), ocitar.CopyToOCILayoutOptions{})
	r.NoError(err)
	rc, err := b.ReadCloser()
	r.NoError(err)
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	r.NoError(err)
	return data
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// uploadRun is what a TestTransform row checks beyond its columns.
type uploadRun struct {
	repo  *artifactorytest.Repository
	reqs  []artifactorytest.Request // of the last transfer
	out   *descriptorv2.Resource
	local *localChartRepo
}

func (u uploadRun) bodyPUTs() []artifactorytest.Request {
	var puts []artifactorytest.Request
	for _, req := range u.reqs {
		if req.Method == http.MethodPut && !req.Deploy {
			puts = append(puts, req)
		}
	}
	return puts
}

func TestTransform(t *testing.T) {
	chartTGZ, err := os.ReadFile("../../../../helm/testdata/mychart-0.1.0.tgz")
	require.NoError(t, err)
	chartDigest := sha256Hex(chartTGZ)
	notAChart := []byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xff, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	hello := []byte("hello")
	helloDigest := sha256Hex(hello)
	chartLayout := helmChartLayout(t, chartTGZ)

	scheme := runtime.NewScheme()
	scheme.MustRegisterScheme(uploadv1alpha1.Scheme)
	scheme.MustRegisterScheme(helmaccess.Scheme)
	scheme.MustRegisterScheme(wgetaccess.Scheme)

	const (
		file          = "ocm.software/test/1.0.0/renamed-9.9.9"
		detect        = "GET /artifactory/api/repositories/helm-local"
		fileInfo      = "GET /artifactory/api/storage/helm-local/" + file
		put           = "PUT /artifactory/helm-local/" + file
		chartProps    = "?properties=chart.name,chart.version"
		npmProps      = "?properties=npm.name,npm.version"
		ownerProps    = "?properties=ocm.component.name,ocm.component.version,ocm.resource.name,ocm.resource.version,ocm.resource.extraIdentity"
		invalidDigest = "0000000000000000000000000000000000000000000000000000000000000000"
	)
	owner := map[string]string{
		"ocm.component.name": "ocm.software/test", "ocm.component.version": "1.0.0",
		"ocm.resource.name": "renamed", "ocm.resource.version": "9.9.9",
	}
	withDigest := func(value, normalisation string, hashAlgorithm ...string) func(*descriptorv2.Resource) {
		return func(res *descriptorv2.Resource) {
			res.Digest = &descriptorv2.Digest{HashAlgorithm: append(hashAlgorithm, "SHA-256")[0], NormalisationAlgorithm: normalisation, Value: value}
		}
	}
	helmCreds := &helmcredsv1.HelmHTTPCredentials{Type: runtime.NewVersionedType(helmcredsv1.HelmHTTPCredentialsType, helmcredsv1.Version), Username: "helm-user", Password: "helm-pass"}
	certCreds := &helmcredsv1.HelmHTTPCredentials{Type: runtime.NewVersionedType(helmcredsv1.HelmHTTPCredentialsType, helmcredsv1.Version), CertFile: "/cert.pem", KeyFile: "/key.pem"}
	wgetCreds := &wgetcredsv1.WgetCredentials{Type: wgetcredsv1.WgetCredentialsVersionedType, IdentityToken: "wget-token"}
	helmType, wgetType := helmidentityv1.Type.String(), wgetidentityv1.Type.String()
	auth := func(basic []string, bearer string) func(*require.Assertions, uploadRun) {
		return func(r *require.Assertions, u uploadRun) {
			r.Len(u.reqs, 4, "detection, file info, upload and chart properties")
			for _, req := range u.reqs {
				if basic != nil {
					r.True(req.Basic, "%s %s must use basic auth", req.Method, req.Path)
					r.Equal(basic, []string{req.Username, req.Password})
				} else {
					r.Equal("Bearer "+bearer, req.Authorization)
				}
			}
		}
	}
	nothingWritten := func(r *require.Assertions, u uploadRun) { r.Empty(u.bodyPUTs(), "nothing may be written") }
	storedElsewhere := func(path string) func(*artifactorytest.Repository) {
		return func(repo *artifactorytest.Repository) {
			repo.StoredPath = func(string) string { return path }
		}
	}

	type access struct {
		helmChart string // Helm/v1 access in the Artifactory Helm API
		url       string // else Wget/v1 access on srv.URL+url
		mediaType string
	}
	tests := []struct {
		name     string
		repoType string // default helm
		// content is served by the source repository (default chartTGZ) with mediaType.
		content   []byte
		mediaType string
		resource  func(*descriptorv2.Resource)
		local     bool // local blob read from the source component version
		path      string
		creds     uploadtest.Credentials
		seed      func(*artifactorytest.Repository)
		transfers int // requests and output of the last transfer are checked; default 1
		wantErr   string
		// wantAccess and wantDigest (genericBlobDigest/v1 value) describe the published resource.
		wantAccess   access
		wantDigest   string
		wantRequests []string // method, path and query, where the request order is the behavior
		check        func(*require.Assertions, uploadRun)
	}{
		{
			name:         "helm stores the chart under the component version and publishes the name artifactory records",
			wantAccess:   access{helmChart: "mychart:0.1.0"},
			wantDigest:   chartDigest,
			wantRequests: []string{detect, fileInfo + ".tgz", put + ".tgz", fileInfo + ".tgz" + chartProps},
			check: func(r *require.Assertions, u uploadRun) {
				r.Equal("application/gzip", u.reqs[2].ContentType)
				r.Equal(chartTGZ, u.reqs[2].Body)
				r.Empty(u.reqs[2].Checksum, "without a source digest there is no checksum to announce")
				r.Equal(owner, u.reqs[2].Properties, "the deploy records the owning resource as properties")
			},
		},
		{
			name:    "helm content artifactory does not recognize as a chart is deleted and fails",
			content: notAChart,
			wantErr: "content of resource name=renamed,version=9.9.9 is not a helm chart: artifactory recorded no chart name and version for {url}/artifactory/helm-local/" + file + ".tgz",
			check: func(r *require.Assertions, u uploadRun) {
				r.Len(u.reqs, 4+repositoryupload.PollAttempts, "detection, file info, upload, properties polled, then delete")
				r.Equal(http.MethodDelete, u.reqs[len(u.reqs)-1].Method)
				r.False(u.repo.Stored(file + ".tgz"))
			},
		},
		{name: "HelmChartRepository HelmHTTPCredentials use basic auth", creds: uploadtest.Credentials{helmType: helmCreds}, check: auth([]string{"helm-user", "helm-pass"}, "")},
		{name: "Wget WgetCredentials use a bearer token", creds: uploadtest.Credentials{wgetType: wgetCreds}, check: auth(nil, "wget-token")},
		{name: "HelmChartRepository credentials win over Wget", creds: uploadtest.Credentials{helmType: helmCreds, wgetType: wgetCreds}, check: auth([]string{"helm-user", "helm-pass"}, "")},
		{
			name:    "HelmHTTPCredentials client certificates are rejected before any request",
			creds:   uploadtest.Credentials{helmType: certCreds},
			wantErr: "HelmHTTPCredentials certFile/keyFile are not supported for repository uploads",
			check:   func(r *require.Assertions, u uploadRun) { r.Empty(u.reqs) },
		},
		{
			name:         "source digest is announced as checksum after deploy by checksum was tried",
			resource:     withDigest(chartDigest, "genericBlobDigest/v1"),
			wantAccess:   access{helmChart: "mychart:0.1.0"},
			wantDigest:   chartDigest,
			wantRequests: []string{detect, fileInfo + ".tgz", put + ".tgz", put + ".tgz", fileInfo + ".tgz" + chartProps},
			check: func(r *require.Assertions, u uploadRun) {
				r.True(u.reqs[2].Deploy)
				r.Empty(u.reqs[2].Body)
				r.Equal(chartDigest, u.reqs[3].Checksum)
				r.Equal(chartTGZ, u.reqs[3].Body)
			},
		},
		{
			name:         "re-transfer with a source digest writes nothing",
			resource:     withDigest(chartDigest, "genericBlobDigest/v1"),
			transfers:    2,
			wantAccess:   access{helmChart: "mychart:0.1.0"},
			wantDigest:   chartDigest,
			wantRequests: []string{detect, fileInfo + ".tgz", fileInfo + ".tgz" + chartProps},
		},
		{
			name:         "re-transfer of the same resource replaces its earlier upload",
			transfers:    2,
			wantRequests: []string{detect, fileInfo + ".tgz", fileInfo + ".tgz" + ownerProps, put + ".tgz", fileInfo + ".tgz" + chartProps},
		},
		{
			name:         "source digest mismatch is rejected by artifactory",
			resource:     withDigest(invalidDigest, "genericBlobDigest/v1"),
			wantErr:      "returned status 409",
			wantRequests: []string{detect, fileInfo + ".tgz", put + ".tgz", put + ".tgz"},
			check:        func(r *require.Assertions, u uploadRun) { r.False(u.repo.Stored(file + ".tgz")) },
		},
		{
			name:         "a SHA-512 source digest is verified after the upload",
			resource:     withDigest(digest.SHA512.FromBytes(chartTGZ).Encoded(), "genericBlobDigest/v1", "SHA-512"),
			wantAccess:   access{helmChart: "mychart:0.1.0"},
			wantRequests: []string{detect, fileInfo + ".tgz", put + ".tgz", fileInfo + ".tgz" + chartProps},
			check: func(r *require.Assertions, u uploadRun) {
				r.Empty(u.reqs[2].Checksum, "artifactory only verifies SHA-256 checksums")
				r.Equal(&descriptorv2.Digest{HashAlgorithm: "SHA-512", NormalisationAlgorithm: "genericBlobDigest/v1", Value: digest.SHA512.FromBytes(chartTGZ).Encoded()}, u.out.Digest)
			},
		},
		{
			name:     "a SHA-512 source digest mismatch fails after the upload and removes the file",
			resource: withDigest(digest.SHA512.FromBytes(hello).Encoded(), "genericBlobDigest/v1", "SHA-512"),
			wantErr:  "digest mismatch: expected sha512:" + digest.SHA512.FromBytes(hello).Encoded() + ", got sha512:" + digest.SHA512.FromBytes(chartTGZ).Encoded(),
			check:    func(r *require.Assertions, u uploadRun) { r.False(u.repo.Stored(file + ".tgz")) },
		},
		{
			name:         "invalid source digest fails before uploading",
			resource:     withDigest("0000", "genericBlobDigest/v1", "SHA-256"),
			wantErr:      "invalid source digest",
			wantRequests: []string{detect},
		},
		{
			name:         "unsupported source digest fails before uploading",
			resource:     withDigest(chartDigest, "ociArtifactDigest/v1"),
			wantErr:      "unsupported normalisation algorithm",
			wantRequests: []string{detect},
		},
		{
			name:     "extra identity gets its own path and property",
			resource: func(res *descriptorv2.Resource) { res.ExtraIdentity = runtime.Identity{"arch": "arm64", "os": "linux"} },
			check: func(r *require.Assertions, u uploadRun) {
				p := u.bodyPUTs()[0]
				r.True(strings.HasPrefix(p.Path, "/artifactory/helm-local/"+file+"-"), p.Path)
				r.Equal("arch=arm64,os=linux", p.Properties["ocm.resource.extraIdentity"], "separators survive the matrix parameter escaping")
			},
		},
		{
			name:       "custom path",
			path:       "team a/charts/mychart.tgz",
			wantAccess: access{helmChart: "mychart:0.1.0"},
			check: func(r *require.Assertions, u uploadRun) {
				r.Equal("/artifactory/helm-local/team a/charts/mychart.tgz", u.bodyPUTs()[0].Path)
				r.Equal(owner, u.bodyPUTs()[0].Properties)
			},
		},
		{name: "custom path must not leave the repository", path: "../other/mychart.tgz", wantErr: "path", wantRequests: []string{detect}},
		{name: "custom path must be relative", path: "/abs/mychart.tgz", wantErr: "path", wantRequests: []string{detect}},
		{name: "custom path must not have empty segments", path: "a//mychart.tgz", wantErr: "path", wantRequests: []string{detect}},
		{name: "custom path must not have dot segments", path: "a/./mychart.tgz", wantErr: "path", wantRequests: []string{detect}},
		{name: "custom path must not have backslashes", path: `a\b.tgz`, wantErr: "path", wantRequests: []string{detect}},
		{name: "helm custom path must end in .tgz", path: "mychart.zip", wantErr: "path", wantRequests: []string{detect}},
		{name: "npm custom path must end in .tgz", repoType: "npm", path: "packages/renamed", wantErr: `path "packages/renamed" must end in .tgz`, wantRequests: []string{detect}},
		{
			name:         "resource names cannot escape the component version path",
			resource:     func(res *descriptorv2.Resource) { res.Name = "../../other" },
			wantErr:      "must not contain path separators",
			wantRequests: []string{detect},
		},
		{
			name: "a file stored for another component version is never overwritten",
			path: "shared/mychart.tgz",
			seed: func(repo *artifactorytest.Repository) {
				repo.Store("shared/mychart.tgz", "0123", map[string]string{"ocm.component.name": "ocm.software/test", "ocm.component.version": "2.0.0", "ocm.resource.name": "renamed", "ocm.resource.version": "9.9.9"})
			},
			wantErr: "was not uploaded for this resource (recorded owner: ",
			check:   nothingWritten,
		},
		{
			name: "a file stored for another extra identity is never overwritten",
			path: "shared/mychart.tgz",
			seed: func(repo *artifactorytest.Repository) {
				repo.Store("shared/mychart.tgz", "0123", map[string]string{"ocm.component.name": "ocm.software/test", "ocm.component.version": "1.0.0", "ocm.resource.name": "renamed", "ocm.resource.version": "9.9.9", "ocm.resource.extraIdentity": "arch=arm64"})
			},
			wantErr: "was not uploaded for this resource",
			check:   nothingWritten,
		},
		{
			name:    "a file not uploaded by ocm is never overwritten",
			path:    "shared/mychart.tgz",
			seed:    func(repo *artifactorytest.Repository) { repo.Store("shared/mychart.tgz", "0123", nil) },
			wantErr: "refusing to overwrite it, configure a different path",
			check:   nothingWritten,
		},
		{
			name:     "local blob without repository provider",
			resource: func(res *descriptorv2.Resource) { res.Access = localBlobAccess() },
			wantErr:  "no component version repository provider configured",
		},
		{
			name:       "local blob is read from the source component version",
			resource:   func(res *descriptorv2.Resource) { res.Access = localBlobAccess() },
			local:      true,
			wantAccess: access{helmChart: "mychart:0.1.0"},
			check: func(r *require.Assertions, u uploadRun) {
				r.Equal("ocm.software/test", u.local.component)
				r.Equal("1.0.0", u.local.version)
				r.Equal(runtime.Identity{"name": "renamed", "version": "9.9.9"}, u.local.identity)
				r.Equal(chartTGZ, u.bodyPUTs()[0].Body)
			},
		},
		{
			name:       "helm chart OCI artifact uploads its chart layer",
			content:    chartLayout,
			mediaType:  layout.MediaTypeOCIImageLayoutTarGzipV1,
			resource:   ociImageSource(invalidDigest),
			wantAccess: access{helmChart: "mychart:0.1.0"},
			wantDigest: chartDigest,
			check: func(r *require.Assertions, u uploadRun) {
				r.Equal(chartTGZ, u.bodyPUTs()[0].Body)
				r.Equal("application/gzip", u.bodyPUTs()[0].ContentType)
			},
		},
		{
			name:       "generic uploads an OCI artifact as its OCI layout",
			repoType:   "generic",
			content:    chartLayout,
			mediaType:  layout.MediaTypeOCIImageLayoutTarGzipV1,
			resource:   ociImageSource(invalidDigest),
			wantAccess: access{url: "/artifactory/helm-local/" + file, mediaType: layout.MediaTypeOCIImageLayoutTarGzipV1},
			wantDigest: sha256Hex(chartLayout),
			check: func(r *require.Assertions, u uploadRun) {
				r.Equal(chartLayout, u.bodyPUTs()[0].Body)
				r.Equal(layout.MediaTypeOCIImageLayoutTarGzipV1, u.bodyPUTs()[0].ContentType)
			},
		},
		{
			name:         "generic stores the content and publishes a Wget access",
			repoType:     "generic",
			content:      hello,
			mediaType:    "text/plain",
			wantAccess:   access{url: "/artifactory/helm-local/" + file, mediaType: "text/plain"},
			wantDigest:   helloDigest,
			wantRequests: []string{detect, fileInfo, put, put},
			check: func(r *require.Assertions, u uploadRun) {
				r.True(u.reqs[2].Deploy, "deploy by checksum is tried first")
				r.Equal(hello, u.reqs[3].Body)
				r.Equal(owner, u.reqs[3].Properties)
			},
		},
		{
			name:       "maven access points at the file artifactory stored",
			repoType:   "maven",
			content:    hello,
			seed:       storedElsewhere("ocm.software/test/1.0.0/renamed-1.0.0-20260928.182907-1"),
			resource:   func(res *descriptorv2.Resource) { res.Version = "1.0.0-SNAPSHOT" },
			wantAccess: access{url: "/artifactory/helm-local/ocm.software/test/1.0.0/renamed-1.0.0-20260928.182907-1"},
		},
		{
			name:       "maven re-transfer deploys by checksum and points at the file artifactory stored",
			repoType:   "maven",
			content:    hello,
			seed:       storedElsewhere("ocm.software/test/1.0.0/renamed-1.0.0-20260928.182907-1"),
			resource:   func(res *descriptorv2.Resource) { res.Version = "1.0.0-SNAPSHOT" },
			transfers:  2,
			wantAccess: access{url: "/artifactory/helm-local/ocm.software/test/1.0.0/renamed-1.0.0-20260928.182907-1"},
			check: func(r *require.Assertions, u uploadRun) {
				r.True(u.reqs[len(u.reqs)-1].Deploy, "the second transfer reuses the stored content")
				r.Empty(u.bodyPUTs())
			},
		},
		{
			name:     "npm publishes a package artifactory recognizes",
			repoType: "npm",
			content:  hello,
			seed: func(repo *artifactorytest.Repository) {
				repo.NPM = map[string]artifactorytest.Package{helloDigest: {Name: "renamed", Version: "9.9.9"}}
			},
			wantAccess:   access{url: "/artifactory/helm-local/" + file + ".tgz"},
			wantRequests: []string{detect, fileInfo + ".tgz", put + ".tgz", put + ".tgz", fileInfo + ".tgz" + npmProps},
		},
		{
			name:     "npm content that is not a package is removed and fails",
			repoType: "npm",
			content:  hello,
			wantErr:  "content of resource name=renamed,version=9.9.9 is not an npm package: artifactory recorded no npm.name and npm.version for {url}/artifactory/helm-local/" + file + ".tgz",
			check:    func(r *require.Assertions, u uploadRun) { r.False(u.repo.Stored(file + ".tgz")) },
		},
		{
			name:     "npm re-transfer reuses the stored tarball",
			repoType: "npm",
			content:  hello,
			resource: withDigest(helloDigest, "genericBlobDigest/v1"),
			seed: func(repo *artifactorytest.Repository) {
				repo.NPM = map[string]artifactorytest.Package{helloDigest: {Name: "renamed", Version: "9.9.9"}}
			},
			transfers:    2,
			wantRequests: []string{detect, fileInfo + ".tgz", fileInfo + ".tgz" + npmProps},
		},
		{
			name:     "npm custom .tgz path",
			repoType: "npm",
			content:  hello,
			path:     "packages/renamed-9.9.9.tgz",
			seed: func(repo *artifactorytest.Repository) {
				repo.NPM = map[string]artifactorytest.Package{helloDigest: {Name: "renamed", Version: "9.9.9"}}
			},
			wantAccess: access{url: "/artifactory/helm-local/packages/renamed-9.9.9.tgz"},
		},
		{
			name:    "remote repositories are rejected",
			seed:    func(repo *artifactorytest.Repository) { repo.RClass = "remote" },
			wantErr: `artifactory repository "helm-local" is a remote repository; uploads need a local repository`,
			check:   nothingWritten,
		},
		{name: "federated repositories are accepted", repoType: "generic", seed: func(repo *artifactorytest.Repository) { repo.RClass = "federated" }},
		{name: "package type is case-insensitive", repoType: "Maven", content: hello, check: func(r *require.Assertions, u uploadRun) { r.Len(u.bodyPUTs(), 1) }},
		{
			name:     "unsupported package type",
			repoType: "docker",
			wantErr:  `artifactory repository "helm-local" has package type "docker"; supported: helm, generic, maven, npm`,
			check:    nothingWritten,
		},
		{
			name:    "unauthorized repository detection",
			seed:    func(repo *artifactorytest.Repository) { repo.DetectionStatus = http.StatusUnauthorized },
			wantErr: `failed detecting the type of artifactory repository "helm-local": GET {url}/artifactory/api/repositories/helm-local returned status 401`,
			check:   nothingWritten,
		},
		{
			name:    "invalid repository configuration",
			seed:    func(repo *artifactorytest.Repository) { repo.DetectionBody = "not json" },
			wantErr: `failed detecting the type of artifactory repository "helm-local": failed decoding response of GET`,
			check:   nothingWritten,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			repo := &artifactorytest.Repository{Charts: map[string]artifactorytest.Package{chartDigest: {Name: "mychart", Version: "0.1.0"}}, PackageType: tc.repoType}
			if tc.seed != nil {
				tc.seed(repo)
			}
			srv := httptest.NewServer(repo)
			t.Cleanup(srv.Close)
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
			spec := &uploadv1alpha1.RepositoryUploadSpec{
				Resource:         res,
				ComponentVersion: &uploadv1alpha1.RepositoryUploadComponentVersion{Component: "ocm.software/test", Version: "1.0.0"},
				URL:              srv.URL,
				Repository:       artifactorytest.Key,
				Path:             tc.path,
			}
			local := &localChartRepo{content: content}
			tr := &Transformer{repositoryupload.Uploader{
				Scheme:             scheme,
				ResourceRepository: &uploadtest.ResourceRepository{Content: content, MediaType: tc.mediaType},
				PollInterval:       time.Millisecond,
			}}
			if tc.creds != nil {
				tr.CredentialProvider = tc.creds
			}
			if res.Access.GetType().Name == descriptorv2.LocalBlobAccessType {
				spec.ComponentVersion.Repository = &runtime.Raw{Type: runtime.NewVersionedType("OCIRepository", "v1"), Data: []byte(`{"type":"OCIRepository/v1","baseUrl":"ghcr.io/source"}`)}
			}
			if tc.local {
				tr.RepoProvider = &localChartRepoProvider{repo: local}
			}

			var out runtime.Typed
			var err error
			var reqs []artifactorytest.Request
			for i := range max(tc.transfers, 1) {
				before := len(repo.Requests())
				out, err = tr.Transform(t.Context(), &uploadv1alpha1.ArtifactoryUpload{Type: uploadv1alpha1.ArtifactoryUploadV1alpha1, ID: "upload", Spec: spec})
				reqs = repo.Requests()[before:]
				if i < tc.transfers-1 {
					r.NoError(err)
				}
			}

			run := uploadRun{repo: repo, reqs: reqs, local: local}
			if tc.wantErr != "" {
				r.ErrorContains(err, strings.ReplaceAll(tc.wantErr, "{url}", srv.URL))
			} else {
				r.NoError(err)
				run.out = out.(*uploadv1alpha1.ArtifactoryUpload).Output.Resource
				r.Equal("renamed", run.out.Name)
				switch {
				case tc.wantAccess.helmChart != "":
					var access helmaccessv1.Helm
					r.NoError(helmaccess.Scheme.Convert(run.out.Access, &access))
					r.Equal(srv.URL+"/artifactory/api/helm/helm-local", access.HelmRepository)
					r.Equal(tc.wantAccess.helmChart, access.HelmChart, "name and version come from artifactory, not from the resource")
				case tc.wantAccess.url != "":
					var access wgetaccessv1.Wget
					r.NoError(wgetaccess.Scheme.Convert(run.out.Access, &access))
					r.Equal(srv.URL+tc.wantAccess.url, access.URL)
					if tc.wantAccess.mediaType != "" {
						r.Equal(tc.wantAccess.mediaType, access.MediaType)
					}
				}
				if tc.wantDigest != "" {
					r.Equal(&descriptorv2.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: tc.wantDigest}, run.out.Digest)
				}
			}
			if tc.wantRequests != nil {
				var got []string
				for _, req := range reqs {
					target := req.Method + " " + req.Path
					if req.Query != "" {
						target += "?" + req.Query
					}
					got = append(got, target)
				}
				r.Equal(tc.wantRequests, got)
			}
			if tc.check != nil {
				tc.check(r, run)
			}
		})
	}
}

func localBlobAccess() *runtime.Raw {
	return &runtime.Raw{Type: runtime.NewVersionedType("LocalBlob", "v1"), Data: []byte(`{"type":"LocalBlob/v1","localReference":"sha256:abc","mediaType":"application/vnd.cncf.helm.chart.content.v1.tar+gzip"}`)}
}

// ociImageSource makes the resource a remote OCI image with a genericBlobDigest/v1 digest value,
// which does not describe the downloaded OCI layout.
func ociImageSource(digestValue string) func(*descriptorv2.Resource) {
	return func(res *descriptorv2.Resource) {
		res.Access = &runtime.Raw{Type: runtime.NewVersionedType("OCIImage", "v1"), Data: []byte(`{"type":"OCIImage/v1","imageReference":"registry.example.com/charts/mychart:0.1.0"}`)}
		res.Digest = &descriptorv2.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: digestValue}
	}
}
