package ctf

import (
	"bytes"
	_ "crypto/sha512" // for digest.SHA512 in TestBuildReferrersTag
	"encoding/json"
	"strconv"
	"sync"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/memory"

	"ocm.software/open-component-model/bindings/go/ctf"
	ocmannotations "ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	ocidescriptor "ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
	indexv1 "ocm.software/open-component-model/bindings/go/oci/spec/index/component/v1"
)

const testOwnershipArtifactType = "application/vnd.ocm.software.ownership.v1+json"

// referrersTestRepo returns a repository for "test-repo" on a fresh CTF
// together with the underlying archive for white-box assertions.
func referrersTestRepo(t *testing.T) (*repository, ctf.CTF) {
	t.Helper()
	archive := setupTestCTF(t)
	store, err := NewFromCTF(archive).StoreForReference(t.Context(), "test-repo:latest")
	require.NoError(t, err)
	return store.(*repository), archive
}

// pushEmptyJSON pushes the shared empty JSON blob referenced as config and
// layer by the test manifests.
func pushEmptyJSON(t *testing.T, repo *repository) {
	t.Helper()
	desc := ociImageSpecV1.DescriptorEmptyJSON
	require.NoError(t, repo.Push(t.Context(), desc, bytes.NewReader(desc.Data)))
}

// imageManifest builds an OCI image manifest and its descriptor. An empty
// configMediaType keeps the empty JSON config.
func imageManifest(t *testing.T, subject *ociImageSpecV1.Descriptor, artifactType, configMediaType string, annotations map[string]string) (ociImageSpecV1.Descriptor, []byte) {
	t.Helper()
	cfg := ociImageSpecV1.DescriptorEmptyJSON
	if configMediaType != "" {
		cfg.MediaType = configMediaType
	}
	manifest := ociImageSpecV1.Manifest{
		Versioned:    specs.Versioned{SchemaVersion: 2},
		MediaType:    ociImageSpecV1.MediaTypeImageManifest,
		ArtifactType: artifactType,
		Config:       cfg,
		Layers:       []ociImageSpecV1.Descriptor{ociImageSpecV1.DescriptorEmptyJSON},
		Subject:      subject,
		Annotations:  annotations,
	}
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	return ociImageSpecV1.Descriptor{
		MediaType: manifest.MediaType,
		Digest:    digest.FromBytes(raw),
		Size:      int64(len(raw)),
	}, raw
}

// pushedSubject pushes the empty JSON blob and a subject-less manifest acting
// as the referrer target.
func pushedSubject(t *testing.T, repo *repository) ociImageSpecV1.Descriptor {
	t.Helper()
	pushEmptyJSON(t, repo)
	desc, raw := imageManifest(t, nil, "", "", map[string]string{"test": "subject"})
	require.NoError(t, repo.Push(t.Context(), desc, bytes.NewReader(raw)))
	return desc
}

// listReferrers collects the result of a single Referrers call.
func listReferrers(t *testing.T, repo *repository, subject ociImageSpecV1.Descriptor, artifactType string) []ociImageSpecV1.Descriptor {
	t.Helper()
	var out []ociImageSpecV1.Descriptor
	require.NoError(t, repo.Referrers(t.Context(), subject, artifactType, func(referrers []ociImageSpecV1.Descriptor) error {
		out = append(out, referrers...)
		return nil
	}))
	return out
}

func TestBuildReferrersTag(t *testing.T) {
	sha256Digest := digest.FromString("subject")
	tag, err := buildReferrersTag(ociImageSpecV1.Descriptor{Digest: sha256Digest})
	require.NoError(t, err)
	assert.Equal(t, "sha256-"+sha256Digest.Encoded(), tag)

	// mirror oras-go: sha512 tags are not truncated to the spec's 64
	// character limit.
	sha512Digest := digest.SHA512.FromString("subject")
	tag, err = buildReferrersTag(ociImageSpecV1.Descriptor{Digest: sha512Digest})
	require.NoError(t, err)
	assert.Equal(t, "sha512-"+sha512Digest.Encoded(), tag)
	assert.Len(t, tag, len("sha512-")+128)

	_, err = buildReferrersTag(ociImageSpecV1.Descriptor{Digest: "not-a-digest"})
	assert.Error(t, err)
}

func TestAddReferrer(t *testing.T) {
	a := ociImageSpecV1.Descriptor{MediaType: ociImageSpecV1.MediaTypeImageManifest, Digest: digest.FromString("a"), Size: 1}
	b := ociImageSpecV1.Descriptor{MediaType: ociImageSpecV1.MediaTypeImageManifest, Digest: digest.FromString("b"), Size: 2}

	updated, changed := addReferrer(nil, a)
	assert.True(t, changed)
	assert.Equal(t, []ociImageSpecV1.Descriptor{a}, updated)

	updated, changed = addReferrer([]ociImageSpecV1.Descriptor{a}, b)
	assert.True(t, changed)
	assert.Equal(t, []ociImageSpecV1.Descriptor{a, b}, updated)

	// re-adding an existing referrer is a no-op
	_, changed = addReferrer([]ociImageSpecV1.Descriptor{a, b}, a)
	assert.False(t, changed)

	// bad and duplicate entries are cleaned up and force a rewrite even if
	// the added referrer is already present
	updated, changed = addReferrer([]ociImageSpecV1.Descriptor{{}, a, a}, a)
	assert.True(t, changed)
	assert.Equal(t, []ociImageSpecV1.Descriptor{a}, updated)
}

func TestPushWithSubjectIndexesReferrer(t *testing.T) {
	repo, _ := referrersTestRepo(t)
	subject := pushedSubject(t, repo)

	annotations := map[string]string{
		"software.ocm.component.name":    "acme.org/app",
		"software.ocm.component.version": "1.0.0",
	}
	refDesc, refRaw := imageManifest(t, &subject, testOwnershipArtifactType, "", annotations)
	require.NoError(t, repo.Push(t.Context(), refDesc, bytes.NewReader(refRaw)))

	got := listReferrers(t, repo, subject, "")
	require.Len(t, got, 1)
	assert.Equal(t, refDesc.Digest, got[0].Digest)
	assert.Equal(t, refDesc.Size, got[0].Size)
	assert.Equal(t, refDesc.MediaType, got[0].MediaType)
	assert.Equal(t, testOwnershipArtifactType, got[0].ArtifactType)
	assert.Equal(t, annotations, got[0].Annotations)

	// artifact type filtering, client-side
	assert.Len(t, listReferrers(t, repo, subject, testOwnershipArtifactType), 1)
	assert.Empty(t, listReferrers(t, repo, subject, "application/vnd.other"))

	// the referrers tag resolves to an OCI image index, like on a registry
	referrersTag, err := buildReferrersTag(subject)
	require.NoError(t, err)
	idxDesc, err := repo.Resolve(t.Context(), referrersTag)
	require.NoError(t, err)
	assert.Equal(t, ociImageSpecV1.MediaTypeImageIndex, idxDesc.MediaType)
}

func TestReferrerArtifactTypeFallsBackToConfigMediaType(t *testing.T) {
	repo, _ := referrersTestRepo(t)
	subject := pushedSubject(t, repo)

	const configMediaType = "application/vnd.test.config.v1+json"
	refDesc, refRaw := imageManifest(t, &subject, "", configMediaType, nil)
	require.NoError(t, repo.Push(t.Context(), refDesc, bytes.NewReader(refRaw)))

	got := listReferrers(t, repo, subject, "")
	require.Len(t, got, 1)
	// https://github.com/opencontainers/distribution-spec/blob/v1.1.1/spec.md#listing-referrers
	assert.Equal(t, configMediaType, got[0].ArtifactType)
}

func TestSecondReferrerRetagsIndexAndGCsPrior(t *testing.T) {
	ctx := t.Context()
	repo, archive := referrersTestRepo(t)
	subject := pushedSubject(t, repo)
	referrersTag, err := buildReferrersTag(subject)
	require.NoError(t, err)

	ref1Desc, ref1Raw := imageManifest(t, &subject, testOwnershipArtifactType, "", map[string]string{"ref": "1"})
	require.NoError(t, repo.Push(ctx, ref1Desc, bytes.NewReader(ref1Raw)))
	firstIndex, err := repo.Resolve(ctx, referrersTag)
	require.NoError(t, err)

	ref2Desc, ref2Raw := imageManifest(t, &subject, testOwnershipArtifactType, "", map[string]string{"ref": "2"})
	require.NoError(t, repo.Push(ctx, ref2Desc, bytes.NewReader(ref2Raw)))
	secondIndex, err := repo.Resolve(ctx, referrersTag)
	require.NoError(t, err)
	require.NotEqual(t, firstIndex.Digest, secondIndex.Digest)

	got := listReferrers(t, repo, subject, "")
	require.Len(t, got, 2)
	digests := []digest.Digest{got[0].Digest, got[1].Digest}
	assert.Contains(t, digests, ref1Desc.Digest)
	assert.Contains(t, digests, ref2Desc.Digest)

	// exactly one index entry refers to the referrers tag/digest, and it
	// points at the new referrers index. The prior index has no entry.
	idx, err := archive.GetIndex(ctx)
	require.NoError(t, err)
	for _, artifact := range idx.GetArtifacts() {
		if artifact.Repository != repo.repo {
			continue
		}
		assert.NotEqual(t, firstIndex.Digest.String(), artifact.Digest,
			"prior referrers index entry should be removed")
	}
	var tagged []string
	for _, artifact := range idx.GetArtifacts() {
		if artifact.Repository == repo.repo && artifact.Tag == referrersTag {
			tagged = append(tagged, artifact.Digest)
		}
	}
	require.Len(t, tagged, 1)
	assert.Equal(t, secondIndex.Digest.String(), tagged[0])

	// the prior referrers index blob is gone
	blobs, err := archive.ListBlobs(ctx)
	require.NoError(t, err)
	assert.NotContains(t, blobs, firstIndex.Digest.String())
	_, err = repo.Resolve(ctx, firstIndex.Digest.String())
	assert.Error(t, err)
}

// TestReferrerIndexBlobsAreGCdAcrossPushes asserts that each push to a subject
// that already has referrers retires the prior referrers index: the new index
// supersedes the old one as the referrers tag, and the prior index blob is
// best-effort deleted. Only the latest index blob remains on disk.
func TestReferrerIndexBlobsAreGCdAcrossPushes(t *testing.T) {
	ctx := t.Context()
	repo, archive := referrersTestRepo(t)
	subject := pushedSubject(t, repo)
	referrersTag, err := buildReferrersTag(subject)
	require.NoError(t, err)

	const pushes = 4
	indexDigests := make([]digest.Digest, 0, pushes)
	for i := range pushes {
		refDesc, refRaw := imageManifest(t, &subject, testOwnershipArtifactType, "", map[string]string{"ref": strconv.Itoa(i)})
		require.NoError(t, repo.Push(ctx, refDesc, bytes.NewReader(refRaw)))
		indexDesc, err := repo.Resolve(ctx, referrersTag)
		require.NoError(t, err)
		indexDigests = append(indexDigests, indexDesc.Digest)
	}

	// every push produced a fresh index digest; nothing was reused
	seen := make(map[digest.Digest]struct{}, pushes)
	for _, d := range indexDigests {
		seen[d] = struct{}{}
	}
	require.Len(t, seen, pushes, "each push should produce a new referrers index digest")

	// referrers list itself stays correct
	require.Len(t, listReferrers(t, repo, subject, ""), pushes)

	// only the latest index blob remains; prior indexes are GCd
	blobs, err := archive.ListBlobs(ctx)
	require.NoError(t, err)
	latest := indexDigests[pushes-1]
	assert.Contains(t, blobs, latest.String())
	for _, d := range indexDigests[:pushes-1] {
		assert.NotContains(t, blobs, d.String(), "stale referrers index blob %s should be GCd", d)
	}

	// only one index entry carries the referrers tag, and no entry references
	// any of the prior referrers index digests
	idx, err := archive.GetIndex(ctx)
	require.NoError(t, err)
	priorDigests := make(map[string]struct{}, pushes-1)
	for _, d := range indexDigests[:pushes-1] {
		priorDigests[d.String()] = struct{}{}
	}
	tagCount := 0
	for _, artifact := range idx.GetArtifacts() {
		if artifact.Repository != repo.repo {
			continue
		}
		_, isPrior := priorDigests[artifact.Digest]
		assert.False(t, isPrior, "prior referrers index %s should have no index entry", artifact.Digest)
		if artifact.Tag == referrersTag {
			tagCount++
			assert.Equal(t, latest.String(), artifact.Digest)
		}
	}
	assert.Equal(t, 1, tagCount, "exactly one entry should carry the referrers tag")
}

func TestRepushReferrerIsIdempotent(t *testing.T) {
	ctx := t.Context()
	repo, archive := referrersTestRepo(t)
	subject := pushedSubject(t, repo)
	referrersTag, err := buildReferrersTag(subject)
	require.NoError(t, err)

	refDesc, refRaw := imageManifest(t, &subject, testOwnershipArtifactType, "", nil)
	require.NoError(t, repo.Push(ctx, refDesc, bytes.NewReader(refRaw)))

	idx, err := archive.GetIndex(ctx)
	require.NoError(t, err)
	entriesAfterFirst := len(idx.GetArtifacts())
	indexAfterFirst, err := repo.Resolve(ctx, referrersTag)
	require.NoError(t, err)

	require.NoError(t, repo.Push(ctx, refDesc, bytes.NewReader(refRaw)))

	idx, err = archive.GetIndex(ctx)
	require.NoError(t, err)
	assert.Equal(t, entriesAfterFirst, len(idx.GetArtifacts()))
	indexAfterSecond, err := repo.Resolve(ctx, referrersTag)
	require.NoError(t, err)
	assert.Equal(t, indexAfterFirst.Digest, indexAfterSecond.Digest)
	assert.Len(t, listReferrers(t, repo, subject, ""), 1)
}

func TestIndexReferrerKeepsEmptyArtifactType(t *testing.T) {
	repo, _ := referrersTestRepo(t)
	subject := pushedSubject(t, repo)

	index := ociImageSpecV1.Index{
		Versioned:   specs.Versioned{SchemaVersion: 2},
		MediaType:   ociImageSpecV1.MediaTypeImageIndex,
		Manifests:   []ociImageSpecV1.Descriptor{},
		Subject:     &subject,
		Annotations: map[string]string{"ref": "index"},
	}
	raw, err := json.Marshal(index)
	require.NoError(t, err)
	refDesc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageIndex,
		Digest:    digest.FromBytes(raw),
		Size:      int64(len(raw)),
	}
	require.NoError(t, repo.Push(t.Context(), refDesc, bytes.NewReader(raw)))

	got := listReferrers(t, repo, subject, "")
	require.Len(t, got, 1)
	// indexes have no config to fall back to; the artifact type stays empty
	// and only matches the unfiltered listing
	assert.Empty(t, got[0].ArtifactType)
	assert.Empty(t, listReferrers(t, repo, subject, testOwnershipArtifactType))
}

func TestPushVerifiesManifestContent(t *testing.T) {
	ctx := t.Context()
	repo, _ := referrersTestRepo(t)

	good := []byte(`{"schemaVersion":2}`)
	wrongDigest := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromString("something else"),
		Size:      int64(len(good)),
	}
	require.Error(t, repo.Push(ctx, wrongDigest, bytes.NewReader(good)))
	exists, err := repo.Exists(ctx, wrongDigest)
	require.NoError(t, err)
	assert.False(t, exists, "mismatching manifest content must not be stored")

	wrongSize := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromBytes(good),
		Size:      int64(len(good)) - 1,
	}
	require.Error(t, repo.Push(ctx, wrongSize, bytes.NewReader(good)))
}

func TestReferrersIsPermissiveAndQuietOnMiss(t *testing.T) {
	repo, _ := referrersTestRepo(t)

	called := false
	fn := func([]ociImageSpecV1.Descriptor) error {
		called = true
		return nil
	}

	// unknown manifest: no referrers index, fn is never invoked
	unknown, _ := imageManifest(t, nil, "", "", map[string]string{"test": "unknown"})
	require.NoError(t, repo.Referrers(t.Context(), unknown, "", fn))
	assert.False(t, called)

	// non-manifest content with a valid digest is accepted (permissive,
	// like oras-go's remote.Repository) and simply yields nothing
	blobDesc := ociImageSpecV1.Descriptor{MediaType: "application/octet-stream", Digest: digest.FromString("blob"), Size: 4}
	require.NoError(t, repo.Referrers(t.Context(), blobDesc, "", fn))
	assert.False(t, called)

	// an invalid digest cannot form a referrers tag
	require.Error(t, repo.Referrers(t.Context(), ociImageSpecV1.Descriptor{Digest: "not-a-digest"}, "", fn))
	assert.False(t, called)
}

func TestPredecessorsReturnsReferrers(t *testing.T) {
	ctx := t.Context()
	repo, _ := referrersTestRepo(t)
	subject := pushedSubject(t, repo)

	refDesc, refRaw := imageManifest(t, &subject, testOwnershipArtifactType, "", nil)
	require.NoError(t, repo.Push(ctx, refDesc, bytes.NewReader(refRaw)))

	predecessors, err := repo.Predecessors(ctx, subject)
	require.NoError(t, err)
	require.Len(t, predecessors, 1)
	assert.Equal(t, refDesc.Digest, predecessors[0].Digest)

	// the referrer itself has no predecessors; in particular the referrers
	// index (registry-local bookkeeping without a subject edge) is never
	// reported and thus never copied by ExtendedCopyGraph
	predecessors, err = repo.Predecessors(ctx, refDesc)
	require.NoError(t, err)
	assert.Empty(t, predecessors)
}

func TestReferrersCallbackCanReenterStore(t *testing.T) {
	ctx := t.Context()
	repo, _ := referrersTestRepo(t)
	subject := pushedSubject(t, repo)

	refDesc, refRaw := imageManifest(t, &subject, testOwnershipArtifactType, "", nil)
	require.NoError(t, repo.Push(ctx, refDesc, bytes.NewReader(refRaw)))

	// the lock is released before fn runs, so fn may call back into the
	// store, as e.g. the version lister's referrer resolvers do
	require.NoError(t, repo.Referrers(ctx, subject, "", func(referrers []ociImageSpecV1.Descriptor) error {
		rc, err := repo.Fetch(ctx, referrers[0])
		if err != nil {
			return err
		}
		defer func() {
			require.NoError(t, rc.Close())
		}()
		var fetched bytes.Buffer
		if _, err := fetched.ReadFrom(rc); err != nil {
			return err
		}
		assert.Equal(t, refRaw, fetched.Bytes())
		return nil
	}))
}

func TestConcurrentReferrerPushes(t *testing.T) {
	ctx := t.Context()
	archive := setupTestCTF(t)
	provider := NewFromCTF(archive)

	base, err := provider.StoreForReference(ctx, "test-repo:latest")
	require.NoError(t, err)
	repo := base.(*repository)
	subject := pushedSubject(t, repo)

	// build all referrers up front; goroutines must not call into testing.T
	const n = 16
	type referrer struct {
		desc ociImageSpecV1.Descriptor
		raw  []byte
	}
	referrers := make([]referrer, n)
	for i := range n {
		desc, raw := imageManifest(t, &subject, testOwnershipArtifactType, "", map[string]string{"worker": strconv.Itoa(i)})
		referrers[i] = referrer{desc: desc, raw: raw}
	}

	// distinct repository instances share the parent Store's mutex, so the
	// read-modify-write of the referrers index is serialized and no update
	// is lost (run with -race)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store, err := provider.StoreForReference(ctx, "test-repo:latest")
			if err != nil {
				errs[i] = err
				return
			}
			errs[i] = store.Push(ctx, referrers[i].desc, bytes.NewReader(referrers[i].raw))
		}()
	}
	wg.Wait()
	for i, err := range errs {
		require.NoError(t, err, "push %d failed", i)
	}

	got := listReferrers(t, repo, subject, "")
	require.Len(t, got, n)
	seen := make(map[digest.Digest]struct{}, n)
	for _, d := range got {
		seen[d.Digest] = struct{}{}
	}
	for i := range n {
		assert.Contains(t, seen, referrers[i].desc.Digest, "referrer %d missing from index", i)
	}
}

func TestExtendedCopyGraphCopiesReferrers(t *testing.T) {
	ctx := t.Context()
	source, _ := referrersTestRepo(t)
	subject := pushedSubject(t, source)

	annotations := map[string]string{"software.ocm.component.name": "acme.org/app"}
	refDesc, refRaw := imageManifest(t, &subject, testOwnershipArtifactType, "", annotations)
	require.NoError(t, source.Push(ctx, refDesc, bytes.NewReader(refRaw)))

	// CTF -> memory: the referrer is discovered via Predecessors and rides
	// along with its subject
	intermediate := memory.New()
	require.NoError(t, oras.ExtendedCopyGraph(ctx, source, intermediate, subject, oras.ExtendedCopyGraphOptions{}))
	exists, err := intermediate.Exists(ctx, refDesc)
	require.NoError(t, err)
	require.True(t, exists, "referrer must be copied out of the CTF")

	// memory -> CTF: pushing the referrer manifest rebuilds the referrers
	// index in the target archive
	target, _ := referrersTestRepo(t)
	require.NoError(t, oras.ExtendedCopyGraph(ctx, intermediate, target, subject, oras.ExtendedCopyGraphOptions{}))
	got := listReferrers(t, target, subject, testOwnershipArtifactType)
	require.Len(t, got, 1)
	assert.Equal(t, refDesc.Digest, got[0].Digest)
	assert.Equal(t, annotations, got[0].Annotations)

	// CTF -> CTF directly
	direct, _ := referrersTestRepo(t)
	require.NoError(t, oras.ExtendedCopyGraph(ctx, source, direct, subject, oras.ExtendedCopyGraphOptions{}))
	got = listReferrers(t, direct, subject, testOwnershipArtifactType)
	require.Len(t, got, 1)
	assert.Equal(t, refDesc.Digest, got[0].Digest)
}

// TestComponentVersionReferrerListingContract exercises the contract the
// version lister relies on for ReferrerTrackingPolicyByIndexAndSubject:
// component version manifests carry the well-known component index as
// subject, and Referrers on that index returns descriptors whose annotations
// hold the component version (read by ReferrerAnnotationVersionResolver
// without fetching the manifests).
func TestComponentVersionReferrerListingContract(t *testing.T) {
	ctx := t.Context()
	archive := setupTestCTF(t)
	store, err := NewFromCTF(archive).StoreForReference(ctx, "ctf.ocm.software/component-descriptors/acme.org/app:1.0.0")
	require.NoError(t, err)
	repo := store.(*repository)

	require.NoError(t, indexv1.CreateIfNotExists(ctx, repo))

	subject := indexv1.Descriptor
	for _, version := range []string{"1.0.0", "1.1.0"} {
		annotations := map[string]string{
			ocmannotations.OCMComponentVersion: ocmannotations.NewComponentVersionAnnotation("acme.org/app", version),
		}
		desc, raw := imageManifest(t, &subject, ocidescriptor.MediaTypeComponentDescriptorV2, "", annotations)
		require.NoError(t, repo.Push(ctx, desc, bytes.NewReader(raw)))
	}

	got := listReferrers(t, repo, subject, ocidescriptor.MediaTypeComponentDescriptorV2)
	require.Len(t, got, 2)
	versions := make([]string, 0, len(got))
	for _, d := range got {
		versions = append(versions, d.Annotations[ocmannotations.OCMComponentVersion])
	}
	assert.Contains(t, versions, ocmannotations.NewComponentVersionAnnotation("acme.org/app", "1.0.0"))
	assert.Contains(t, versions, ocmannotations.NewComponentVersionAnnotation("acme.org/app", "1.1.0"))
}
