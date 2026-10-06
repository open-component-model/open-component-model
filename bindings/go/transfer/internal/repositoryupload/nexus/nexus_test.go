package nexus

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
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
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/nexus/nexustest"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/uploadtest"
	uploadv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/transformation/spec/v1alpha1"
	wgetaccess "ocm.software/open-component-model/bindings/go/wget/spec/access"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
	wgetcredsv1 "ocm.software/open-component-model/bindings/go/wget/spec/credentials/v1"
	wgetidentityv1 "ocm.software/open-component-model/bindings/go/wget/spec/identity/v1"
)

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// uploadRun is what a TestTransform row checks beyond its columns.
type uploadRun struct {
	repo *nexustest.FakeRepository
	reqs []nexustest.Request // of the last transfer
}

// requests returns the method and path of the requests of the last transfer.
func (u uploadRun) requests() []string {
	var out []string
	for _, req := range u.reqs {
		out = append(out, req.String())
	}
	return out
}

func (u uploadRun) hasRequest(prefix string) bool {
	return slices.ContainsFunc(u.requests(), func(req string) bool { return strings.HasPrefix(req, prefix) })
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
		repoPath     = "/repository/helm-hosted/"
		detect       = "GET /service/rest/v1/repositories/helm-hosted"
		chartPut     = "PUT " + repoPath + "renamed-9.9.9.tgz"
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
	store := func(key, digest string) func(*nexustest.FakeRepository) {
		return func(repo *nexustest.FakeRepository) { repo.Store(key, digest) }
	}
	nothingWritten := func(r *require.Assertions, u uploadRun) {
		r.False(u.hasRequest("PUT "), "nothing may be written")
		r.False(u.hasRequest(components), "nothing may be written")
	}
	// pinned returns the asset search download URL path that pins the content with digest d
	// among the assets query selects.
	pinned := func(query url.Values, d digest.Digest) string {
		query.Set("repository", "helm-hosted")
		query.Set(d.Algorithm().String(), d.Encoded())
		return "/service/rest/v1/search/assets/download?" + query.Encode()
	}
	rawPinned := func(path string, d digest.Digest) string { return pinned(url.Values{"name": {"/" + path}}, d) }
	mavenPinned := func(classifier, extension string, d digest.Digest) string {
		query := url.Values{"maven.groupId": {"com.example"}, "maven.artifactId": {"demo"}, "maven.baseVersion": {"1.0.0"}, "maven.extension": {extension}}
		if classifier != "" {
			query.Set("maven.classifier", classifier)
		}
		return pinned(query, d)
	}
	parentPOM := []byte(`<project><parent><groupId>com.example</groupId></parent><artifactId>demo</artifactId><version>1.0.0</version></project>`)

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
		creds     uploadtest.StubCredentials
		seed      func(*nexustest.FakeRepository)
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
			seed:         func(repo *nexustest.FakeRepository) { repo.AllowOnce = true; repo.Store("mychart-0.1.0", chartDigest) },
			wantAccess:   access{helmChart: "mychart:0.1.0"},
			wantRequests: []string{detect, chartPut, search, search},
		},
		{
			name:    "helm redeploy rejection of different content under the same chart version fails",
			seed:    func(repo *nexustest.FakeRepository) { repo.AllowOnce = true; repo.Store("mychart-0.1.0", otherDigest) },
			wantErr: "returned status 409",
		},
		{
			name:         "helm context path is kept",
			seed:         func(repo *nexustest.FakeRepository) { repo.BasePath = "/nexus" },
			wantAccess:   access{helmChart: "mychart:0.1.0"},
			wantRequests: []string{"GET /nexus/service/rest/v1/repositories/helm-hosted", "PUT /nexus" + repoPath + "renamed-9.9.9.tgz", "GET /nexus/service/rest/v1/search"},
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
			wantErr: "content of resource name=renamed,version=9.9.9 is not a helm chart: nexus recorded no chart name and version for {url}" + repoPath + "renamed-9.9.9.tgz",
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
			wantAccess:   access{url: rawPinned(rawPath, digest.FromBytes(hello)), mediaType: "text/plain"},
			wantDigest:   helloDigest,
			wantRequests: []string{detect, "HEAD " + repoPath + rawPath, "PUT " + repoPath + rawPath, searchAssets},
		},
		{
			name:     "raw SHA-512 source digest is verified after the upload",
			repoType: "raw",
			content:  hello,
			resource: func(res *descriptorv2.Resource) {
				res.Digest = &descriptorv2.Digest{HashAlgorithm: "SHA-512", NormalisationAlgorithm: "genericBlobDigest/v1", Value: digest.SHA512.FromBytes(hello).Encoded()}
			},
			wantAccess:   access{url: rawPinned(rawPath, digest.SHA512.FromBytes(hello))},
			wantRequests: []string{detect, "HEAD " + repoPath + rawPath, "PUT " + repoPath + rawPath, searchAssets},
		},
		{
			name:         "raw re-transfer reuses the file once the search finds it",
			repoType:     "raw",
			content:      hello,
			resource:     withDigest(helloDigest),
			seed:         func(repo *nexustest.FakeRepository) { repo.SearchLag = 2 },
			transfers:    2,
			wantAccess:   access{url: rawPinned(rawPath, digest.FromBytes(hello))},
			wantRequests: []string{detect, "HEAD " + repoPath + rawPath, searchAssets, searchAssets},
		},
		{
			name:         "raw file with the same content on a later search page is reused",
			repoType:     "raw",
			content:      hello,
			resource:     withDigest(helloDigest),
			seed:         func(repo *nexustest.FakeRepository) { repo.Store(rawPath, helloDigest); repo.AssetPageSize = 1 },
			wantAccess:   access{url: rawPinned(rawPath, digest.FromBytes(hello))},
			wantRequests: []string{detect, "HEAD " + repoPath + rawPath, searchAssets, searchAssets, searchAssets},
		},
		{
			name:     "raw file with other content at the path is never overwritten",
			repoType: "raw",
			content:  hello,
			resource: withDigest(helloDigest),
			seed:     store(rawPath, otherDigest),
			wantErr:  `nexus repository "helm-hosted" already stores a different file at {url}` + repoPath + rawPath + "; the uploader never overwrites files in raw repositories, configure a different path",
			check:    nothingWritten,
		},
		{
			name:         "raw custom path",
			repoType:     "raw",
			content:      hello,
			path:         "files/notes.txt",
			wantAccess:   access{url: rawPinned("files/notes.txt", digest.FromBytes(hello))},
			wantRequests: []string{detect, "HEAD " + repoPath + "files/notes.txt", "PUT " + repoPath + "files/notes.txt", searchAssets},
		},
		{
			name:     "raw unexpected HEAD status fails before uploading",
			repoType: "raw",
			content:  hello,
			resource: withDigest(helloDigest),
			// Not a 5xx, which the HTTP client retries with backoff.
			seed:    func(repo *nexustest.FakeRepository) { repo.HeadStatus = http.StatusForbidden },
			wantErr: "returned status 403",
			check:   nothingWritten,
		},
		{
			name:     "target credentials are sent on every request",
			repoType: "raw",
			content:  hello,
			creds: uploadtest.StubCredentials{wgetidentityv1.Type.String(): &wgetcredsv1.WgetCredentials{
				Type: wgetcredsv1.WgetCredentialsVersionedType, Username: "u", Password: "p",
			}},
			check: func(r *require.Assertions, u uploadRun) {
				r.NotEmpty(u.reqs)
				for _, req := range u.reqs {
					r.Equal("Basic dTpw", req.Authorization)
				}
			},
		},
		{
			name: "target credential error prevents any request",
			creds: uploadtest.StubCredentials{helmidentityv1.Type.String(): &helmcredsv1.HelmHTTPCredentials{
				Type: runtime.NewVersionedType(helmcredsv1.HelmHTTPCredentialsType, helmcredsv1.Version), CertFile: "/cert.pem", KeyFile: "/key.pem",
			}},
			wantErr: "HelmHTTPCredentials certFile/keyFile are not supported",
			check:   func(r *require.Assertions, u uploadRun) { r.Empty(u.reqs) },
		},
		{
			name:    "proxy repositories are rejected",
			seed:    func(repo *nexustest.FakeRepository) { repo.Type = "proxy" },
			wantErr: `nexus repository "helm-hosted" is a proxy repository; uploads need a hosted repository`,
		},
		{
			name:     "unsupported format",
			repoType: "pypi",
			wantErr:  `nexus repository "helm-hosted" has format "pypi"; supported: helm, raw, maven2, npm`,
		},
		{
			name:    "forbidden repository detection",
			seed:    func(repo *nexustest.FakeRepository) { repo.DetectionStatus = http.StatusForbidden },
			wantErr: `failed detecting the type of nexus repository "helm-hosted": GET {url}/service/rest/v1/repositories/helm-hosted returned status 403`,
		},
		{
			name:    "invalid repository settings",
			seed:    func(repo *nexustest.FakeRepository) { repo.DetectionBody = "not json" },
			wantErr: `failed detecting the type of nexus repository "helm-hosted": failed decoding response of GET`,
		},
		{
			name:       "maven uploads through the components API and publishes the Maven layout URL",
			repoType:   "maven2",
			content:    jar,
			resource:   mavenSource,
			path:       mavenPath,
			wantAccess: access{url: mavenPinned("sources", "jar", digest.FromBytes(jar))},
			check: func(r *require.Assertions, u uploadRun) {
				r.Equal([]string{components, searchAssets}, u.requests()[len(u.reqs)-2:], "the search finds the upload to pin it")
				r.Equal(map[string][]string{
					"maven2.groupId":           {"com.example"},
					"maven2.artifactId":        {"demo"},
					"maven2.version":           {"1.0.0"},
					"maven2.generate-pom":      {"false"},
					"maven2.asset1.extension":  {"jar"},
					"maven2.asset1.classifier": {"sources"},
				}, u.repo.MavenForms()[0])
				r.Equal(jar, u.repo.Content(mavenPath))
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
			content:    parentPOM,
			resource:   resource("demo", "1.0.0", ""),
			path:       "com/example/demo/1.0.0/demo-1.0.0.pom",
			wantAccess: access{url: mavenPinned("", "pom", digest.FromBytes(parentPOM))},
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
				r.Contains(u.requests(), "PUT "+repoPath+"com/example/demo/1.0.0-SNAPSHOT/demo-1.0.0-SNAPSHOT.jar")
				r.NotContains(u.requests(), components)
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
			seed: func(repo *nexustest.FakeRepository) {
				repo.ComponentStatus, repo.ComponentBody = http.StatusBadRequest, `[{"id":"*","message":"Version policy mismatch"}]`
			},
			wantErr: "returned status 400: [{\"id\":\"*\",\"message\":\"Version policy mismatch\"}]",
		},
		{
			name:       "maven context path is kept",
			repoType:   "maven2",
			content:    jar,
			resource:   mavenSource,
			path:       mavenPath,
			seed:       func(repo *nexustest.FakeRepository) { repo.BasePath = "/nexus" },
			wantAccess: access{url: "/nexus" + mavenPinned("sources", "jar", digest.FromBytes(jar))},
			check: func(r *require.Assertions, u uploadRun) {
				r.Contains(u.requests(), "POST /nexus/service/rest/v1/components")
			},
		},
		{
			name:       "npm uploads through the components API and publishes the stored tarball",
			repoType:   "npm",
			content:    npmTarball,
			resource:   npmSource,
			seed:       func(repo *nexustest.FakeRepository) { repo.SearchLag = 1 },
			wantAccess: access{url: pinned(url.Values{}, digest.FromBytes(npmTarball))},
			wantDigest: npmDigest,
			check:      func(r *require.Assertions, u uploadRun) { r.Contains(u.requests(), components) },
		},
		{
			name:       "npm reuses a stored package with the same content",
			repoType:   "npm",
			content:    npmTarball,
			resource:   resource("demo", "2.0.0", npmDigest),
			seed:       store(npmStored, npmDigest),
			wantAccess: access{url: pinned(url.Values{}, digest.FromBytes(npmTarball))},
			check:      nothingWritten,
		},
		{
			name:     "npm content that is not a package fails",
			repoType: "npm",
			content:  npmTarball,
			resource: npmSource,
			seed:     func(repo *nexustest.FakeRepository) { repo.NPM = nil },
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
			seed:     func(repo *nexustest.FakeRepository) { repo.SearchLag = 1000 },
			wantErr:  `nexus repository "helm-hosted" stored content sha256:` + npmDigest + ", but its search does not find it",
			check: func(r *require.Assertions, u uploadRun) {
				upload := slices.Index(u.requests(), components)
				r.NotEqual(-1, upload)
				r.Equal(slices.Repeat([]string{searchAssets}, repositoryupload.PollAttempts), u.requests()[upload+1:], "the search is polled after the upload")
			},
		},
		{
			name:     "npm redeploy rejection is returned",
			repoType: "npm",
			content:  npmTarball,
			resource: npmSource,
			seed:     func(repo *nexustest.FakeRepository) { repo.AllowOnce = true; repo.Store(npmStored, otherDigest) },
			wantErr:  "redeploy is not allowed",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			repo := &nexustest.FakeRepository{
				Charts: map[string]nexustest.Package{chartDigest: {Name: "mychart", Version: "0.1.0"}},
				NPM:    map[string]nexustest.Package{npmDigest: {Name: "@acme/demo", Version: "2.0.0"}},
				Format: tc.repoType,
			}
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
			upload := &repositoryupload.Uploader{
				Scheme:             scheme,
				ResourceRepository: &uploadtest.StubResourceRepository{Content: content, MediaType: tc.mediaType},
				PollInterval:       time.Millisecond,
			}
			if tc.creds != nil {
				upload.CredentialProvider = tc.creds
			}
			tr := &Transformer{Uploader: upload}
			step := &uploadv1alpha1.NexusUpload{Type: uploadv1alpha1.NexusUploadV1alpha1, ID: "upload", Spec: &uploadv1alpha1.RepositoryUploadSpec{
				Resource:         res,
				ComponentVersion: &uploadv1alpha1.RepositoryUploadComponentVersion{Component: "ocm.software/test", Version: "1.0.0"},
				URL:              srv.URL + repo.BasePath,
				Repository:       nexustest.Key,
				Path:             tc.path,
			}}

			var out runtime.Typed
			var err error
			var reqs []nexustest.Request
			for i := range max(tc.transfers, 1) {
				before := len(repo.Requests())
				out, err = tr.Transform(t.Context(), step)
				reqs = repo.Requests()[before:]
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
					r.Equal(srv.URL+repo.BasePath+"/repository/helm-hosted", access.HelmRepository)
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
				r.Equal(tc.wantRequests, uploadRun{reqs: reqs}.requests())
			}
			if tc.check != nil {
				tc.check(r, uploadRun{repo: repo, reqs: reqs})
			}
		})
	}
}
