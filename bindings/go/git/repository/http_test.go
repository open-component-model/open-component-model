package repository_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/git/repository"
	accessv1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestResourceRepositoryHTTPConfigIsolated(t *testing.T) {
	r := require.New(t)

	var discoveries atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/repo.git/info/refs" {
			discoveries.Add(1)
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	noRetry := -1
	newRepo := func(insecure bool) *repository.ResourceRepository {
		dir := t.TempDir()
		return repository.NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &dir}, repository.WithHTTPConfig(&httpv1alpha1.Config{
			TLSConfig: httpv1alpha1.TLSConfig{InsecureSkipVerify: &insecure},
			Retry:     &httpv1alpha1.RetryConfig{MaxRetries: &noRetry},
		}))
	}
	// Both repositories address the same URL; only the insecure one may trust the test certificate.
	repos := map[bool]*repository.ResourceRepository{true: newRepo(true), false: newRepo(false)}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	const rounds = 5
	var wg sync.WaitGroup
	errs := make(chan error, 2*rounds)
	for range rounds {
		for insecure, repo := range repos {
			wg.Go(func() {
				res := &descriptor.Resource{Access: &accessv1.Git{
					Type: runtime.NewVersionedType("Git", "v1"), Repository: server.URL + "/repo.git", Ref: "HEAD",
				}}
				_, err := repo.DownloadResource(ctx, res, nil)
				switch {
				case err == nil:
					errs <- fmt.Errorf("insecure=%t: download of a missing repository succeeded", insecure)
				case insecure && strings.Contains(err.Error(), "certificate"):
					errs <- fmt.Errorf("insecure=%t: TLS verification leaked from the other repository: %w", insecure, err)
				case !insecure && !strings.Contains(err.Error(), "certificate"):
					errs <- fmt.Errorf("insecure=%t: TLS verification was skipped: %w", insecure, err)
				}
			})
		}
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		r.NoError(err)
	}
	r.Equal(int32(rounds), discoveries.Load(), "only the insecure repository may reach the server")
}
