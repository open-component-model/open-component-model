package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/log"
	"github.com/testcontainers/testcontainers-go/wait"
	"oras.land/oras-go/v2/registry/remote/errcode"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
)

const nexusImage = "sonatype/nexus3:3.96.3"

type nexusRepositoryFormat string

const (
	nexusFormatDocker nexusRepositoryFormat = "docker"
	nexusFormatOCI    nexusRepositoryFormat = "oci"
)

// nexusHostedRepository describes a hosted repository with redeploy disabled, listening on
// its own connector.
type nexusHostedRepository struct {
	format    nexusRepositoryFormat
	name      string
	connector string // in-container port the repository listens on
	strict    bool   // strict content type validation
}

var (
	nexusDockerRepository = nexusHostedRepository{format: nexusFormatDocker, name: "docker-hosted", connector: "5000/tcp"}

	// nexusHostedRepositories lists every repository variant. startNexus exposes their connectors.
	nexusHostedRepositories = []nexusHostedRepository{
		{format: nexusFormatOCI, name: "oci-hosted", connector: "5002/tcp"},
		{format: nexusFormatOCI, name: "oci-hosted-strict", connector: "5003/tcp", strict: true},
		nexusDockerRepository,
		{format: nexusFormatDocker, name: "docker-hosted-strict", connector: "5001/tcp", strict: true},
	}
)

// Test_Integration_Nexus_RedeployDisabled drives the OCI uploader against a real
// Nexus "docker (hosted)" repo with redeploy disabled (writePolicy=allow_once),
// the configuration that produced the original 409 "redeploy disabled ... cannot
// be updated as asset already exists". The component-descriptor and OCI artifact
// tag subtests (UploadResource and UploadResourceStream) 409 on re-tag without
// the fix; the ownership subtest confirms referrer attach works end-to-end on
// Nexus (which serves the Referrers API, so redeploy-disabled never bites).
func Test_Integration_Nexus_RedeployDisabled(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Nexus integration test in short mode")
	}
	t.Parallel()
	ctx := t.Context()

	nexus := startNexus(t, ctx)
	resolver, repo, dockerEndpoint := nexus.addHostedRepository(t, ctx, nexusDockerRepository)

	t.Run("component version re-add is idempotent", func(t *testing.T) {
		r := require.New(t)
		desc := barebonesDescriptor("ocm.software/nexus/cv-idempotent", "1.0.0", "acme")

		r.NoError(repo.AddComponentVersion(ctx, desc), "first add must succeed")
		r.NoError(repo.AddComponentVersion(ctx, desc),
			"identical re-add must be idempotent despite redeploy being disabled")
	})

	t.Run("component version changed content surfaces the 409", func(t *testing.T) {
		r := require.New(t)
		const name, version = "ocm.software/nexus/cv-conflict", "1.0.0"

		r.NoError(repo.AddComponentVersion(ctx, barebonesDescriptor(name, version, "acme")))
		err := repo.AddComponentVersion(ctx, barebonesDescriptor(name, version, "other-provider"))
		r.Error(err, "re-tagging changed content on a redeploy-disabled repo must fail")
		r.ErrorContains(err, "failed to tag manifest")

		var resp *errcode.ErrorResponse
		r.ErrorAs(err, &resp)
		r.Equal(http.StatusConflict, resp.StatusCode, "the surfaced error must be the registry 409")
	})

	t.Run("oci artifact re-upload is idempotent", func(t *testing.T) {
		r := require.New(t)
		target := fmt.Sprintf("%s/nexus/artifact:1.0.0", dockerEndpoint)

		data, access := createSingleLayerOCIImage(t, []byte("nexus-artifact-payload"), "ghcr.io/src:1.0.0")
		access.ImageReference = target
		res := &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "artifact", Version: "1.0.0"}},
			Type:        "some-arbitrary-type-packed-in-image",
			Access:      access,
		}

		_, err := repo.UploadResource(ctx, res, inmemory.New(bytes.NewReader(data)))
		r.NoError(err, "first upload must succeed")
		_, err = repo.UploadResource(ctx, res, inmemory.New(bytes.NewReader(data)))
		r.NoError(err, "identical re-upload must be idempotent despite redeploy being disabled")
	})

	t.Run("oci artifact stream re-upload is idempotent", func(t *testing.T) {
		// By-reference ociArtifact transfer goes through UploadResourceStream, a
		// different tag site than UploadResource above.
		r := require.New(t)
		srcRef := fmt.Sprintf("%s/nexus/stream-src:1.0.0", dockerEndpoint)
		dstRef := fmt.Sprintf("%s/nexus/stream-dst:1.0.0", dockerEndpoint)

		src := byReferenceResource("artifact", "1.0.0",
			pushOwnershipByReferenceImage(t, ctx, repo, srcRef, []byte("nexus-stream-payload")))

		upload := func() error {
			stream, err := repo.DownloadResourceStream(ctx, src)
			if err != nil {
				return err
			}
			_, err = repo.UploadResourceStream(ctx, byReferenceResource("artifact", "1.0.0", dstRef), stream)
			return err
		}
		r.NoError(upload(), "first stream upload must succeed")
		r.NoError(upload(), "identical stream re-upload must be idempotent despite redeploy being disabled")
	})

	// End-to-end ownership against Nexus. Nexus serves the Referrers API, so
	// referrer manifests are pushed by digest with server-side discovery and
	// never touch the sha256-<subject> tag schema - redeploy-disabled therefore
	// does not bite, even for a second owner on the same subject (the case that
	// 409s on a no-Referrers-API registry, covered by
	// Test_Integration_WriteOnceNoReferrersAPI_Ownership).
	t.Run("ownership attach works and is idempotent", func(t *testing.T) {
		r := require.New(t)
		const version = "1.0.0"
		subject := byReferenceResource("owned-image", version, pushOwnershipByReferenceImage(t, ctx, repo,
			fmt.Sprintf("%s/nexus/owned-image:%s", dockerEndpoint, version), []byte("nexus-ownership-payload")))

		r.NoError(repo.AddOwnership(ctx, "ocm.software/owner-a", version, subject, nil), "first attach must succeed")
		r.NoError(repo.AddOwnership(ctx, "ocm.software/owner-a", version, subject, nil), "identical re-attach must be idempotent")
		r.NoError(repo.AddOwnership(ctx, "ocm.software/owner-b", version, subject, nil), "a second owner on the same subject must succeed")

		subjectRef := subject.Access.(*v1.OCIImage).ImageReference
		assertOwnershipReferrerCount(t, ctx, resolver, subjectRef, 2)
		assertOwnershipReferrerPresent(t, ctx, resolver, subjectRef, "ocm.software/owner-a", version, "owned-image")
		assertOwnershipReferrerPresent(t, ctx, resolver, subjectRef, "ocm.software/owner-b", version, "owned-image")
	})
}

func barebonesDescriptor(name, version, provider string) *descriptor.Descriptor {
	desc := &descriptor.Descriptor{Meta: descriptor.Meta{Version: "v2"}}
	desc.Component.Name = name
	desc.Component.Version = version
	desc.Component.Provider.Name = provider
	return desc
}

// nexusServer is a running Nexus with the EULA accepted and the Docker and OCI bearer-token realms enabled.
type nexusServer struct {
	container *testcontainers.DockerContainer
	baseURL   string
	password  string
}

// startNexus boots Nexus with all repository connector ports exposed, accepts the
// Community-edition EULA and enables the Docker and OCI bearer-token realms.
func startNexus(t *testing.T, ctx context.Context) *nexusServer {
	t.Helper()
	r := require.New(t)

	ports := []string{"8081/tcp"}
	for _, repository := range nexusHostedRepositories {
		ports = append(ports, repository.connector)
	}

	container, err := testcontainers.Run(ctx, nexusImage,
		testcontainers.WithExposedPorts(ports...),
		testcontainers.WithEnv(map[string]string{
			"INSTALL4J_ADD_VM_PARAMS": "-Xms1g -Xmx1g -XX:MaxDirectMemorySize=1g -Djava.util.prefs.userRoot=/nexus-data/javaprefs",
		}),
		// WithWaitStrategy caps the wait at a 60s deadline regardless of the strategy's
		// own timeout; the Nexus JVM routinely needs longer under CI load, so set the
		// deadline explicitly. Both the deadline and the strategy timeout must allow 5m.
		testcontainers.WithWaitStrategyAndDeadline(5*time.Minute,
			wait.ForHTTP("/service/rest/v1/status/writable").WithPort("8081/tcp").WithStartupTimeout(5*time.Minute),
		),
		testcontainers.WithLogger(log.TestLogger(t)),
	)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(testcontainers.TerminateContainer(container)) })

	apiEndpoint, err := container.PortEndpoint(ctx, "8081/tcp", "")
	r.NoError(err)
	baseURL := "http://" + apiEndpoint

	rc, err := container.CopyFileFromContainer(ctx, "/nexus-data/admin.password")
	r.NoError(err)
	pwRaw, err := io.ReadAll(rc)
	r.NoError(err)
	r.NoError(rc.Close())
	password := string(bytes.TrimSpace(pwRaw))

	// Community edition blocks uploads until the EULA is accepted.
	eulaURL := baseURL + "/service/rest/v1/system/eula"
	status, body := nexusAdminRequest(t, http.MethodGet, eulaURL, password, nil)
	if status != http.StatusNotFound {
		r.Equal(http.StatusOK, status, string(body))
		body = bytes.Replace(body, []byte(`"accepted" : false`), []byte(`"accepted" : true`), 1)
		body = bytes.Replace(body, []byte(`"accepted":false`), []byte(`"accepted":true`), 1)
		status, respBody := nexusAdminRequest(t, http.MethodPost, eulaURL, password, body)
		r.Equal(http.StatusNoContent, status, string(respBody))
	}

	// Clients authenticate via the bearer-token realm of the repository format.
	status, body = nexusAdminRequest(t, http.MethodPut, baseURL+"/service/rest/v1/security/realms/active", password,
		[]byte(`["NexusAuthenticatingRealm","DockerToken","OciBearerToken"]`))
	r.Equal(http.StatusNoContent, status, string(body))

	return &nexusServer{container: container, baseURL: baseURL, password: password}
}

// addHostedRepository creates the hosted repository. It returns a resolver and repository
// wired to its connector, plus the connector endpoint.
func (n *nexusServer) addHostedRepository(t *testing.T, ctx context.Context, repository nexusHostedRepository) (*urlresolver.CachingResolver, *oci.Repository, string) {
	t.Helper()
	r := require.New(t)

	status, body := nexusAdminRequest(t, http.MethodPost, n.baseURL+"/service/rest/v1/repositories/"+string(repository.format)+"/hosted", n.password,
		[]byte(`{"name":"`+repository.name+`","online":true,`+
			`"storage":{"blobStoreName":"default","strictContentTypeValidation":`+strconv.FormatBool(repository.strict)+`,"writePolicy":"allow_once"},`+
			`"`+string(repository.format)+`":{"v1Enabled":false,"forceBasicAuth":true,"httpPort":`+strings.TrimSuffix(repository.connector, "/tcp")+`}}`))
	r.Equal(http.StatusCreated, status, string(body))

	endpoint, err := n.container.PortEndpoint(ctx, repository.connector, "")
	r.NoError(err)

	client := createAuthClient(endpoint, "admin", n.password)

	// The connector comes up asynchronously after the repo is created.
	r.Eventually(func() bool {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+endpoint+"/v2/", nil)
		req.SetBasicAuth("admin", n.password)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, time.Minute, 2*time.Second, "repository connector never became ready")

	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(endpoint),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)

	repo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	return resolver, repo, endpoint
}

func nexusAdminRequest(t *testing.T, method, target, password string, body []byte) (int, []byte) {
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
