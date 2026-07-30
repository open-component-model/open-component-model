package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	wgetrepository "ocm.software/open-component-model/bindings/go/wget/repository"
)

// Test_Integration_WgetAccessDigestVerification drives a wget access resource through the
// real constructor with a digest processor. A matching, author-supplied digest must pass
// and be recorded; a mismatching one must fail construction.
func Test_Integration_WgetAccessDigestVerification(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	content := []byte("wget-access-content")
	sum := sha256.Sum256(content)
	correctDigest := hex.EncodeToString(sum[:])

	newServer := func(t *testing.T) *httptest.Server {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(content)
		}))
		t.Cleanup(srv.Close)
		return srv
	}

	t.Run("matching digest passes and is recorded", func(t *testing.T) {
		r := require.New(t)
		srv := newServer(t)

		repo := constructWgetAccess(t, fmt.Sprintf(`
components:
  - name: ocm.software/wget-app
    version: v1.0.0
    provider:
      name: ocm
    resources:
      - name: remote
        version: v1.0.0
        relation: external
        type: blob
        access:
          type: Wget/v1
          url: %s/file.bin
          mediaType: application/octet-stream
        digest:
          hashAlgorithm: SHA-256
          normalisationAlgorithm: genericBlobDigest/v1
          value: "%s"
`, srv.URL, correctDigest))

		desc, err := repo.GetComponentVersion(ctx, "ocm.software/wget-app", "v1.0.0")
		r.NoError(err)
		r.Len(desc.Component.Resources, 1)
		res := desc.Component.Resources[0]
		r.NotNil(res.Digest)
		r.Equal(correctDigest, res.Digest.Value)
	})

	t.Run("mismatching digest fails construction", func(t *testing.T) {
		r := require.New(t)
		srv := newServer(t)

		_, err := constructWgetAccessErr(t, fmt.Sprintf(`
components:
  - name: ocm.software/wget-app
    version: v1.0.0
    provider:
      name: ocm
    resources:
      - name: remote
        version: v1.0.0
        relation: external
        type: blob
        access:
          type: Wget/v1
          url: %s/file.bin
          mediaType: application/octet-stream
        digest:
          hashAlgorithm: SHA-256
          normalisationAlgorithm: genericBlobDigest/v1
          value: "0000000000000000000000000000000000000000000000000000000000000000"
`, srv.URL))
		r.Error(err)
		r.ErrorContains(err, "digest mismatch")
	})
}

// constructWgetAccess runs the constructor for a wget access resource with a digest
// processor and returns the target repository. It fails the test if construction errors.
func constructWgetAccess(t *testing.T, yamlData string) *inMemoryTargetRepository {
	t.Helper()
	repo, err := constructWgetAccessErr(t, yamlData)
	require.NoError(t, err)
	return repo
}

// constructWgetAccessErr is like constructWgetAccess but returns the construction error.
func constructWgetAccessErr(t *testing.T, yamlData string) (*inMemoryTargetRepository, error) {
	t.Helper()
	r := require.New(t)

	var spec constructorv1.ComponentConstructor
	r.NoError(yaml.Unmarshal([]byte(yamlData), &spec))

	repo := newInMemoryTargetRepository()
	err := constructor.NewDefaultConstructor(
		constructorruntime.ConvertToRuntimeConstructor(&spec),
		constructor.Options{
			TargetRepositoryProvider:        inMemoryTargetProvider{repo: repo},
			ResourceDigestProcessorProvider: wgetDigestProvider{},
		},
	).Construct(context.Background())

	return repo, err
}

// wgetDigestProvider resolves every resource to the wget resource repository as digest processor.
type wgetDigestProvider struct{}

func (wgetDigestProvider) GetDigestProcessor(_ context.Context, _ *descriptor.Resource) (constructor.ResourceDigestProcessor, error) {
	return wgetrepository.NewResourceRepository(nil), nil
}
