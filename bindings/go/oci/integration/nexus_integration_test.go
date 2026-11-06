package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/registry/remote/errcode"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/internal/testutil/nexustest"
	"ocm.software/open-component-model/bindings/go/internal/testutil/registrytest"
	"ocm.software/open-component-model/bindings/go/oci"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
)

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

	nexus := startNexus(t)
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
	nexustest.Nexus
}

// startNexus boots Nexus with all repository connector ports exposed and enables the
// Docker and OCI bearer-token realms.
func startNexus(t *testing.T) *nexusServer {
	t.Helper()

	var ports []string
	for _, repository := range nexusHostedRepositories {
		ports = append(ports, repository.connector)
	}
	nexus := nexustest.Start(t, nexustest.WithExposedPorts(ports...))

	// Clients authenticate via the bearer-token realm of the repository format.
	status, body := nexustest.Request(t, http.MethodPut, nexus.BaseURL+"/service/rest/v1/security/realms/active", nexus.Password,
		[]byte(`["NexusAuthenticatingRealm","DockerToken","OciBearerToken"]`))
	require.Equal(t, http.StatusNoContent, status, string(body))

	return &nexusServer{Nexus: nexus}
}

// addHostedRepository creates the hosted repository. It returns a resolver and repository
// wired to its connector, plus the connector endpoint.
func (n *nexusServer) addHostedRepository(t *testing.T, ctx context.Context, repository nexusHostedRepository) (*urlresolver.CachingResolver, *oci.Repository, string) {
	t.Helper()
	r := require.New(t)

	status, body := nexustest.Request(t, http.MethodPost, n.BaseURL+"/service/rest/v1/repositories/"+string(repository.format)+"/hosted", n.Password,
		[]byte(`{"name":"`+repository.name+`","online":true,`+
			`"storage":{"blobStoreName":"default","strictContentTypeValidation":`+strconv.FormatBool(repository.strict)+`,"writePolicy":"allow_once"},`+
			`"`+string(repository.format)+`":{"v1Enabled":false,"forceBasicAuth":true,"httpPort":`+strings.TrimSuffix(repository.connector, "/tcp")+`}}`))
	r.Equal(http.StatusCreated, status, string(body))

	endpoint, err := n.Container.PortEndpoint(ctx, repository.connector, "")
	r.NoError(err)

	client := registrytest.AuthClient(endpoint, "admin", n.Password)

	// The connector comes up asynchronously after the repo is created.
	r.Eventually(func() bool {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+endpoint+"/v2/", nil)
		req.SetBasicAuth("admin", n.Password)
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
