package integration_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	helmresource "ocm.software/open-component-model/bindings/go/helm/repository/resource"
	helmaccess "ocm.software/open-component-model/bindings/go/helm/spec/access"
	helmaccessv1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	helmidentityv1 "ocm.software/open-component-model/bindings/go/helm/spec/identity/v1"
	ctfrepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	wgetrepository "ocm.software/open-component-model/bindings/go/wget/repository"
	wgetaccess "ocm.software/open-component-model/bindings/go/wget/spec/access"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

const (
	nexusImage          = "sonatype/nexus3:3.96.3"
	nexusHelmRepository = "helm-hosted"
	nexusNpmRepository  = "npm-hosted"
)

// Test_Integration_NexusUploader transfers resources into the hosted repositories of one real
// Nexus Repository 3 server, one parallel subtest per repository format. Every subtest transfers twice:
// all repositories disallow redeploys, so the second transfer must reuse what the first stored.
func Test_Integration_NexusUploader(t *testing.T) {
	t.Parallel()
	baseURL, adminPassword := startNexus(t)
	creds := nexusCredentials(t, baseURL, adminPassword, nexusHelmRepository, "raw-hosted", "maven-releases", nexusNpmRepository)
	uploader := func(accessType runtime.Type, repository, path string) transferv1alpha1.UploaderConfig {
		return &transferv1alpha1.NexusUploaderConfig{
			Type:       runtime.NewVersionedType(transferv1alpha1.NexusUploaderConfigType, transferv1alpha1.Version),
			Match:      `resource.access.isType("` + accessType.String() + `")`,
			URL:        baseURL,
			Repository: repository,
			Path:       path,
		}
	}
	wgetType := runtime.NewVersionedType(wgetaccess.WgetConsumerType, wgetaccessv1.Version)
	// wgetAccesses returns the Wget/v1 access URLs of the transferred resources by resource name.
	wgetAccesses := func(r *require.Assertions, targetPath, component, version string) map[string]string {
		desc, err := createCTFRepository(t, targetPath).GetComponentVersion(t.Context(), component, version)
		r.NoError(err)
		urls := map[string]string{}
		for _, res := range desc.Component.Resources {
			var access wgetaccessv1.Wget
			r.NoError(wgetaccess.Scheme.Convert(res.Access, &access))
			urls[res.Name] = access.URL
		}
		return urls
	}

	// The helm repository rejects redeploying mychart-0.1.0; the uploader finds the same
	// content stored and re-describes the resource with the chart name and version Nexus records.
	t.Run("helm", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		helmRepo := baseURL + "/repository/" + nexusHelmRepository
		chartTgzBytes, err := os.ReadFile("../../helm/testdata/mychart-0.1.0.tgz")
		r.NoError(err)

		// The helm downloader with helmChart "mychart-0.1.0.tgz" GETs the file directly.
		srcSrv := httptest.NewServer(http.FileServer(http.Dir("../../helm/testdata/provenance")))
		t.Cleanup(srcSrv.Close)
		const component, version = "ocm.software/nexus-helm-uploader-test", "1.0.0"
		sourcePath := t.TempDir()
		ctfRepo := createCTFRepository(t, sourcePath)
		helmAccessData, err := json.Marshal(map[string]string{"type": "Helm/v1", "helmRepository": srcSrv.URL, "helmChart": "mychart-0.1.0.tgz"})
		r.NoError(err)
		rawHelmAccess := &runtime.Raw{}
		r.NoError(rawHelmAccess.UnmarshalJSON(helmAccessData))
		r.NoError(ctfRepo.AddComponentVersion(t.Context(), &descriptor.Descriptor{
			Meta: descriptor.Meta{Version: "v2"},
			Component: descriptor.Component{
				ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: component, Version: version}},
				Provider:      descriptor.Provider{Name: "test-provider"},
				Resources: []descriptor.Resource{{
					// Deliberately differs from Chart.yaml: name and version come from the chart.
					ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "chart-resource", Version: "9.9.9"}},
					Type:        "helmChart",
					Relation:    descriptor.ExternalRelation,
					Access:      rawHelmAccess,
				}},
			},
		}))
		sourceSpec := &ctfrepospec.Repository{Type: runtime.Type{Name: ctfrepospec.Type, Version: ctfrepospec.Version}, FilePath: sourcePath}
		targetPath, targetSpec := newTargetCTF(t)
		up := uploader(runtime.NewVersionedType(helmaccessv1.Type, helmaccessv1.LegacyTypeVersion), nexusHelmRepository, "")
		for range 2 {
			transferOnce(t, ctfRepo, sourceSpec, targetSpec, up, helmresource.NewResourceRepository(nil), creds, component, version)
		}

		gotDesc, err := createCTFRepository(t, targetPath).GetComponentVersion(t.Context(), component, version)
		r.NoError(err)
		r.Len(gotDesc.Component.Resources, 1)
		gotResource := gotDesc.Component.Resources[0]
		var typedHelm helmaccessv1.Helm
		r.NoError(helmaccess.Scheme.Convert(gotResource.Access, &typedHelm))
		r.Equal(helmRepo, typedHelm.HelmRepository)
		r.Equal("mychart:0.1.0", typedHelm.HelmChart, "helmChart is the chart name and version nexus records")
		r.Equal(&descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: digestOf(chartTgzBytes).Encoded()}, gotResource.Digest)

		// Nexus stores the chart under the path it derives from Chart.yaml and indexes it.
		r.Equal(chartTgzBytes, nexusGet(t, helmRepo+"/mychart-0.1.0.tgz", adminPassword))
		r.Contains(string(nexusGet(t, helmRepo+"/index.yaml", adminPassword)), "mychart:")
	})

	// The second transfer finds the stored file by HEAD and the asset search, which the uploader
	// polls until Nexus has indexed the first upload.
	t.Run("raw", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		const component, version = "ocm.software/nexus-raw-uploader-test", "1.0.0"
		data := []byte("hello")
		ctfRepo, sourceSpec := addWgetComponent(t, component, version, wgetFile{name: "blob.txt", resource: "raw-resource", version: "1.0.0", mediaType: "text/plain", data: data})
		targetPath, targetSpec := newTargetCTF(t)
		for range 2 {
			transferOnce(t, ctfRepo, sourceSpec, targetSpec, uploader(wgetType, "raw-hosted", ""), wgetrepository.NewResourceRepository(nil), creds, component, version)
		}

		want := baseURL + "/repository/raw-hosted/ocm.software/nexus-raw-uploader-test/1.0.0/raw-resource-1.0.0"
		r.Equal(map[string]string{"raw-resource": want}, wgetAccesses(r, targetPath, component, version))
		r.Equal(data, nexusGet(t, want, adminPassword))
	})

	// Release files go through the components API, which records them in maven-metadata.xml.
	t.Run("maven2", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		const component, version = "ocm.software/nexus-maven-uploader-test", "1.0.0"
		pom := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>demo</artifactId>
  <version>1.0.0</version>
</project>
`)
		ctfRepo, sourceSpec := addWgetComponent(t, component, version,
			wgetFile{name: "demo-1.0.0.jar", resource: "jar", version: "1.0.0", mediaType: "application/java-archive", data: []byte("jar")},
			wgetFile{name: "demo-1.0.0.pom", resource: "pom", version: "1.0.0", mediaType: "application/xml", data: pom},
		)
		targetPath, targetSpec := newTargetCTF(t)
		up := uploader(wgetType, "maven-releases",
			`${"com/example/demo/" + resource.version + "/demo-" + resource.version + (resource.name == "pom" ? ".pom" : ".jar")}`)
		for range 2 {
			transferOnce(t, ctfRepo, sourceSpec, targetSpec, up, wgetrepository.NewResourceRepository(nil), creds, component, version)
		}

		base := baseURL + "/repository/maven-releases/com/example/demo"
		r.Equal(map[string]string{"jar": base + "/1.0.0/demo-1.0.0.jar", "pom": base + "/1.0.0/demo-1.0.0.pom"}, wgetAccesses(r, targetPath, component, version))
		r.Equal([]byte("jar"), nexusGet(t, base+"/1.0.0/demo-1.0.0.jar", adminPassword))
		r.Eventually(func() bool {
			status, body := nexusRequest(t, http.MethodGet, base+"/maven-metadata.xml", adminPassword, nil)
			return status == http.StatusOK && strings.Contains(string(body), "<version>1.0.0</version>")
		}, 15*time.Second, 500*time.Millisecond, "maven-metadata.xml lists the uploaded version")
	})

	// Nexus reads name and version from package.json and stores the tarball under its npm path.
	t.Run("npm", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		const component, version = "ocm.software/nexus-npm-uploader-test", "1.0.0"
		tarball := npmTarball(t, `{"name":"ocm-integration-demo","version":"1.0.0"}`)
		ctfRepo, sourceSpec := addWgetComponent(t, component, version, wgetFile{name: "pkg.tgz", resource: "pkg", version: "1.0.0", mediaType: "application/gzip", data: tarball})
		targetPath, targetSpec := newTargetCTF(t)
		for range 2 {
			transferOnce(t, ctfRepo, sourceSpec, targetSpec, uploader(wgetType, nexusNpmRepository, ""), wgetrepository.NewResourceRepository(nil), creds, component, version)
		}

		want := baseURL + "/repository/" + nexusNpmRepository + "/ocm-integration-demo/-/ocm-integration-demo-1.0.0.tgz"
		r.Equal(map[string]string{"pkg": want}, wgetAccesses(r, targetPath, component, version))
		r.Equal(tarball, nexusGet(t, want, adminPassword))
		r.Eventually(func() bool {
			status, body := nexusRequest(t, http.MethodGet, baseURL+"/repository/"+nexusNpmRepository+"/ocm-integration-demo", adminPassword, nil)
			var metadata struct {
				Versions map[string]json.RawMessage `json:"versions"`
			}
			return status == http.StatusOK && json.Unmarshal(body, &metadata) == nil && metadata.Versions["1.0.0"] != nil
		}, 15*time.Second, 500*time.Millisecond, "the npm package metadata lists the uploaded version")
	})
}

// nexusCredentials resolves the admin credentials for the HelmChartRepository identity of each
// repository, which the Nexus uploader looks up for every repository format.
func nexusCredentials(t *testing.T, baseURL, password string, repos ...string) credentials.Resolver {
	t.Helper()
	creds := map[string]map[string]string{}
	for _, repo := range repos {
		id, err := runtime.ParseURLToIdentity(baseURL + "/repository/" + repo)
		require.NoError(t, err)
		id.SetType(helmidentityv1.Type)
		creds[id.String()] = map[string]string{"username": "admin", "password": password}
	}
	return credentials.NewStaticCredentialsResolver(creds)
}

// npmTarball returns a gzipped npm package tarball holding only package/package.json.
func npmTarball(t *testing.T, packageJSON string) []byte {
	t.Helper()
	r := require.New(t)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	r.NoError(tw.WriteHeader(&tar.Header{Name: "package/package.json", Mode: 0o644, Size: int64(len(packageJSON)), Typeflag: tar.TypeReg}))
	_, err := io.WriteString(tw, packageJSON)
	r.NoError(err)
	r.NoError(tw.Close())
	r.NoError(gz.Close())
	return buf.Bytes()
}

// startNexus starts a Nexus Repository 3 container, accepts the Community Edition EULA and
// creates Helm, raw and npm hosted repositories with redeploy disabled. It returns the base URL and the
// admin password.
func startNexus(t *testing.T) (baseURL, adminPassword string) {
	t.Helper()
	r := require.New(t)
	ctx := t.Context()

	container, err := testcontainers.Run(ctx, nexusImage,
		testcontainers.WithExposedPorts("8081/tcp"),
		testcontainers.WithEnv(map[string]string{
			"INSTALL4J_ADD_VM_PARAMS": "-Xms1g -Xmx1g -XX:MaxDirectMemorySize=1g -Djava.util.prefs.userRoot=/nexus-data/javaprefs",
		}),
		testcontainers.WithWaitStrategy(wait.ForHTTP("/service/rest/v1/status/writable").WithPort("8081/tcp").WithStartupTimeout(5*time.Minute)),
	)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(testcontainers.TerminateContainer(container)) })
	hostPort, err := container.PortEndpoint(ctx, "8081/tcp", "")
	r.NoError(err)
	baseURL = "http://" + hostPort

	rc, err := container.CopyFileFromContainer(ctx, "/nexus-data/admin.password")
	r.NoError(err)
	password, err := io.ReadAll(rc)
	r.NoError(err)
	r.NoError(rc.Close())
	adminPassword = string(bytes.TrimSpace(password))

	// Nexus Community Edition blocks uploads until the EULA is accepted.
	eulaURL := baseURL + "/service/rest/v1/system/eula"
	status, body := nexusRequest(t, http.MethodGet, eulaURL, adminPassword, nil)
	if status != http.StatusNotFound {
		r.Equal(http.StatusOK, status, string(body))
		var eula map[string]any
		r.NoError(json.Unmarshal(body, &eula))
		eula["accepted"] = true
		accepted, err := json.Marshal(eula)
		r.NoError(err)
		status, body = nexusRequest(t, http.MethodPost, eulaURL, adminPassword, accepted)
		r.Equal(http.StatusNoContent, status, string(body))
	}

	status, body = nexusRequest(t, http.MethodPost, baseURL+"/service/rest/v1/repositories/helm/hosted", adminPassword,
		[]byte(`{"name":"`+nexusHelmRepository+`","online":true,"storage":{"blobStoreName":"default","strictContentTypeValidation":true,"writePolicy":"allow_once"}}`))
	r.Equal(http.StatusCreated, status, string(body))

	status, body = nexusRequest(t, http.MethodPost, baseURL+"/service/rest/v1/repositories/raw/hosted", adminPassword,
		[]byte(`{"name":"raw-hosted","online":true,"storage":{"blobStoreName":"default","strictContentTypeValidation":false,"writePolicy":"allow_once"}}`))
	r.Equal(http.StatusCreated, status, string(body))

	// maven-releases exists by default.
	status, body = nexusRequest(t, http.MethodPost, baseURL+"/service/rest/v1/repositories/npm/hosted", adminPassword,
		[]byte(`{"name":"`+nexusNpmRepository+`","online":true,"storage":{"blobStoreName":"default","strictContentTypeValidation":true,"writePolicy":"allow_once"}}`))
	r.Equal(http.StatusCreated, status, string(body))
	return baseURL, adminPassword
}

// nexusRequest sends a request as the Nexus admin and returns status and body.
func nexusRequest(t *testing.T, method, target, password string, body []byte) (int, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, target, bytes.NewReader(body))
	require.NoError(t, err)
	req.SetBasicAuth("admin", password)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, respBody
}

// nexusGet GETs target as the Nexus admin and requires 200.
func nexusGet(t *testing.T, target, password string) []byte {
	t.Helper()
	status, body := nexusRequest(t, http.MethodGet, target, password, nil)
	require.Equal(t, http.StatusOK, status, string(body))
	return body
}
