package internal

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"log/slog"
	"os"
	"testing"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/oras-project/oras-go/v3/content"
	"github.com/oras-project/oras-go/v3/errdef"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	ocidescriptor "ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
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
	spec *ctfv1.Repository
	// ctf is the archive itself, to remove content behind the back of the OCI store.
	ctf     ctf.CTF
	archive *ocictf.Store
	repo    *oci.Repository
	// logs collects what repo logs at warning level and above.
	logs *bytes.Buffer
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
	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	require.NoError(t, err)
	c := &ctfRepo{spec: testCTFRepo(path), ctf: ctf.NewFileSystemCTF(fs), logs: &bytes.Buffer{}}
	c.archive = ocictf.NewFromCTF(c.ctf)
	logger := slog.New(slog.NewTextHandler(c.logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	c.repo, err = oci.NewRepository(append([]oci.RepositoryOption{ocictf.WithCTF(c.archive), oci.WithTempDir(t.TempDir()), oci.WithLogger(logger)}, opts...)...)
	require.NoError(t, err)
	if p.repos == nil {
		p.repos = map[string]*ctfRepo{}
	}
	p.repos[path] = c
	return c
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
	return newNormalizedSourceWith(t, nil, opts...)
}

// newNormalizedSourceWith is [newNormalizedSource] for a component version that extend completes
// before it is stored, e.g. with further resources it adds to source.
func newNormalizedSourceWith(t *testing.T, extend func(t *testing.T, source *ctfRepo, desc *descriptor.Descriptor), opts ...oci.RepositoryOption) *normalizedSource {
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
	if extend != nil {
		extend(t, source, desc)
	}
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
	return f.transferWith(t, transferRun{}, targets...)
}

// transferRun varies a transfer of the [normalizedSource].
type transferRun struct {
	// pin is the OCI digest of the source artifact the root is pinned to. If it is empty, the
	// root is read by its version tag.
	pin string
	// discovery is the repository the root is discovered from, if it is not the source repository
	// the transformations open.
	discovery repository.ComponentVersionRepository
	// edit changes the graph definition before it is built and processed.
	edit func(t *testing.T, tgd *transformv1alpha1.TransformationGraphDefinition)
}

// transferWith is [normalizedSource.transfer] for a transfer varied by run.
func (f *normalizedSource) transferWith(t *testing.T, run transferRun, targets ...*ctfRepo) error {
	t.Helper()
	r := require.New(t)
	specs := make([]runtime.Typed, len(targets))
	for i, target := range targets {
		specs[i] = target.spec
	}
	root := TransferRoot{
		RootComponentKey: normalizedKey,
		Targets:          specs,
		SourceResolver: &mockCVRepoResolver{
			specs:      map[string]runtime.Typed{normalizedKey: f.source.spec},
			sharedRepo: f.source.repo,
		},
	}
	if run.discovery != nil {
		root.SourceResolver.(*mockCVRepoResolver).sharedRepo = run.discovery
	}
	if run.pin != "" {
		pin, err := repository.ParseDigestPin(run.pin)
		r.NoError(err)
		root.ExpectedDigest = pin
	}
	tgd, err := BuildGraphDefinition(t.Context(), map[string]TransferRoot{normalizedKey: root}, transferv1alpha1.Config{}, nil)
	r.NoError(err)
	if run.edit != nil {
		run.edit(t, tgd)
	}
	graph, err := NewDefaultBuilder(f.provider, nil, nil, nil, t.TempDir()).BuildAndCheck(tgd)
	r.NoError(err)
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

// requireCopied asserts that target holds the component version as the OCI graph the source
// stores it as: the version tag resolves to the normalized manifest of the source, and signatures
// exist byte-identical.
func (f *normalizedSource) requireCopied(t *testing.T, target *ctfRepo, signatures []ociImageSpecV1.Descriptor) {
	t.Helper()
	r := require.New(t)
	sourceStore, _ := f.source.store(t, normalizedComponent, normalizedVersion)
	targetStore, targetReference := target.store(t, normalizedComponent, normalizedVersion)

	tagged, err := targetStore.Resolve(t.Context(), targetReference)
	r.NoError(err)
	r.Equal(f.normalized.Digest, tagged.Digest, "the version tag must resolve to the normalized manifest of the source")
	for _, signature := range signatures {
		exists, err := targetStore.Exists(t.Context(), signature)
		r.NoError(err)
		r.True(exists, "signature referrer %s (%s) must exist on the target", signature.Digest, signature.ArtifactType)
		r.Equal(fetchAll(t, sourceStore, signature), fetchAll(t, targetStore, signature), "signature referrers must be copied byte-identical")
	}
}

// requireNotAdded asserts that target holds neither the version tag nor a signature referrer of
// the source: a transfer that fails its checks must not have written the component version.
func (f *normalizedSource) requireNotAdded(t *testing.T, target *ctfRepo) {
	t.Helper()
	r := require.New(t)
	targetStore, targetReference := target.store(t, normalizedComponent, normalizedVersion)

	_, err := targetStore.Resolve(t.Context(), targetReference)
	r.ErrorIs(err, errdef.ErrNotFound, "the version must not be tagged")
	for _, signature := range f.signatures {
		exists, err := targetStore.Exists(t.Context(), signature)
		r.NoError(err)
		r.False(exists, "signature referrer %s (%s) must not exist on the target", signature.Digest, signature.ArtifactType)
	}
	r.Empty(target.referrers(t, normalizedComponent, normalizedVersion, f.normalized, ""), "the normalized manifest must have no referrers on the target")
}

// requireOwnAccessReferrers asserts that target lists n access referrers of the normalized
// manifest and holds none of the source, and returns them.
func (f *normalizedSource) requireOwnAccessReferrers(t *testing.T, target *ctfRepo, n int) []ociImageSpecV1.Descriptor {
	t.Helper()
	r := require.New(t)
	targetStore, _ := target.store(t, normalizedComponent, normalizedVersion)
	for _, sourceAccess := range f.accessReferrers(t, f.source) {
		exists, err := targetStore.Exists(t.Context(), sourceAccess)
		r.NoError(err)
		r.False(exists, "access referrer %s of the source must not be copied", sourceAccess.Digest)
	}
	targetAccess := f.accessReferrers(t, target)
	r.Len(targetAccess, n, "the target must hold only the access referrers it wrote itself")
	return targetAccess
}
