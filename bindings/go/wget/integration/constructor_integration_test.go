package integration_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	wgetinput "ocm.software/open-component-model/bindings/go/wget/input"
)

// Test_Integration_WgetInputConstruction drives the wget input method through the real
// constructor pipeline: it downloads a resource from an HTTP server and verifies the
// bytes are stored as a local blob on the constructed component version.
func Test_Integration_WgetInputConstruction(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	content := []byte("wget-input-content")

	t.Run("downloads url as local blob", func(t *testing.T) {
		r := require.New(t)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(content)
		}))
		t.Cleanup(srv.Close)

		repo := constructWget(t, fmt.Sprintf(`
components:
  - name: ocm.software/wget-app
    version: v1.0.0
    provider:
      name: ocm
    resources:
      - name: remote-blob
        version: v1.0.0
        relation: local
        type: blob
        input:
          type: wget
          url: %s/file.bin
          mediaType: application/custom
`, srv.URL))

		desc, err := repo.GetComponentVersion(ctx, "ocm.software/wget-app", "v1.0.0")
		r.NoError(err)
		r.Len(desc.Component.Resources, 1)
		res := desc.Component.Resources[0]
		r.Equal("remote-blob", res.Name)

		data, ok := repo.localResourceData(res)
		r.True(ok, "resource must be stored as a local blob")
		r.Equal(content, readAll(t, data))
	})

	t.Run("applies basic auth credentials from url userinfo", func(t *testing.T) {
		r := require.New(t)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			u, p, ok := req.BasicAuth()
			if !ok || u != "user" || p != "pass" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			_, _ = w.Write(content)
		}))
		t.Cleanup(srv.Close)

		// userinfo in the URL is passed through the standard http client into the request.
		authURL := fmt.Sprintf("http://user:pass@%s/file.bin", srv.Listener.Addr().String())
		repo := constructWget(t, fmt.Sprintf(`
components:
  - name: ocm.software/wget-app
    version: v1.0.0
    provider:
      name: ocm
    resources:
      - name: remote-blob
        version: v1.0.0
        relation: local
        type: blob
        input:
          type: wget
          url: %s
`, authURL))

		desc, err := repo.GetComponentVersion(ctx, "ocm.software/wget-app", "v1.0.0")
		r.NoError(err)
		res := desc.Component.Resources[0]
		data, ok := repo.localResourceData(res)
		r.True(ok)
		r.Equal(content, readAll(t, data))
	})

	t.Run("fails construction when server returns an error", func(t *testing.T) {
		r := require.New(t)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "gone", http.StatusNotFound)
		}))
		t.Cleanup(srv.Close)

		var spec constructorv1.ComponentConstructor
		r.NoError(yaml.Unmarshal([]byte(fmt.Sprintf(`
components:
  - name: ocm.software/wget-app
    version: v1.0.0
    provider:
      name: ocm
    resources:
      - name: remote-blob
        version: v1.0.0
        relation: local
        type: blob
        input:
          type: wget
          url: %s/missing.bin
`, srv.URL)), &spec))

		err := constructor.NewDefaultConstructor(
			constructorruntime.ConvertToRuntimeConstructor(&spec),
			constructor.Options{
				ResourceInputMethodProvider: wgetInputProvider{},
				TargetRepositoryProvider:    inMemoryTargetProvider{repo: newInMemoryTargetRepository()},
			},
		).Construct(ctx)
		r.Error(err)
		r.Contains(err.Error(), "404")
	})
}

// constructWget parses the given component constructor YAML, runs it through the real
// constructor with the wget input method against an in-memory target repository, and
// returns that repository for inspection.
func constructWget(t *testing.T, yamlData string) *inMemoryTargetRepository {
	t.Helper()
	r := require.New(t)

	var spec constructorv1.ComponentConstructor
	r.NoError(yaml.Unmarshal([]byte(yamlData), &spec))

	repo := newInMemoryTargetRepository()
	r.NoError(constructor.NewDefaultConstructor(
		constructorruntime.ConvertToRuntimeConstructor(&spec),
		constructor.Options{
			ResourceInputMethodProvider: wgetInputProvider{},
			TargetRepositoryProvider:    inMemoryTargetProvider{repo: repo},
		},
	).Construct(context.Background()))

	return repo
}

func readAll(t *testing.T, b blob.ReadOnlyBlob) []byte {
	t.Helper()
	rc, err := b.ReadCloser()
	require.NoError(t, err)
	defer func() { require.NoError(t, rc.Close()) }()
	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	return data
}

// wgetInputProvider resolves every resource input to the wget input method.
type wgetInputProvider struct{}

func (wgetInputProvider) GetResourceInputMethod(_ context.Context, _ *constructorruntime.Resource) (constructor.ResourceInputMethod, error) {
	return &wgetinput.InputMethod{}, nil
}

// inMemoryTargetProvider hands the same in-memory repository to every component.
type inMemoryTargetProvider struct {
	repo *inMemoryTargetRepository
}

func (p inMemoryTargetProvider) GetTargetRepository(_ context.Context, _ *constructorruntime.Component) (constructor.TargetRepository, error) {
	return p.repo, nil
}

// inMemoryTargetRepository is a minimal constructor.TargetRepository that records
// component versions and the local blob data uploaded for each resource.
type inMemoryTargetRepository struct {
	mu                sync.Mutex
	components        map[string]*descriptor.Descriptor
	localResourceBlob map[string]blob.ReadOnlyBlob
}

func newInMemoryTargetRepository() *inMemoryTargetRepository {
	return &inMemoryTargetRepository{
		components:        make(map[string]*descriptor.Descriptor),
		localResourceBlob: make(map[string]blob.ReadOnlyBlob),
	}
}

func (m *inMemoryTargetRepository) localResourceData(res descriptor.Resource) (blob.ReadOnlyBlob, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.localResourceBlob[res.ToIdentity().String()]
	return data, ok
}

func (m *inMemoryTargetRepository) GetComponentVersion(_ context.Context, name, version string) (*descriptor.Descriptor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if desc, ok := m.components[name+":"+version]; ok {
		return desc, nil
	}
	return nil, fmt.Errorf("component version %q not found: %w", name+":"+version, repository.ErrNotFound)
}

func (m *inMemoryTargetRepository) AddLocalResource(_ context.Context, _, _ string, resource *descriptor.Resource, data blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.localResourceBlob[resource.ToIdentity().String()] = data
	return resource, nil
}

func (m *inMemoryTargetRepository) AddLocalSource(_ context.Context, _, _ string, source *descriptor.Source, _ blob.ReadOnlyBlob) (*descriptor.Source, error) {
	return source, nil
}

func (m *inMemoryTargetRepository) AddComponentVersion(_ context.Context, desc *descriptor.Descriptor) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.components[desc.Component.Name+":"+desc.Component.Version] = desc
	return nil
}
