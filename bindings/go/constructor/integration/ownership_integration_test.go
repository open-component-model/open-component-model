package integration_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	"ocm.software/open-component-model/bindings/go/ctf"
	ocmoci "ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	ocitar "ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const ownershipYAML = `
components:
  - name: ocm.software/owned
    version: v1.0.0
    provider:
      name: ocm
    resources:
      - name: data
        version: v1.0.0
        relation: local
        type: ociArtifact
        options:
          ownershipPolicy: Always
        input:
          type: blob/v1
`

func Test_Integration_OCI_OwnershipPolicy_Always(t *testing.T) {
	t.Parallel()
	runOwnershipConstruct(t, launchRegistryOCIRepository(t))
}

func Test_Integration_CTF_OwnershipPolicy_Always(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo, err := ocmoci.NewRepository(ocmoci.WithResolver(store), ocmoci.WithTempDir(t.TempDir()))
	r.NoError(err)

	runOwnershipConstruct(t, repo)
}

func runOwnershipConstruct(t *testing.T, repo *ocmoci.Repository) {
	t.Helper()
	ctx := context.Background()
	r := require.New(t)

	_, ok := repository.ComponentVersionRepository(repo).(repository.OwnershipAwareRepository)
	r.True(ok, "OCI repository must implement OwnershipAwareRepository")

	var spec constructorv1.ComponentConstructor
	r.NoError(yaml.Unmarshal([]byte(ownershipYAML), &spec))
	converted := constructorruntime.ConvertToRuntimeConstructor(&spec)

	opts := constructor.Options{
		ResourceInputMethodProvider: ociLayoutBlobInputProvider{t: t},
		TargetRepositoryProvider:    ownershipTargetRepositoryProvider{repo: repo},
	}
	r.NoError(constructor.NewDefaultConstructor(converted, opts).Construct(ctx))

	desc, err := repo.GetComponentVersion(ctx, "ocm.software/owned", "v1.0.0")
	r.NoError(err)
	r.Len(desc.Component.Resources, 1)
	r.Equal("data", desc.Component.Resources[0].Name)
}

func launchRegistryOCIRepository(t *testing.T) *ocmoci.Repository {
	t.Helper()
	cvr := launchRegistryRepository(t)
	repo, ok := cvr.(*ocmoci.Repository)
	require.True(t, ok, "launchRegistryRepository must return *ocmoci.Repository")
	return repo
}

type ownershipTargetRepositoryProvider struct {
	repo *ocmoci.Repository
}

func (p ownershipTargetRepositoryProvider) GetTargetRepository(_ context.Context, _ *constructorruntime.Component) (constructor.TargetRepository, error) {
	return p.repo, nil
}

type ociLayoutBlobInputProvider struct {
	t *testing.T
}

func (p ociLayoutBlobInputProvider) GetResourceInputMethod(_ context.Context, _ *constructorruntime.Resource) (constructor.ResourceInputMethod, error) {
	return ociLayoutBlobInputMethod{t: p.t}, nil
}

type ociLayoutBlobInputMethod struct {
	t *testing.T
}

func (ociLayoutBlobInputMethod) GetResourceCredentialConsumerIdentity(_ context.Context, _ *constructorruntime.Resource) (runtime.Identity, error) {
	return nil, nil
}

func (m ociLayoutBlobInputMethod) ProcessResource(_ context.Context, resource *constructorruntime.Resource, _ runtime.Typed) (*constructor.ResourceInputMethodResult, error) {
	payload := []byte("payload-for-" + resource.ElementMeta.ToIdentity().String())
	data := singleLayerOCILayoutTarGzip(m.t, payload)
	return &constructor.ResourceInputMethodResult{
		ProcessedBlobData: inmemory.New(bytes.NewReader(data), inmemory.WithMediaType(layout.MediaTypeOCIImageLayoutTarGzipV1)),
	}, nil
}

func singleLayerOCILayoutTarGzip(t *testing.T, layerData []byte) []byte {
	t.Helper()
	r := require.New(t)

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	w, err := ocitar.NewOCILayoutWriterWithTempFile(gz, t.TempDir())
	r.NoError(err)

	ctx := context.Background()

	layerDesc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageLayer,
		Digest:    digest.FromBytes(layerData),
		Size:      int64(len(layerData)),
	}
	r.NoError(w.Push(ctx, layerDesc, bytes.NewReader(layerData)))

	configRaw, err := json.Marshal(map[string]string{})
	r.NoError(err)
	configDesc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageConfig,
		Digest:    digest.FromBytes(configRaw),
		Size:      int64(len(configRaw)),
	}
	r.NoError(w.Push(ctx, configDesc, bytes.NewReader(configRaw)))

	manifest := ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config:    configDesc,
		Layers:    []ociImageSpecV1.Descriptor{layerDesc},
	}
	manifestRaw, err := json.Marshal(manifest)
	r.NoError(err)
	manifestDesc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromBytes(manifestRaw),
		Size:      int64(len(manifestRaw)),
	}
	r.NoError(w.Push(ctx, manifestDesc, bytes.NewReader(manifestRaw)))

	r.NoError(w.Close())
	r.NoError(gz.Close())
	return buf.Bytes()
}
