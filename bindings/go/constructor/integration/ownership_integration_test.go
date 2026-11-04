package integration_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"
	orasregistry "oras.land/oras-go/v2/registry"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ocmoci "ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	ocitar "ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Test_Integration_OCI_OwnershipPolicy_Always(t *testing.T) {
	t.Parallel()
	repo, resolver := launchRegistryOCIRepository(t)
	runOwnershipConstruct(t, repo, resolver)
}

func Test_Integration_CTF_OwnershipPolicy_Always(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo, err := ocmoci.NewRepository(ocmoci.WithResolver(store), ocmoci.WithTempDir(t.TempDir()))
	r.NoError(err)

	runOwnershipConstruct(t, repo, store)
}

func runOwnershipConstruct(t *testing.T, repo *ocmoci.Repository, resolver ocmoci.Resolver) {
	t.Helper()
	ctx := context.Background()
	r := require.New(t)

	_, ok := repository.ComponentVersionRepository(repo).(repository.OwnershipAwareRepository)
	r.True(ok, "OCI repository must implement OwnershipAwareRepository")

	workingDir := t.TempDir()
	imagePath := filepath.Join(workingDir, "image.tar.gz")
	r.NoError(os.WriteFile(imagePath, singleLayerOCILayoutTarGzip(t, []byte("payload")), 0o600))

	const (
		componentName    = "ocm.software/owned"
		componentVersion = "v1.0.0"
	)
	specYAML := []byte(`
components:
  - name: ` + componentName + `
    version: ` + componentVersion + `
    provider:
      name: ocm
    resources:
      - name: data
        version: ` + componentVersion + `
        relation: local
        type: ociArtifact
        options:
          ownershipPolicy: Always
        input:
          type: testinput/v1
          path: image.tar.gz
          mediaType: ` + layout.MediaTypeOCIImageLayoutTarGzipV1 + `
`)
	var spec constructorv1.ComponentConstructor
	r.NoError(yaml.Unmarshal(specYAML, &spec))
	converted := constructorruntime.ConvertToRuntimeConstructor(&spec)

	inputScheme := newLocalFileInputScheme()
	registry := constructor.New(inputScheme)
	registry.MustRegisterResourceInputMethod(&localFileInput{}, &localFileInputMethod{scheme: inputScheme, workingDir: workingDir})

	opts := constructor.Options{
		ResourceInputMethodProvider: registry,
		TargetRepositoryProvider:    ownershipTargetRepositoryProvider{repo: repo},
	}
	r.NoError(constructor.NewDefaultConstructor(converted, opts).Construct(ctx))

	desc, err := repo.GetComponentVersion(ctx, componentName, componentVersion)
	r.NoError(err)
	r.Len(desc.Component.Resources, 1)
	r.Equal("data", desc.Component.Resources[0].Name)

	localBlob := &descriptorv2.LocalBlob{}
	r.NoError(descriptorv2.Scheme.Convert(desc.Component.Resources[0].Access, localBlob))
	r.NotEmpty(localBlob.LocalReference, "uploaded resource must carry a non-empty LocalReference")

	store, err := resolver.StoreForReference(ctx, resolver.ComponentVersionReference(ctx, componentName, componentVersion))
	r.NoError(err)
	graphStore, ok := store.(content.ReadOnlyGraphStorage)
	r.Truef(ok, "store %T must implement content.ReadOnlyGraphStorage for referrers discovery", store)

	subject, err := store.Resolve(ctx, localBlob.LocalReference)
	r.NoError(err)

	refs, err := orasregistry.Referrers(ctx, graphStore, subject, annotations.OwnershipArtifactType)
	r.NoError(err)
	r.Lenf(refs, 1, "expected exactly one ownership referrer on subject %s", subject.Digest)
	r.Equal(componentName, refs[0].Annotations[annotations.OwnershipComponentName])
	r.Equal(componentVersion, refs[0].Annotations[annotations.OwnershipComponentVersion])
}

func launchRegistryOCIRepository(t *testing.T) (*ocmoci.Repository, ocmoci.Resolver) {
	t.Helper()
	cvr, resolver := launchRegistryRepositoryWithResolver(t)
	repo, ok := cvr.(*ocmoci.Repository)
	require.True(t, ok, "launchRegistryRepository must return *ocmoci.Repository")
	return repo, resolver
}

type ownershipTargetRepositoryProvider struct {
	repo *ocmoci.Repository
}

func (p ownershipTargetRepositoryProvider) GetTargetRepository(_ context.Context, _ *constructorruntime.Component) (constructor.TargetRepository, error) {
	return p.repo, nil
}

// localFileInput and localFileInputMethod are a minimal constructor input method
// that reads a file from a working directory. They let this test exercise the
// construction + ownership-referrer path without importing the input/file binding
// (a higher layer, disallowed by depguard; see golangci.yml).
type localFileInput struct {
	Type      runtime.Type `json:"type"`
	Path      string       `json:"path"`
	MediaType string       `json:"mediaType,omitempty"`
}

func (s *localFileInput) GetType() runtime.Type        { return s.Type }
func (s *localFileInput) SetType(t runtime.Type)       { s.Type = t }
func (s *localFileInput) DeepCopyTyped() runtime.Typed { cp := *s; return &cp }

func newLocalFileInputScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	scheme.MustRegisterWithAlias(&localFileInput{},
		runtime.NewVersionedType("testinput", "v1"),
		runtime.NewUnversionedType("testinput"),
	)
	return scheme
}

type localFileInputMethod struct {
	scheme     *runtime.Scheme
	workingDir string
}

func (m *localFileInputMethod) GetResourceCredentialConsumerIdentity(context.Context, *constructorruntime.Resource) (runtime.Identity, error) {
	return runtime.Identity{}, nil
}

func (m *localFileInputMethod) ProcessResource(_ context.Context, resource *constructorruntime.Resource, _ runtime.Typed) (*constructor.ResourceInputMethodResult, error) {
	spec := localFileInput{}
	if err := m.scheme.Convert(resource.Input, &spec); err != nil {
		return nil, err
	}
	b, err := filesystem.GetBlobInWorkingDirectory(spec.Path, m.workingDir)
	if err != nil {
		return nil, err
	}
	b.SetMediaType(spec.MediaType)
	return &constructor.ResourceInputMethodResult{ProcessedBlobData: b}, nil
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
