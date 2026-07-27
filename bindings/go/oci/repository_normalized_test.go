package oci_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	ocidescriptor "ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
	"ocm.software/open-component-model/bindings/go/repository"
)

// newNormalizedRepo creates an *oci.Repository backed by an in-memory CTF store,
// matching the setup used by the existing repository tests.
func newNormalizedRepo(t *testing.T, opts ...oci.RepositoryOption) *oci.Repository {
	t.Helper()
	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	require.NoError(t, err)
	ctfStore := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	// Repository() helper (defined in repository_test.go) prepends WithTempDir, so
	// replicate that here with the same helper to keep setup consistent.
	return Repository(t, append([]oci.RepositoryOption{ocictf.WithCTF(ctfStore)}, opts...)...)
}

func simpleDescriptor(name, version string) *descriptor.Descriptor {
	return &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			Provider: descriptor.Provider{Name: "internal"},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    name,
					Version: version,
				},
			},
		},
	}
}

// TestRepository_NormalizedLayout_RoundTrip checks that a repository created
// with WithComponentVersionLayout(LayoutNormalized) can add and retrieve a
// component version via the normalized layout path.
func TestRepository_NormalizedLayout_RoundTrip(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	repo := newNormalizedRepo(t, oci.WithComponentVersionLayout(oci.LayoutNormalized))

	desc := simpleDescriptor("example.org/comp", "1.0.0")

	// Should not exist yet.
	_, err := repo.GetComponentVersion(ctx, desc.Component.Name, desc.Component.Version)
	r.Error(err)
	r.ErrorIs(err, repository.ErrNotFound)

	// Add the component version using the normalized layout.
	r.NoError(repo.AddComponentVersion(ctx, desc))

	// Retrieve it — the read path must detect the normalized manifest and delegate
	// to GetNormalizedComponentVersion.
	got, err := repo.GetComponentVersion(ctx, desc.Component.Name, desc.Component.Version)
	r.NoError(err)
	r.NotNil(got)
	r.Equal("example.org/comp", got.Component.Name)
	r.Equal("1.0.0", got.Component.Version)
}

// TestRepository_NormalizedLayout_GetLocalResource_RoundTrip verifies that a local resource
// uploaded to a normalized-layout repository can be read back via GetLocalResource. Under the
// normalized layout the tag resolves to the access-free manifest; the read path must resolve the
// full access-bearing descriptor via the access referrer and then fetch the local blob content by
// digest (the content is pushed as a content-addressable blob during AddLocalResource).
func TestRepository_NormalizedLayout_GetLocalResource_RoundTrip(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	repo := newNormalizedRepo(t, oci.WithComponentVersionLayout(oci.LayoutNormalized))

	desc := simpleDescriptor("example.org/comp", "1.0.0")

	content := []byte("normalized local resource content")
	resource := &descriptor.Resource{
		Relation: descriptor.LocalRelation,
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "test-resource",
				Version: "1.0.0",
			},
		},
		Type: "ociImageLayer",
		Access: &v2.LocalBlob{
			LocalReference: digest.FromBytes(content).String(),
			MediaType:      ociImageSpecV1.MediaTypeImageLayer,
		},
	}

	// Upload the blob; AddLocalResource populates the resource digest from the blob, which the
	// normalized layout requires for every resource.
	b := inmemory.New(bytes.NewReader(content))
	newRes, err := repo.AddLocalResource(ctx, desc.Component.Name, desc.Component.Version, resource, b)
	r.NoError(err)
	r.NotNil(newRes)
	r.NotNil(newRes.Digest, "AddLocalResource should populate the resource digest")

	desc.Component.Resources = append(desc.Component.Resources, *newRes)
	r.NoError(repo.AddComponentVersion(ctx, desc))

	// Read the local resource back via the normalized read path.
	blb, gotRes, err := repo.GetLocalResource(ctx, desc.Component.Name, desc.Component.Version, map[string]string{
		"name":    "test-resource",
		"version": "1.0.0",
	})
	r.NoError(err)
	r.NotNil(blb)
	r.NotNil(gotRes)
	r.Equal("test-resource", gotRes.Name)
	r.Equal("1.0.0", gotRes.Version)

	reader, err := blb.ReadCloser()
	r.NoError(err)
	t.Cleanup(func() { r.NoError(reader.Close()) })
	got, err := io.ReadAll(reader)
	r.NoError(err)
	r.Equal(content, got, "round-tripped blob content must equal what was uploaded")
}

// TestRepository_NormalizedLayout_GetLocalSource_RoundTrip is the source-side counterpart to the
// resource round-trip test above, exercising GetLocalSource under the normalized layout.
func TestRepository_NormalizedLayout_GetLocalSource_RoundTrip(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	repo := newNormalizedRepo(t, oci.WithComponentVersionLayout(oci.LayoutNormalized))

	desc := simpleDescriptor("example.org/comp", "1.0.0")

	content := []byte("normalized local source content")
	source := &descriptor.Source{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "test-source",
				Version: "1.0.0",
			},
		},
		Type: "ociImageLayer",
		Access: &v2.LocalBlob{
			LocalReference: digest.FromBytes(content).String(),
			MediaType:      ociImageSpecV1.MediaTypeImageLayer,
		},
	}

	b := inmemory.New(bytes.NewReader(content))
	newSrc, err := repo.AddLocalSource(ctx, desc.Component.Name, desc.Component.Version, source, b)
	r.NoError(err)
	r.NotNil(newSrc)

	desc.Component.Sources = append(desc.Component.Sources, *newSrc)
	r.NoError(repo.AddComponentVersion(ctx, desc))

	blb, gotSrc, err := repo.GetLocalSource(ctx, desc.Component.Name, desc.Component.Version, map[string]string{
		"name":    "test-source",
		"version": "1.0.0",
	})
	r.NoError(err)
	r.NotNil(blb)
	r.NotNil(gotSrc)
	r.Equal("test-source", gotSrc.Name)

	reader, err := blb.ReadCloser()
	r.NoError(err)
	t.Cleanup(func() { r.NoError(reader.Close()) })
	got, err := io.ReadAll(reader)
	r.NoError(err)
	r.Equal(content, got, "round-tripped source blob content must equal what was uploaded")
}

// pushReferrer pushes an OCI image manifest that references subject as its Subject,
// with the given artifactType, into store. It returns the referrer manifest descriptor.
// The config and a single tiny layer are pushed as content-addressable blobs so the
// whole referrer graph is present in the source store and can be carried.
func pushReferrer(t *testing.T, ctx context.Context, store spec.Store, subject ociImageSpecV1.Descriptor, artifactType string) ociImageSpecV1.Descriptor {
	t.Helper()
	r := require.New(t)

	configRaw := []byte("{}")
	configDesc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageConfig,
		Digest:    digest.FromBytes(configRaw),
		Size:      int64(len(configRaw)),
	}
	r.NoError(store.Push(ctx, configDesc, bytes.NewReader(configRaw)))

	layerRaw := []byte("referrer-payload-" + artifactType)
	layerDesc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageLayer,
		Digest:    digest.FromBytes(layerRaw),
		Size:      int64(len(layerRaw)),
	}
	r.NoError(store.Push(ctx, layerDesc, bytes.NewReader(layerRaw)))

	subjectCopy := subject
	manifest := ociImageSpecV1.Manifest{
		Versioned:    specs.Versioned{SchemaVersion: 2},
		MediaType:    ociImageSpecV1.MediaTypeImageManifest,
		ArtifactType: artifactType,
		Config:       configDesc,
		Layers:       []ociImageSpecV1.Descriptor{layerDesc},
		Subject:      &subjectCopy,
	}
	manifestRaw, err := json.Marshal(manifest)
	r.NoError(err)
	manifestDesc := ociImageSpecV1.Descriptor{
		MediaType:    ociImageSpecV1.MediaTypeImageManifest,
		ArtifactType: artifactType,
		Digest:       digest.FromBytes(manifestRaw),
		Size:         int64(len(manifestRaw)),
	}
	r.NoError(store.Push(ctx, manifestDesc, bytes.NewReader(manifestRaw)))
	return manifestDesc
}

// TestRepository_CarryComponentSignatures verifies that CarryComponentSignatures copies a
// non-access referrer (e.g. a cosign signature) of the normalized component manifest from a
// source repository into a target repository, while skipping the OCM-managed access referrer.
// Both repos use the normalized layout so the component-manifest digest is identical, which is
// the precondition for the referrers to point at content that exists on the target.
func TestRepository_CarryComponentSignatures(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	// Two independent CTF stores, each fronted by an *oci.Repository using the normalized layout.
	srcFS, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	srcCTF := ocictf.NewFromCTF(ctf.NewFileSystemCTF(srcFS))
	src := Repository(t, ocictf.WithCTF(srcCTF), oci.WithComponentVersionLayout(oci.LayoutNormalized))

	dstFS, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	dstCTF := ocictf.NewFromCTF(ctf.NewFileSystemCTF(dstFS))
	dst := Repository(t, ocictf.WithCTF(dstCTF), oci.WithComponentVersionLayout(oci.LayoutNormalized))

	const compName, compVersion = "example.org/comp", "1.0.0"

	// Add the SAME component to BOTH repos → identical normalized manifest digest.
	r.NoError(src.AddComponentVersion(ctx, simpleDescriptor(compName, compVersion)))
	r.NoError(dst.AddComponentVersion(ctx, simpleDescriptor(compName, compVersion)))

	// Grab the source store handle to push fake referrers of the component manifest.
	srcRef := srcCTF.ComponentVersionReference(ctx, compName, compVersion)
	srcStore, err := srcCTF.StoreForReference(ctx, srcRef)
	r.NoError(err)
	srcManifest, err := srcStore.Resolve(ctx, srcRef)
	r.NoError(err)

	dstRef := dstCTF.ComponentVersionReference(ctx, compName, compVersion)
	dstStore, err := dstCTF.StoreForReference(ctx, dstRef)
	r.NoError(err)
	dstManifest, err := dstStore.Resolve(ctx, dstRef)
	r.NoError(err)

	// Precondition: identical component-manifest digest across the two repos.
	r.Equal(srcManifest.Digest, dstManifest.Digest, "normalized manifest digests must match across stores")

	// Push a cosign-type (non-access) referrer and an access-type referrer into the source.
	cosignReferrer := pushReferrer(t, ctx, srcStore, srcManifest, "application/vnd.dev.cosign.simplesigning.v1+json")
	accessReferrer := pushReferrer(t, ctx, srcStore, srcManifest, ocidescriptor.ArtifactTypeAccessDescriptor)

	// Sanity: the fake access referrer does not exist in dst before carry.
	existsBefore, err := dstStore.Exists(ctx, accessReferrer)
	r.NoError(err)
	r.False(existsBefore, "the fake access referrer must not pre-exist in dst")

	// Carry.
	r.NoError(dst.CarryComponentSignatures(ctx, src, compName, compVersion))

	// The cosign referrer (and its blobs) must now exist in dst.
	existsCosign, err := dstStore.Exists(ctx, cosignReferrer)
	r.NoError(err)
	r.True(existsCosign, "the cosign referrer must be carried into dst")

	// The specific fake access referrer from src must NOT have been copied (it was skipped).
	existsAccess, err := dstStore.Exists(ctx, accessReferrer)
	r.NoError(err)
	r.False(existsAccess, "the fake access referrer from src must be skipped, not carried")
}

// TestRepository_CarryComponentSignatures_NoOpWhenDigestsDiffer verifies that no referrers are
// carried when the source and target component manifests resolve to different digests (i.e. the
// normalized-layout digest-preservation precondition does not hold).
func TestRepository_CarryComponentSignatures_NoOpWhenDigestsDiffer(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	srcFS, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	srcCTF := ocictf.NewFromCTF(ctf.NewFileSystemCTF(srcFS))
	src := Repository(t, ocictf.WithCTF(srcCTF), oci.WithComponentVersionLayout(oci.LayoutNormalized))

	dstFS, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	dstCTF := ocictf.NewFromCTF(ctf.NewFileSystemCTF(dstFS))
	dst := Repository(t, ocictf.WithCTF(dstCTF), oci.WithComponentVersionLayout(oci.LayoutNormalized))

	const compName, compVersion = "example.org/comp", "1.0.0"

	// DIFFERENT provider → different normalized bytes → different manifest digest.
	srcDesc := simpleDescriptor(compName, compVersion)
	srcDesc.Component.Provider = descriptor.Provider{Name: "source-provider"}
	r.NoError(src.AddComponentVersion(ctx, srcDesc))

	dstDesc := simpleDescriptor(compName, compVersion)
	dstDesc.Component.Provider = descriptor.Provider{Name: "target-provider"}
	r.NoError(dst.AddComponentVersion(ctx, dstDesc))

	srcRef := srcCTF.ComponentVersionReference(ctx, compName, compVersion)
	srcStore, err := srcCTF.StoreForReference(ctx, srcRef)
	r.NoError(err)
	srcManifest, err := srcStore.Resolve(ctx, srcRef)
	r.NoError(err)

	cosignReferrer := pushReferrer(t, ctx, srcStore, srcManifest, "application/vnd.dev.cosign.simplesigning.v1+json")

	// Carry must be a no-op because the digests differ.
	r.NoError(dst.CarryComponentSignatures(ctx, src, compName, compVersion))

	dstRef := dstCTF.ComponentVersionReference(ctx, compName, compVersion)
	dstStore, err := dstCTF.StoreForReference(ctx, dstRef)
	r.NoError(err)
	existsCosign, err := dstStore.Exists(ctx, cosignReferrer)
	r.NoError(err)
	r.False(existsCosign, "no referrer must be carried when component manifest digests differ")
}

// TestRepository_DefaultLayout_RoundTrip is a regression guard that confirms the
// default (LayoutV2) repository still round-trips a component version correctly
// after the normalized-layout detection changes.
func TestRepository_DefaultLayout_RoundTrip(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	// No WithComponentVersionLayout option → default LayoutV2.
	repo := newNormalizedRepo(t)

	desc := simpleDescriptor("example.org/v2comp", "2.0.0")

	r.NoError(repo.AddComponentVersion(ctx, desc))

	got, err := repo.GetComponentVersion(ctx, desc.Component.Name, desc.Component.Version)
	r.NoError(err)
	r.NotNil(got)
	r.Equal("example.org/v2comp", got.Component.Name)
	r.Equal("2.0.0", got.Component.Version)
}
