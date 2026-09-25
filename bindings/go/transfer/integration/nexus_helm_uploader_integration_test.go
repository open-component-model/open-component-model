package integration_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ctfrepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

const (
	nexusImage          = "sonatype/nexus3:3.96.3"
	nexusHelmRepository = "helm-hosted"
)

// Test_Integration_TransferHelmResource_NexusHelmUploaderDeploysChart verifies that a Helm/v1
// resource routed through a Nexus helm uploader configuration is uploaded to a real Nexus Helm
// hosted repository with redeploy disabled and re-described with the chart name and version
// Nexus records. The transfer runs twice: the second upload is rejected by Nexus because the
// chart already exists, which the uploader recovers from because Nexus stores the same content.
func Test_Integration_TransferHelmResource_NexusHelmUploaderDeploysChart(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := t.Context()

	baseURL, adminPassword := startNexus(t)
	helmRepo := baseURL + "/repository/" + nexusHelmRepository

	chartTgzBytes, err := os.ReadFile("../../helm/testdata/mychart-0.1.0.tgz")
	r.NoError(err)

	// The helm downloader with helmChart "mychart-0.1.0.tgz" GETs the file directly.
	srcSrv := httptest.NewServer(http.FileServer(http.Dir("../../helm/testdata/provenance")))
	t.Cleanup(srcSrv.Close)

	componentName := "ocm.software/nexus-helm-uploader-test"
	componentVersion := "1.0.0"
	sourceCTFPath := t.TempDir()
	ctfRepo := createCTFRepository(t, sourceCTFPath)

	helmAccessData, err := json.Marshal(map[string]string{
		"type":           "Helm/v1",
		"helmRepository": srcSrv.URL,
		"helmChart":      "mychart-0.1.0.tgz",
	})
	r.NoError(err)
	rawHelmAccess := &runtime.Raw{}
	r.NoError(rawHelmAccess.UnmarshalJSON(helmAccessData))
	r.NoError(ctfRepo.AddComponentVersion(ctx, &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: componentName, Version: componentVersion},
			},
			Provider: descriptor.Provider{Name: "test-provider"},
			Resources: []descriptor.Resource{{
				ElementMeta: descriptor.ElementMeta{
					// Deliberately differs from Chart.yaml: name and version come from the chart.
					ObjectMeta: descriptor.ObjectMeta{Name: "chart-resource", Version: "9.9.9"},
				},
				Type:     "helmChart",
				Relation: descriptor.ExternalRelation,
				Access:   rawHelmAccess,
			}},
		},
	}))

	sourceSpec := &ctfrepospec.Repository{
		Type:     runtime.Type{Name: ctfrepospec.Type, Version: ctfrepospec.Version},
		FilePath: sourceCTFPath,
	}
	targetCTFPath := t.TempDir()
	targetSpec := &ctfrepospec.Repository{
		Type:       runtime.Type{Name: ctfrepospec.Type, Version: ctfrepospec.Version},
		FilePath:   targetCTFPath,
		AccessMode: "readwrite|create",
	}
	uploaders := []transferv1alpha1.UploaderConfig{&transferv1alpha1.HelmUploaderConfig{
		Type:       runtime.NewVersionedType(transferv1alpha1.HelmUploaderConfigType, transferv1alpha1.Version),
		MatchSpec:  transferv1alpha1.UploaderMatch{AccessType: runtime.NewVersionedType(helmaccessv1.Type, helmaccessv1.LegacyTypeVersion)},
		Server:     transferv1alpha1.HelmRepositoryServerNexus,
		URL:        baseURL,
		Repository: nexusHelmRepository,
	}}

	id, err := runtime.ParseURLToIdentity(helmRepo)
	r.NoError(err)
	id.SetType(helmidentityv1.Type)
	credResolver := credentials.NewStaticCredentialsResolver(map[string]map[string]string{
		id.String(): {"username": "admin", "password": adminPassword},
	})

	transferOnce := func() {
		tgd, err := transfer.BuildGraphDefinition(ctx,
			&transferv1alpha1.Config{CopyMode: transferv1alpha1.CopyModeAllResources},
			uploaders,
			transfer.Mapping{
				Components: []transfer.ComponentID{{Component: componentName, Version: componentVersion}},
				Target:     targetSpec,
				Resolver:   transfer.NewRepositoryResolver(ctfRepo, sourceSpec),
			},
		)
		r.NoError(err)
		repoProvider := provider.NewComponentVersionRepositoryProvider(provider.WithTempDir(t.TempDir()))
		b := transfer.NewDefaultBuilder(repoProvider, helmresource.NewResourceRepository(nil), credResolver)
		graph, err := b.BuildAndCheck(tgd)
		r.NoError(err)
		r.NoError(graph.Process(ctx))
	}
	transferOnce()
	// Nexus rejects redeploying mychart-0.1.0; the uploader finds the same content stored.
	transferOnce()

	gotDesc, err := createCTFRepository(t, targetCTFPath).GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err)
	r.Len(gotDesc.Component.Resources, 1)
	gotResource := gotDesc.Component.Resources[0]
	var typedHelm helmaccessv1.Helm
	r.NoError(helmaccess.Scheme.Convert(gotResource.Access, &typedHelm))
	r.Equal(helmRepo, typedHelm.HelmRepository)
	r.Equal("mychart:0.1.0", typedHelm.HelmChart, "helmChart is the chart name and version nexus records")
	r.NotNil(gotResource.Digest)
	r.Equal("SHA-256", gotResource.Digest.HashAlgorithm)
	r.Equal("genericBlobDigest/v1", gotResource.Digest.NormalisationAlgorithm)
	r.Equal(digestOf(chartTgzBytes).Encoded(), gotResource.Digest.Value)

	// Nexus stores the chart under the path it derives from Chart.yaml and indexes it.
	r.Equal(chartTgzBytes, nexusGet(t, helmRepo+"/mychart-0.1.0.tgz", adminPassword))
	r.Contains(string(nexusGet(t, helmRepo+"/index.yaml", adminPassword)), "mychart:")
}

// startNexus starts a Nexus Repository 3 container, accepts the Community Edition EULA and
// creates a Helm hosted repository with redeploy disabled. It returns the base URL and the
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
