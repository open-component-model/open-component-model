package internal

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"testing"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/oras-project/oras-go/v3/content"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	ocidescriptor "ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

// The artifact types cosign uses for signatures and attestations. The fixtures only need
// referrers that are not access referrers; the values stand in for real cosign artifacts.
const (
	cosignSignatureArtifactType   = "application/vnd.dev.cosign.artifact.sig.v1+json"
	cosignAttestationArtifactType = "application/vnd.dev.cosign.artifact.sbom.v1+json"
)

const (
	normalizedComponent = "ocm.software/normalized"
	normalizedVersion   = "1.0.0"
	normalizedKey       = normalizedComponent + ":" + normalizedVersion
	normalizedResource  = "payload"
)

// ctfRepo is a CTF repository a test owns: the repository transformations open for specification
// spec, and the archive behind it to inspect and prepare its OCI content.
type ctfRepo struct {
	spec    *ctfv1.Repository
	archive *ocictf.Store
	repo    *oci.Repository
}

// store returns the OCI store of component in the archive and the reference of its version tag.
func (c *ctfRepo) store(t *testing.T, component, version string) (spec.Store, string) {
	t.Helper()
	reference := c.archive.ComponentVersionReference(t.Context(), component, version)
	store, err := c.archive.StoreForReference(t.Context(), reference)
	require.NoError(t, err)
	return store, reference
}

// referrers returns the listed referrers of subject, restricted to artifactType if it is not empty.
func (c *ctfRepo) referrers(t *testing.T, component, version string, subject ociImageSpecV1.Descriptor, artifactType string) []ociImageSpecV1.Descriptor {
	t.Helper()
	store, _ := c.store(t, component, version)
	predecessors, err := store.Predecessors(t.Context(), subject)
	require.NoError(t, err)
	var referrers []ociImageSpecV1.Descriptor
	for _, p := range predecessors {
		if artifactType == "" || p.ArtifactType == artifactType {
			referrers = append(referrers, p)
		}
	}
	return referrers
}

// ctfRepositoryProvider serves CTF repository specifications from the repositories registered by
// their file path, so that a transformation graph runs against archives the test can inspect.
type ctfRepositoryProvider struct {
	repository.ComponentVersionRepositoryProvider
	repos map[string]*ctfRepo
}

// add registers a new, empty CTF repository under path. The path only identifies the repository
// in its specification, the archive lives in a temporary directory.
func (p *ctfRepositoryProvider) add(t *testing.T, path string, opts ...oci.RepositoryOption) *ctfRepo {
	t.Helper()
	archive := newCTFStore(t)
	repo, err := oci.NewRepository(append([]oci.RepositoryOption{ocictf.WithCTF(archive), oci.WithTempDir(t.TempDir())}, opts...)...)
	require.NoError(t, err)
	if p.repos == nil {
		p.repos = map[string]*ctfRepo{}
	}
	p.repos[path] = &ctfRepo{spec: testCTFRepo(path), archive: archive, repo: repo}
	return p.repos[path]
}

func (p *ctfRepositoryProvider) GetComponentVersionRepository(_ context.Context, repositorySpecification, _ runtime.Typed) (repository.ComponentVersionRepository, error) {
	concrete, err := convertToConcreteRepo(repositorySpecification)
	if err != nil {
		return nil, err
	}
	ctfSpec, ok := concrete.(*ctfv1.Repository)
	if !ok {
		return nil, fmt.Errorf("unsupported repository specification %T", concrete)
	}
	repo, ok := p.repos[ctfSpec.FilePath]
	if !ok {
		return nil, fmt.Errorf("no repository registered for %q", ctfSpec.FilePath)
	}
	return repo.repo, nil
}

// normalizedSource is the component version [normalizedKey] with a local blob, stored in the
// normalized layout in the CTF repository source, together with signature referrers:
//
//	normalized manifest ◄── signature ◄── countersignature
//	                    ◄── attestation
//	                    ◄── access referrer (written by the repository)
type normalizedSource struct {
	provider *ctfRepositoryProvider
	source   *ctfRepo
	// normalized is the normalized manifest the version tag resolves to.
	normalized ociImageSpecV1.Descriptor
	// signatures are the signature referrers of normalized, transitively.
	signatures []ociImageSpecV1.Descriptor
	// resourceData is the content of the local blob [normalizedResource].
	resourceData []byte
}

// newNormalizedSource creates the source repository under the path "/source". opts configure it
// in addition to the normalized layout.
func newNormalizedSource(t *testing.T, opts ...oci.RepositoryOption) *normalizedSource {
	t.Helper()
	r := require.New(t)
	ctx := t.Context()

	provider := &ctfRepositoryProvider{}
	source := provider.add(t, "/source", append([]oci.RepositoryOption{oci.WithComponentVersionLayout(oci.LayoutNormalized)}, opts...)...)
	f := &normalizedSource{
		provider:     provider,
		source:       source,
		resourceData: []byte("content bound to the signed normalized descriptor"),
	}

	resource := localBlobResource(normalizedResource, normalizedVersion)
	desc := testDescriptor(normalizedComponent, normalizedVersion, []descriptor.Resource{resource}, nil)
	updated, err := source.repo.AddLocalResource(ctx, normalizedComponent, normalizedVersion, &resource, inmemory.New(bytes.NewReader(f.resourceData)))
	r.NoError(err)
	desc.Component.Resources[0] = *updated
	r.NoError(source.repo.AddComponentVersion(ctx, desc))

	store, reference := source.store(t, normalizedComponent, normalizedVersion)
	f.normalized, err = store.Resolve(ctx, reference)
	r.NoError(err)

	signature := pushReferrerManifest(t, store, f.normalized, cosignSignatureArtifactType)
	f.signatures = []ociImageSpecV1.Descriptor{
		signature,
		pushReferrerManifest(t, store, signature, cosignSignatureArtifactType),
		pushReferrerManifest(t, store, f.normalized, cosignAttestationArtifactType),
	}
	return f
}

// accessReferrers returns the access referrers of the normalized manifest listed in repo.
func (f *normalizedSource) accessReferrers(t *testing.T, repo *ctfRepo) []ociImageSpecV1.Descriptor {
	t.Helper()
	return repo.referrers(t, normalizedComponent, normalizedVersion, f.normalized, ocidescriptor.ArtifactTypeAccessDescriptor)
}

// transfer builds the transformation graph that transfers the component version from the source
// to targets and processes it.
func (f *normalizedSource) transfer(t *testing.T, targets ...*ctfRepo) error {
	t.Helper()
	specs := make([]runtime.Typed, len(targets))
	for i, target := range targets {
		specs[i] = target.spec
	}
	roots := map[string]TransferRoot{normalizedKey: {
		RootComponentKey: normalizedKey,
		Targets:          specs,
		SourceResolver: &mockCVRepoResolver{
			specs:      map[string]runtime.Typed{normalizedKey: f.source.spec},
			sharedRepo: f.source.repo,
		},
	}}
	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, nil)
	require.NoError(t, err)
	graph, err := NewDefaultBuilder(f.provider, nil, nil, nil, t.TempDir()).BuildAndCheck(tgd)
	require.NoError(t, err)
	return graph.Process(t.Context())
}

// pushReferrerManifest pushes a minimal referrer of subject with the given artifact type, standing
// in for a cosign signature or attestation.
func pushReferrerManifest(t *testing.T, store spec.Store, subject ociImageSpecV1.Descriptor, artifactType string) ociImageSpecV1.Descriptor {
	t.Helper()
	r := require.New(t)
	ctx := t.Context()

	payload := []byte(artifactType + " over " + subject.Digest.String())
	layer := content.NewDescriptorFromBytes("application/octet-stream", payload)
	pushIfAbsent := func(desc ociImageSpecV1.Descriptor, raw []byte) {
		exists, err := store.Exists(ctx, desc)
		r.NoError(err)
		if !exists {
			r.NoError(store.Push(ctx, desc, bytes.NewReader(raw)))
		}
	}
	pushIfAbsent(layer, payload)
	pushIfAbsent(ociImageSpecV1.DescriptorEmptyJSON, ociImageSpecV1.DescriptorEmptyJSON.Data)

	subject = ociImageSpecV1.Descriptor{MediaType: subject.MediaType, Digest: subject.Digest, Size: subject.Size}
	manifest := ociImageSpecV1.Manifest{
		MediaType:    ociImageSpecV1.MediaTypeImageManifest,
		ArtifactType: artifactType,
		Config:       ociImageSpecV1.DescriptorEmptyJSON,
		Layers:       []ociImageSpecV1.Descriptor{layer},
		Subject:      &subject,
	}
	manifest.SchemaVersion = 2
	raw, err := json.Marshal(manifest, runtime.JSONOptions)
	r.NoError(err)
	desc := content.NewDescriptorFromBytes(manifest.MediaType, raw)
	desc.ArtifactType = artifactType
	pushIfAbsent(desc, raw)
	return desc
}

// fetchAll returns the content of desc in store.
func fetchAll(t *testing.T, store spec.Store, desc ociImageSpecV1.Descriptor) []byte {
	t.Helper()
	raw, err := content.FetchAll(t.Context(), store, ociImageSpecV1.Descriptor{MediaType: desc.MediaType, Digest: desc.Digest, Size: desc.Size})
	require.NoError(t, err)
	return raw
}
