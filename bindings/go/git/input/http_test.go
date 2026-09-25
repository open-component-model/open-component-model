package input_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	"ocm.software/open-component-model/bindings/go/git/input"
	inputspec "ocm.software/open-component-model/bindings/go/git/spec/input"
	inputv1 "ocm.software/open-component-model/bindings/go/git/spec/input/v1"
)

func TestProcessResourceUsesHTTPClient(t *testing.T) {
	r := require.New(t)
	var reached atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		reached.Store(true)
		<-req.Context().Done()
	}))
	t.Cleanup(server.Close)
	method := &input.InputMethod{
		TempFolder: t.TempDir(),
		HTTPClient: &http.Client{Timeout: 100 * time.Millisecond},
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	result, err := method.ProcessResource(ctx, &constructorruntime.Resource{Input: &inputv1.Git{
		Type: inputspec.V1VersionedType, Repository: server.URL + "/repo.git",
	}}, nil)
	r.ErrorContains(err, "Client.Timeout")
	r.Nil(result)
	r.True(reached.Load())
	r.NoError(ctx.Err(), "the configured client timeout must fire before the caller deadline")
}
