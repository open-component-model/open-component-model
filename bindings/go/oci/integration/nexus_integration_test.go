package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
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
)

const (
	nexusImage            = "sonatype/nexus3:3.96.3"
	nexusDockerConnector  = "5000/tcp" // in-container port the docker hosted repo listens on
	nexusDockerRepository = "docker-hosted"
)

// Test_Integration_Nexus_RedeployDisabled drives the OCI uploader paths against
// a real Nexus "docker (hosted)" repository with redeploy disabled
// (storage.writePolicy = allow_once), the exact configuration that surfaced the
// original 409 "redeploy disabled ... cannot be updated as asset already
// exists" failure. It locks the idempotency contract for the tag write sites
// the fix touches: the component-descriptor tag (AddComponentVersion) and the
// re-materialised OCI artifact tag (UploadResource -> uploadOCIImage), both of
// which 409 on re-tag without the fix. The ownership referrer attach
// (AddOwnership) is included as a positive idempotency check (see the note on
// that subtest for why Nexus does not 409 there).
func Test_Integration_Nexus_RedeployDisabled(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Nexus integration test in short mode")
	}
	t.Parallel()
	ctx := t.Context()

	resolver, repo, dockerEndpoint, _ := startNexusDockerHosted(t, ctx)

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

	t.Run("ownership referrer re-attach is idempotent", func(t *testing.T) {
		// Unlike the tag sites above, this does not reproduce the redeploy 409 on
		// Nexus: the ownership referrer is pushed by digest (no human tag), and
		// Nexus accepts a by-digest re-push of immutable content. It guards the
		// positive idempotency contract end-to-end; the write-once-by-digest case
		// that pushIdempotent actually protects (and the referrers-tag-schema
		// fallback on registries without the Referrers API) is covered by the
		// TestRepository_AddOwnership_ImmutableRegistry unit test.
		r := require.New(t)
		const component, version = "ocm.software/nexus/owned", "1.0.0"
		imageRef := fmt.Sprintf("%s/nexus/owned-image:%s", dockerEndpoint, version)

		subject := pushOwnershipByReferenceImage(t, ctx, repo, imageRef, []byte("nexus-ownership-payload"))
		res := byReferenceResource("owned-image", version, subject)

		r.NoError(repo.AddOwnership(ctx, component, version, res, nil), "first attach must succeed")
		r.NoError(repo.AddOwnership(ctx, component, version, res, nil),
			"identical re-attach must be idempotent despite redeploy being disabled")

		assertOwnershipReferrerCount(t, ctx, resolver, subject, 1)
	})
}

func barebonesDescriptor(name, version, provider string) *descriptor.Descriptor {
	desc := &descriptor.Descriptor{Meta: descriptor.Meta{Version: "v2"}}
	desc.Component.Name = name
	desc.Component.Version = version
	desc.Component.Provider.Name = provider
	return desc
}

// startNexusDockerHosted boots Nexus, accepts the Community-edition EULA, enables
// the Docker bearer-token realm, and creates a docker (hosted) repository with
// redeploy disabled. It returns a resolver and repository wired to that repo's
// docker connector, plus the connector endpoint and admin password.
func startNexusDockerHosted(t *testing.T, ctx context.Context) (*urlresolver.CachingResolver, *oci.Repository, string, string) {
	t.Helper()
	r := require.New(t)

	container, err := testcontainers.Run(ctx, nexusImage,
		testcontainers.WithExposedPorts("8081/tcp", nexusDockerConnector),
		testcontainers.WithEnv(map[string]string{
			"INSTALL4J_ADD_VM_PARAMS": "-Xms1g -Xmx1g -XX:MaxDirectMemorySize=1g -Djava.util.prefs.userRoot=/nexus-data/javaprefs",
		}),
		testcontainers.WithWaitStrategy(
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

	// Docker clients authenticate via the bearer-token realm.
	status, body = nexusAdminRequest(t, http.MethodPut, baseURL+"/service/rest/v1/security/realms/active", password,
		[]byte(`["NexusAuthenticatingRealm","DockerToken"]`))
	r.Equal(http.StatusNoContent, status, string(body))

	// A docker hosted repo with redeploy disabled, listening on its own connector.
	connectorPort := nexusDockerConnector[:len(nexusDockerConnector)-len("/tcp")]
	status, body = nexusAdminRequest(t, http.MethodPost, baseURL+"/service/rest/v1/repositories/docker/hosted", password,
		[]byte(`{"name":"`+nexusDockerRepository+`","online":true,`+
			`"storage":{"blobStoreName":"default","strictContentTypeValidation":false,"writePolicy":"allow_once"},`+
			`"docker":{"v1Enabled":false,"forceBasicAuth":true,"httpPort":`+connectorPort+`}}`))
	r.Equal(http.StatusCreated, status, string(body))

	dockerEndpoint, err := container.PortEndpoint(ctx, nexusDockerConnector, "")
	r.NoError(err)

	client := createAuthClient(dockerEndpoint, "admin", password)

	// The docker connector comes up asynchronously after the repo is created.
	r.Eventually(func() bool {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+dockerEndpoint+"/v2/", nil)
		req.SetBasicAuth("admin", password)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, time.Minute, 2*time.Second, "docker connector never became ready")

	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(dockerEndpoint),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)

	repo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	return resolver, repo, dockerEndpoint, password
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
