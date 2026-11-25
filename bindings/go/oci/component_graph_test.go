package oci_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/oras-project/oras-go/v3"
	"github.com/oras-project/oras-go/v3/content"
	"github.com/oras-project/oras-go/v3/content/memory"
	"github.com/oras-project/oras-go/v3/registry"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	ocidescriptor "ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	cosignSignatureType = "application/vnd.dev.cosign.artifact.sig.v1+json"
	attestationType     = "application/vnd.dev.sigstore.bundle.v0.3+json"
)

// countingGraph counts the requests a walk issues against a graph storage, per digest. It embeds
// the interface, so a registry.ReferrerLister of the wrapped storage is not promoted.
type countingGraph struct {
	content.ReadOnlyGraphStorage

	// fetchErr fails every fetch if set.
	fetchErr error

	mu       sync.Mutex
	fetches  map[digest.Digest]int
	exists   map[digest.Digest]int
	listings map[digest.Digest]int
}

func newCountingGraph(src content.ReadOnlyGraphStorage) *countingGraph {
	return &countingGraph{
		ReadOnlyGraphStorage: src,
		fetches:              map[digest.Digest]int{},
		exists:               map[digest.Digest]int{},
		listings:             map[digest.Digest]int{},
	}
}

func (c *countingGraph) count(counter map[digest.Digest]int, desc ociImageSpecV1.Descriptor) {
	c.mu.Lock()
	defer c.mu.Unlock()
	counter[desc.Digest]++
}

func (c *countingGraph) Fetch(ctx context.Context, desc ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	c.count(c.fetches, desc)
	if c.fetchErr != nil {
		return nil, c.fetchErr
	}
	return c.ReadOnlyGraphStorage.Fetch(ctx, desc)
}

func (c *countingGraph) Exists(ctx context.Context, desc ociImageSpecV1.Descriptor) (bool, error) {
	c.count(c.exists, desc)
	return c.ReadOnlyGraphStorage.Exists(ctx, desc)
}

func (c *countingGraph) Predecessors(ctx context.Context, desc ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error) {
	c.count(c.listings, desc)
	return c.ReadOnlyGraphStorage.Predecessors(ctx, desc)
}

// requestCounts is the expected value of a countingGraph counter.
func requestCounts(n int, descs ...ociImageSpecV1.Descriptor) map[digest.Digest]int {
	counts := make(map[digest.Digest]int, len(descs))
	for _, d := range descs {
		counts[d.Digest] = n
	}
	return counts
}

// untypedReferrers lists the referrers of the wrapped storage without their artifact type, the way
// a referrers tag schema index written by tooling that omits the field does.
type untypedReferrers struct{ content.ReadOnlyGraphStorage }

func (u untypedReferrers) Predecessors(ctx context.Context, desc ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error) {
	predecessors, err := u.ReadOnlyGraphStorage.Predecessors(ctx, desc)
	for i := range predecessors {
		predecessors[i].ArtifactType = ""
	}
	return predecessors, err
}

// pushJSON pushes v as content of the given media type.
func pushJSON(t *testing.T, ctx context.Context, store spec.Store, mediaType string, v any) ociImageSpecV1.Descriptor {
	t.Helper()
	raw, err := json.Marshal(v, runtime.JSONOptions)
	require.NoError(t, err)
	desc := content.NewDescriptorFromBytes(mediaType, raw)
	pushIfAbsent(t, ctx, store, desc, raw)
	return desc
}

// pushReferrerIndex pushes an image index without manifests as referrer of subject. An empty
// artifactType is omitted, which makes the index a referrer that listings report without one.
func pushReferrerIndex(t *testing.T, ctx context.Context, store spec.Store, subject ociImageSpecV1.Descriptor, artifactType string) ociImageSpecV1.Descriptor {
	t.Helper()
	subject = ociImageSpecV1.Descriptor{MediaType: subject.MediaType, Digest: subject.Digest, Size: subject.Size}
	idx := ociImageSpecV1.Index{
		MediaType:    ociImageSpecV1.MediaTypeImageIndex,
		ArtifactType: artifactType,
		Manifests:    []ociImageSpecV1.Descriptor{},
		Subject:      &subject,
	}
	idx.SchemaVersion = 2
	desc := pushJSON(t, ctx, store, idx.MediaType, idx)
	desc.ArtifactType = artifactType
	return desc
}

// pushConfigTypedReferrerManifest pushes a referrer of subject without artifactType whose config
// has the given media type. Registries report the config media type as its artifact type.
func pushConfigTypedReferrerManifest(t *testing.T, ctx context.Context, store spec.Store, subject ociImageSpecV1.Descriptor, configMediaType string) ociImageSpecV1.Descriptor {
	t.Helper()
	empty := ociImageSpecV1.DescriptorEmptyJSON
	pushIfAbsent(t, ctx, store, ociImageSpecV1.Descriptor{MediaType: empty.MediaType, Digest: empty.Digest, Size: empty.Size}, empty.Data)
	subject = ociImageSpecV1.Descriptor{MediaType: subject.MediaType, Digest: subject.Digest, Size: subject.Size}
	m := ociImageSpecV1.Manifest{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config:    ociImageSpecV1.Descriptor{MediaType: configMediaType, Digest: empty.Digest, Size: empty.Size},
		Layers:    []ociImageSpecV1.Descriptor{{MediaType: empty.MediaType, Digest: empty.Digest, Size: empty.Size}},
		Subject:   &subject,
	}
	m.SchemaVersion = 2
	desc := pushJSON(t, ctx, store, m.MediaType, m)
	desc.ArtifactType = configMediaType
	return desc
}

// signatureGraphFixture is a normalized component version in a CTF with access and signature
// referrers of its normalized manifest.
type signatureGraphFixture struct {
	repo       *oci.Repository
	store      spec.Store
	normalized ociImageSpecV1.Descriptor
	// access are the access referrers: the manifest written with the component version, an access
	// index, and a manifest that carries the access type only as config media type.
	access []ociImageSpecV1.Descriptor
	// signatures are the referrers a copy has to carry: a cosign signature and an attestation.
	signatures []ociImageSpecV1.Descriptor
}

func newSignatureGraphFixture(t *testing.T) signatureGraphFixture {
	t.Helper()
	r := require.New(t)
	ctx := t.Context()
	ctfStore := newCTFStore(t)
	repo := Repository(t, ocictf.WithCTF(ctfStore), oci.WithComponentVersionLayout(oci.LayoutNormalized))
	r.NoError(repo.AddComponentVersion(ctx, normDescriptor()))
	store, ref := componentStore(t, ctx, ctfStore, normComponent, normVersion)
	m, err := store.Resolve(ctx, ref)
	r.NoError(err)

	return signatureGraphFixture{
		repo:       repo,
		store:      store,
		normalized: m,
		access: []ociImageSpecV1.Descriptor{
			accessReferrerOf(t, ctx, store, m),
			pushReferrerIndex(t, ctx, store, m, ocidescriptor.ArtifactTypeAccessDescriptor),
			pushConfigTypedReferrerManifest(t, ctx, store, m, ocidescriptor.ArtifactTypeAccessDescriptor),
		},
		signatures: []ociImageSpecV1.Descriptor{
			pushReferrerManifest(t, ctx, store, m, cosignSignatureType),
			pushReferrerManifest(t, ctx, store, m, attestationType),
		},
	}
}

func digestsOf(descs []ociImageSpecV1.Descriptor) []digest.Digest {
	digests := make([]digest.Digest, 0, len(descs))
	for _, d := range descs {
		digests = append(digests, d.Digest)
	}
	return digests
}

func TestSignatureGraph_Predecessors(t *testing.T) {
	t.Run("listed artifact types decide without a fetch", func(t *testing.T) {
		r := require.New(t)
		f := newSignatureGraphFixture(t)
		src := newCountingGraph(f.store)

		got, err := f.repo.ExtendedCopyGraphOptions().FindPredecessors(t.Context(), oci.SignatureGraph(src), f.normalized)
		r.NoError(err)

		r.ElementsMatch(digestsOf(f.signatures), digestsOf(got), "access referrers must be hidden")
		r.Empty(src.fetches, "a listed artifact type must not cost a fetch")
		r.Equal(requestCounts(1, f.signatures...), src.exists, "only kept referrers are checked for existence")
		r.Equal(requestCounts(1, f.normalized), src.listings)
	})

	t.Run("referrer listed without artifact type is decided from its body", func(t *testing.T) {
		r := require.New(t)
		f := newSignatureGraphFixture(t)
		// The referrers API omits the artifact type of an image index that has none.
		untyped := pushReferrerIndex(t, t.Context(), f.store, f.normalized, "")
		src := newCountingGraph(f.store)

		got, err := f.repo.ExtendedCopyGraphOptions().FindPredecessors(t.Context(), oci.SignatureGraph(src), f.normalized)
		r.NoError(err)

		kept := append([]ociImageSpecV1.Descriptor{untyped}, f.signatures...)
		r.ElementsMatch(digestsOf(kept), digestsOf(got))
		r.Equal(requestCounts(1, untyped), src.fetches, "exactly the untyped referrer is fetched, once")
		r.Equal(requestCounts(1, kept...), src.exists)
	})

	t.Run("access referrers listed without artifact type are hidden", func(t *testing.T) {
		r := require.New(t)
		f := newSignatureGraphFixture(t)
		src := newCountingGraph(untypedReferrers{f.store})

		got, err := f.repo.ExtendedCopyGraphOptions().FindPredecessors(t.Context(), oci.SignatureGraph(src), f.normalized)
		r.NoError(err)

		r.ElementsMatch(digestsOf(f.signatures), digestsOf(got), "the body decides: manifest, index and config media type")
		r.Equal(requestCounts(1, append(f.access, f.signatures...)...), src.fetches, "one fetch per untyped referrer")
		r.Equal(requestCounts(1, f.signatures...), src.exists)
	})

	t.Run("untyped referrer of another media type is kept without a fetch", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()
		store := memory.New()
		subject := pushJSON(t, ctx, store, ociImageSpecV1.MediaTypeImageManifest, ociImageSpecV1.Manifest{MediaType: ociImageSpecV1.MediaTypeImageManifest})
		docker := ociImageSpecV1.Descriptor{MediaType: "application/vnd.docker.distribution.manifest.v2+json", Digest: digest.FromString("docker"), Size: 6}
		src := newCountingGraph(listedReferrers{ReadOnlyGraphStorage: store, referrers: []ociImageSpecV1.Descriptor{docker}})

		got, err := oci.SignatureGraph(src).Predecessors(ctx, subject)
		r.NoError(err)

		r.Equal([]ociImageSpecV1.Descriptor{docker}, got)
		r.Empty(src.fetches)
	})

	t.Run("peek error is returned", func(t *testing.T) {
		r := require.New(t)
		f := newSignatureGraphFixture(t)
		src := newCountingGraph(untypedReferrers{f.store})
		src.fetchErr = errors.New("registry unavailable")

		_, err := f.repo.ExtendedCopyGraphOptions().FindPredecessors(t.Context(), oci.SignatureGraph(src), f.normalized)

		r.ErrorIs(err, src.fetchErr, "an unpeeked referrer must fail the walk instead of being kept")
		r.Empty(src.exists)
	})

	t.Run("extended copy carries signatures only", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()
		f := newSignatureGraphFixture(t)
		src := newCountingGraph(f.store)
		dst := memory.New()

		r.NoError(oras.ExtendedCopyGraph(ctx, oci.SignatureGraph(src), dst, f.normalized, f.repo.ExtendedCopyGraphOptions()))

		for _, d := range append([]ociImageSpecV1.Descriptor{f.normalized}, f.signatures...) {
			exists, err := dst.Exists(ctx, d)
			r.NoError(err)
			r.True(exists, "%s must be copied", d.Digest)
		}
		for _, d := range f.access {
			exists, err := dst.Exists(ctx, d)
			r.NoError(err)
			r.False(exists, "access referrer %s must not be copied", d.Digest)
			r.Zero(src.fetches[d.Digest], "access referrer %s must not be fetched", d.Digest)
			r.Zero(src.exists[d.Digest], "access referrer %s must not be checked for existence", d.Digest)
		}
	})

	t.Run("referrer lister of the source is not promoted", func(t *testing.T) {
		r := require.New(t)
		f := newSignatureGraphFixture(t)
		_, ok := f.store.(registry.ReferrerLister)
		r.True(ok, "the fixture store must be a referrer lister for this test to mean anything")

		_, ok = oci.SignatureGraph(f.store).(registry.ReferrerLister)
		r.False(ok, "oras prefers Referrers over Predecessors and would bypass the filter")
	})
}

// listedReferrers lists fixed referrers for every subject.
type listedReferrers struct {
	content.ReadOnlyGraphStorage
	referrers []ociImageSpecV1.Descriptor
}

func (l listedReferrers) Predecessors(context.Context, ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error) {
	return l.referrers, nil
}

func TestRejectAccessReferrers(t *testing.T) {
	newSource := func(t *testing.T) (*memory.Store, ociImageSpecV1.Descriptor) {
		t.Helper()
		store := memory.New()
		subject := pushJSON(t, t.Context(), store, ociImageSpecV1.MediaTypeImageManifest, ociImageSpecV1.Manifest{MediaType: ociImageSpecV1.MediaTypeImageManifest})
		return store, subject
	}

	for _, tc := range []struct {
		name string
		push func(t *testing.T, store spec.Store, subject ociImageSpecV1.Descriptor) ociImageSpecV1.Descriptor
	}{
		{name: "image manifest", push: func(t *testing.T, store spec.Store, subject ociImageSpecV1.Descriptor) ociImageSpecV1.Descriptor {
			t.Helper()
			return pushReferrerManifest(t, t.Context(), store, subject, ocidescriptor.ArtifactTypeAccessDescriptor)
		}},
		{name: "image index", push: func(t *testing.T, store spec.Store, subject ociImageSpecV1.Descriptor) ociImageSpecV1.Descriptor {
			t.Helper()
			return pushReferrerIndex(t, t.Context(), store, subject, ocidescriptor.ArtifactTypeAccessDescriptor)
		}},
		{name: "image manifest typed by its config", push: func(t *testing.T, store spec.Store, subject ociImageSpecV1.Descriptor) ociImageSpecV1.Descriptor {
			t.Helper()
			return pushConfigTypedReferrerManifest(t, t.Context(), store, subject, ocidescriptor.ArtifactTypeAccessDescriptor)
		}},
	} {
		t.Run(tc.name+" with access artifact type is rejected", func(t *testing.T) {
			r := require.New(t)
			ctx := t.Context()
			store, subject := newSource(t)
			access := tc.push(t, store, subject)
			src := newCountingGraph(store)
			dst := memory.New()

			err := oras.CopyGraph(ctx, src, dst, access, oras.CopyGraphOptions{FindSuccessors: oci.RejectAccessReferrers(nil)})

			r.ErrorContains(err, "access referrer "+access.Digest.String())
			r.Equal(requestCounts(1, access), src.fetches, "the body is fetched once through the copy proxy and nothing else")
			r.Empty(src.exists)
			exists, err := dst.Exists(ctx, access)
			r.NoError(err)
			r.False(exists)
		})
	}

	t.Run("rejection does not delegate", func(t *testing.T) {
		r := require.New(t)
		store, subject := newSource(t)
		access := pushReferrerManifest(t, t.Context(), store, subject, ocidescriptor.ArtifactTypeAccessDescriptor)
		next := func(context.Context, content.Fetcher, ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error) {
			r.FailNow("next must not be called for an access referrer")
			return nil, nil
		}

		_, err := oci.RejectAccessReferrers(next)(t.Context(), store, access)
		r.Error(err)
	})

	t.Run("other manifests and indexes pass through to next", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()
		store, subject := newSource(t)
		signature := pushReferrerManifest(t, ctx, store, subject, cosignSignatureType)
		index := pushReferrerIndex(t, ctx, store, subject, attestationType)
		src := newCountingGraph(store)
		dst := memory.New()
		var delegated []digest.Digest
		next := func(ctx context.Context, fetcher content.Fetcher, desc ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error) {
			delegated = append(delegated, desc.Digest)
			// The subject is not part of this copy.
			return nil, nil
		}
		opts := oras.CopyGraphOptions{Concurrency: 1, FindSuccessors: oci.RejectAccessReferrers(next)}

		r.NoError(oras.CopyGraph(ctx, src, dst, signature, opts))
		r.NoError(oras.CopyGraph(ctx, src, dst, index, opts))

		r.Equal([]digest.Digest{signature.Digest, index.Digest}, delegated)
		r.Equal(requestCounts(1, signature, index), src.fetches, "the inspected body is the one the copy caches")
		for _, d := range []ociImageSpecV1.Descriptor{signature, index} {
			exists, err := dst.Exists(ctx, d)
			r.NoError(err)
			r.True(exists)
		}
	})

	t.Run("blobs are not fetched", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()
		store, _ := newSource(t)
		blob := pushJSON(t, ctx, store, "application/octet-stream", map[string]string{"artifactType": ocidescriptor.ArtifactTypeAccessDescriptor})
		src := newCountingGraph(store)

		successors, err := oci.RejectAccessReferrers(nil)(ctx, src, blob)
		r.NoError(err)
		r.Empty(successors)
		r.Empty(src.fetches)
	})
}
