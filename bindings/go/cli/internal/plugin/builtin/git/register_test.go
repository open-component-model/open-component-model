package git

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing/transport"
	gitclient "github.com/go-git/go-git/v5/plumbing/transport/client"
	"github.com/stretchr/testify/require"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	gitrepository "ocm.software/open-component-model/bindings/go/git/repository"
	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/digestprocessor"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/resource"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestRegister(t *testing.T) {
	r := require.New(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	previousHTTP, previousHTTPS := gitclient.Protocols["http"], gitclient.Protocols["https"]
	t.Cleanup(func() {
		gitclient.Protocols["http"], gitclient.Protocols["https"] = previousHTTP, previousHTTPS
	})

	resources := resource.NewResourceRegistry(ctx)
	maxRetries := -1
	r.NoError(Register(
		resources,
		digestprocessor.NewDigestProcessorRegistry(ctx),
		credentialrepository.NewCredentialRepositoryRegistry(ctx),
		&filesystemv1alpha1.Config{},
		&httpv1alpha1.Config{Retry: &httpv1alpha1.RetryConfig{MaxRetries: &maxRetries}},
	))

	for _, typ := range []runtime.Type{
		runtime.NewVersionedType(accessv1.Type, accessv1.Version),
		runtime.NewVersionedType(accessv1.LegacyType, "v1alpha1"),
	} {
		plugin, err := resources.GetResourcePlugin(ctx, &accessv1.Git{Type: typ, Repository: "https://example.com/repo.git", Ref: "main"})
		r.NoError(err, typ.String())
		r.IsType(&gitrepository.ResourceRepository{}, plugin, typ.String())
	}

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	endpoint, err := transport.NewEndpoint(server.URL + "/repo.git")
	r.NoError(err)
	installed, err := gitclient.NewClient(endpoint)
	r.NoError(err)
	r.Same(installed, gitclient.Protocols["https"])
	session, err := installed.NewUploadPackSession(endpoint, nil)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(session.Close()) })

	_, err = session.AdvertisedReferencesContext(ctx)
	r.ErrorContains(err, "503")
	r.Equal(int32(1), requests.Load(), "configured client must not retry HTTP 503 responses")
}
