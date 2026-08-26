package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/log"
	"github.com/testcontainers/testcontainers-go/modules/registry"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ocmoci "ocm.software/open-component-model/bindings/go/oci"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const distributionRegistryImage = "registry:3.0.0"

// Test_Integration_ConstructSameNamedReferences is a very simple test
// to test construct. It creates two top level components that share
// a reference name "lib" but point at different components.
func Test_Integration_ConstructSameNamedReferences(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := require.New(t)

	repo := launchRegistryRepository(t)

	const yamlData = `
components:
  - name: ocm.software/app-a
    version: v1.0.0
    provider:
      name: ocm
    resources:
      - name: data
        version: v1.0.0
        relation: local
        type: blob
        input:
          type: blob/v1
    componentReferences:
      - name: lib
        version: v1.0.0
        componentName: ocm.software/lib-a
  - name: ocm.software/app-b
    version: v1.0.0
    provider:
      name: ocm
    resources:
      - name: data
        version: v1.0.0
        relation: local
        type: blob
        input:
          type: blob/v1
    componentReferences:
      - name: lib
        version: v1.0.0
        componentName: ocm.software/lib-b
  - name: ocm.software/lib-a
    version: v1.0.0
    provider:
      name: ocm
    resources:
      - name: data
        version: v1.0.0
        relation: local
        type: blob
        input:
          type: blob/v1
  - name: ocm.software/lib-b
    version: v1.0.0
    provider:
      name: ocm
    resources:
      - name: data
        version: v2.0.0
        relation: local
        type: blob
        input:
          type: blob/v1
`

	var spec constructorv1.ComponentConstructor
	r.NoError(yaml.Unmarshal([]byte(yamlData), &spec))
	converted := constructorruntime.ConvertToRuntimeConstructor(&spec)

	opts := constructor.Options{
		ResourceInputMethodProvider: blobInputProvider{},
		TargetRepositoryProvider:    targetRepositoryProvider{repo: repo},
	}
	r.NoError(constructor.NewDefaultConstructor(converted, opts).Construct(ctx))

	appA, err := repo.GetComponentVersion(ctx, "ocm.software/app-a", "v1.0.0")
	r.NoError(err)
	appB, err := repo.GetComponentVersion(ctx, "ocm.software/app-b", "v1.0.0")
	r.NoError(err)

	refA := referenceByName(t, appA, "lib")
	refB := referenceByName(t, appB, "lib")

	r.Equal("ocm.software/lib-a", refA.Component)
	r.Equal("ocm.software/lib-b", refB.Component)

	r.NotEmpty(refA.Digest.Value, "reference to lib-a must carry a digest")
	r.NotEmpty(refB.Digest.Value, "reference to lib-b must carry a digest")

	r.NotEqual(refA.Digest.Value, refB.Digest.Value,
		"references named %q must resolve to distinct digests for distinct components", "lib")
}

// launchRegistryRepository starts a containerized OCI distribution registry and
// returns a component version repository targeting it over plain HTTP.
func launchRegistryRepository(t *testing.T) repository.ComponentVersionRepository {
	t.Helper()
	repo, _ := launchRegistryRepositoryWithResolver(t)
	return repo
}

// launchRegistryRepositoryWithResolver is like [launchRegistryRepository] but
// also returns the [ocmoci.Resolver] backing the repository, so callers can
// inspect the underlying OCI store (e.g. to list referrers).
func launchRegistryRepositoryWithResolver(t *testing.T) (repository.ComponentVersionRepository, ocmoci.Resolver) {
	t.Helper()
	ctx := context.Background()
	r := require.New(t)

	t.Logf("Launching test registry (%s)...", distributionRegistryImage)
	registryContainer, err := registry.Run(ctx, distributionRegistryImage,
		testcontainers.WithEnv(map[string]string{
			"REGISTRY_VALIDATION_DISABLED": "true",
		}),
		testcontainers.WithLogger(log.TestLogger(t)),
	)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(testcontainers.TerminateContainer(registryContainer))
	})

	address, err := registryContainer.HostAddress(ctx)
	r.NoError(err)

	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(address),
		urlresolver.WithPlainHTTP(true),
	)
	r.NoError(err)

	repo, err := ocmoci.NewRepository(ocmoci.WithResolver(resolver), ocmoci.WithTempDir(t.TempDir()))
	r.NoError(err)
	return repo, resolver
}

func referenceByName(t *testing.T, desc *descriptor.Descriptor, name string) descriptor.Reference {
	t.Helper()
	for _, ref := range desc.Component.References {
		if ref.Name == name {
			return ref
		}
	}
	t.Fatalf("reference %q not found in component %q", name, desc.Component.Name)
	return descriptor.Reference{}
}

// blobInputProvider resolves every resource input to blobInputMethod.
type blobInputProvider struct{}

func (blobInputProvider) GetResourceInputMethod(_ context.Context, _ *constructorruntime.Resource) (constructor.ResourceInputMethod, error) {
	return blobInputMethod{}, nil
}

// blobInputMethod synthesizes deterministic local blob content keyed by the
// resource identity so that lib-a and lib-b carry distinct content and digests.
type blobInputMethod struct{}

func (blobInputMethod) GetResourceCredentialConsumerIdentity(_ context.Context, _ *constructorruntime.Resource) (runtime.Identity, error) {
	return nil, nil
}

func (blobInputMethod) ProcessResource(_ context.Context, resource *constructorruntime.Resource, _ runtime.Typed) (*constructor.ResourceInputMethodResult, error) {
	content := []byte(fmt.Sprintf("content-for-%s", resource.ElementMeta.ToIdentity().String()))
	return &constructor.ResourceInputMethodResult{
		ProcessedBlobData: inmemory.New(bytes.NewReader(content), inmemory.WithMediaType("application/octet-stream")),
	}, nil
}

// targetRepositoryProvider hands the same registry-backed repository to every component.
type targetRepositoryProvider struct {
	repo repository.ComponentVersionRepository
}

func (p targetRepositoryProvider) GetTargetRepository(_ context.Context, _ *constructorruntime.Component) (constructor.TargetRepository, error) {
	return targetRepository{repo: p.repo}, nil
}

// targetRepository adapts a ComponentVersionRepository to constructor.TargetRepository.
type targetRepository struct {
	repo repository.ComponentVersionRepository
}

func (t targetRepository) GetComponentVersion(ctx context.Context, name, version string) (*descriptor.Descriptor, error) {
	return t.repo.GetComponentVersion(ctx, name, version)
}

func (t targetRepository) AddComponentVersion(ctx context.Context, desc *descriptor.Descriptor) error {
	return t.repo.AddComponentVersion(ctx, desc)
}

func (t targetRepository) AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	return t.repo.AddLocalResource(ctx, component, version, res, content)
}

func (t targetRepository) AddLocalSource(ctx context.Context, component, version string, src *descriptor.Source, content blob.ReadOnlyBlob) (*descriptor.Source, error) {
	return t.repo.AddLocalSource(ctx, component, version, src, content)
}
