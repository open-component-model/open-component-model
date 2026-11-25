package oci_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"io"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/memory"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	componentConfig "ocm.software/open-component-model/bindings/go/oci/spec/config/component"
	ocidescriptor "ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	normalizedlayout "ocm.software/open-component-model/bindings/go/oci/spec/layout/normalized/v1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	normComponent = "example.org/comp"
	normVersion   = "1.0.0"
)

func normDescriptor() *descriptor.Descriptor {
	return &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			Provider: descriptor.Provider{Name: "internal"},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: normComponent, Version: normVersion},
			},
		},
	}
}

// cloneDescriptor copies the parts of d that tests mutate.
func cloneDescriptor(d *descriptor.Descriptor) *descriptor.Descriptor {
	c := *d
	c.Component.Resources = make([]descriptor.Resource, len(d.Component.Resources))
	for i := range d.Component.Resources {
		c.Component.Resources[i] = *d.Component.Resources[i].DeepCopy()
	}
	c.Signatures = slices.Clone(d.Signatures)
	return &c
}

func withSignature(desc *descriptor.Descriptor, name string) *descriptor.Descriptor {
	desc = cloneDescriptor(desc)
	desc.Signatures = append(desc.Signatures, descriptor.Signature{
		Name: name,
		Digest: descriptor.Digest{
			HashAlgorithm:          "SHA-256",
			NormalisationAlgorithm: normalizedlayout.NormalisationAlgorithm,
			Value:                  "0000000000000000000000000000000000000000000000000000000000000000",
		},
		Signature: descriptor.SignatureInfo{Algorithm: "RSASSA-PSS", Value: "abcd", MediaType: "application/vnd.ocm.signature.rsa"},
	})
	return desc
}

func newCTFStore(t *testing.T) *ocictf.Store {
	t.Helper()
	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	require.NoError(t, err)
	return ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
}

// componentStore returns the OCI store and tag reference backing a component version in a CTF.
func componentStore(t *testing.T, ctx context.Context, s *ocictf.Store, component, version string) (spec.Store, string) {
	t.Helper()
	ref := s.ComponentVersionReference(ctx, component, version)
	store, err := s.StoreForReference(ctx, ref)
	require.NoError(t, err)
	return store, ref
}

func pushIfAbsent(t *testing.T, ctx context.Context, store spec.Store, desc ociImageSpecV1.Descriptor, raw []byte) {
	t.Helper()
	exists, err := store.Exists(ctx, desc)
	require.NoError(t, err)
	if !exists {
		require.NoError(t, store.Push(ctx, desc, bytes.NewReader(raw)))
	}
}

// pushAccessManifest pushes an access referrer of subject carrying desc and the given local blob
// layers, the way a third party with push rights could. It stamps a creation time one hour ahead so
// the pushed referrer is strictly newer than one AddComponentVersion wrote and wins selection. salt
// only varies the manifest digest.
func pushAccessManifest(t *testing.T, ctx context.Context, store spec.Store, subject ociImageSpecV1.Descriptor, desc *descriptor.Descriptor, layers []ociImageSpecV1.Descriptor, salt string) ociImageSpecV1.Descriptor {
	t.Helper()
	r := require.New(t)
	buf, err := ocidescriptor.SingleFileEncodeDescriptor(testScheme, desc, ocidescriptor.MediaTypeComponentDescriptorJSON)
	r.NoError(err)
	layerDesc := content.NewDescriptorFromBytes(ocidescriptor.MediaTypeComponentDescriptorJSON, buf.Bytes())
	pushIfAbsent(t, ctx, store, layerDesc, buf.Bytes())
	cfgRaw, cfgDesc, err := componentConfig.New(layerDesc)
	r.NoError(err)
	pushIfAbsent(t, ctx, store, cfgDesc, cfgRaw)

	subject = ociImageSpecV1.Descriptor{MediaType: subject.MediaType, Digest: subject.Digest, Size: subject.Size}
	m := normalizedlayout.BuildAccessManifest(&subject, cfgDesc, layerDesc, layers, desc.Component.Name, desc.Component.Version)
	m.Annotations[ociImageSpecV1.AnnotationCreated] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	if salt != "" {
		m.Annotations["test.salt"] = salt
	}
	raw, err := json.Marshal(m, runtime.JSONOptions)
	r.NoError(err)
	mDesc := content.NewDescriptorFromBytes(m.MediaType, raw)
	mDesc.ArtifactType = m.ArtifactType
	pushIfAbsent(t, ctx, store, mDesc, raw)
	return mDesc
}

// fetchJSON fetches desc from store and decodes it into v.
func fetchJSON(t *testing.T, ctx context.Context, store content.Fetcher, desc ociImageSpecV1.Descriptor, v any) {
	t.Helper()
	raw, err := content.FetchAll(ctx, store, desc)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, v))
}

// readAll drains a readable blob.
func readAll(t *testing.T, b interface {
	ReadCloser() (io.ReadCloser, error)
},
) []byte {
	t.Helper()
	rc, err := b.ReadCloser()
	require.NoError(t, err)
	data, readErr := io.ReadAll(rc)
	require.NoError(t, rc.Close())
	require.NoError(t, readErr)
	return data
}

func TestNormalizedLayout_AccessReferrerSelection(t *testing.T) {
	tampered := normDescriptor()
	tampered.Component.Provider.Name = "attacker"

	for _, tc := range []struct {
		name string
		// referrer is pushed as a strictly newer access referrer than the one AddDescriptorToStore wrote.
		referrer      *descriptor.Descriptor
		wantSignature bool
	}{
		{name: "a newer valid referrer is selected", referrer: withSignature(normDescriptor(), "ocm-sig"), wantSignature: true},
		{name: "a newer referrer failing the bind check is ignored", referrer: tampered, wantSignature: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			ctx := t.Context()
			store := memory.New()
			m, err := oci.AddDescriptorToStore(ctx, store, normDescriptor(), oci.AddDescriptorOptions{Scheme: testScheme, Layout: oci.LayoutNormalized})
			r.NoError(err)
			pushAccessManifest(t, ctx, store, *m, tc.referrer, nil, "")

			got, err := oci.GetNormalizedComponentVersion(ctx, store, *m, ocidescriptor.DefaultDescriptorUnmarshalFunc)
			r.NoError(err)
			r.Equal("internal", got.Component.Provider.Name, "a bind-check failure must never win over the genuine referrer")
			r.Equal(tc.wantSignature, len(got.Signatures) == 1)
		})
	}
}

func TestNormalizedLayout_RepositoryRoundTrip(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()
	repo := Repository(t, ocictf.WithCTF(newCTFStore(t)), oci.WithComponentVersionLayout(oci.LayoutNormalized))
	desc := normDescriptor()

	_, err := repo.GetComponentVersion(ctx, normComponent, normVersion)
	r.ErrorIs(err, repository.ErrNotFound)
	_, err = repo.GetComponentVersionLayout(ctx, normComponent, normVersion)
	r.ErrorIs(err, repository.ErrNotFound)

	resContent := []byte("normalized local resource content")
	res, err := repo.AddLocalResource(ctx, normComponent, normVersion, &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "res", Version: "1.0.0"}},
		Type:        "blob", Relation: descriptor.LocalRelation,
		Access: &v2.LocalBlob{LocalReference: digest.FromBytes(resContent).String(), MediaType: "application/octet-stream"},
	}, inmemory.New(bytes.NewReader(resContent)))
	r.NoError(err)
	srcContent := []byte("normalized local source content")
	src, err := repo.AddLocalSource(ctx, normComponent, normVersion, &descriptor.Source{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "src", Version: "1.0.0"}},
		Type:        "blob",
		Access:      &v2.LocalBlob{LocalReference: digest.FromBytes(srcContent).String(), MediaType: "application/octet-stream"},
	}, inmemory.New(bytes.NewReader(srcContent)))
	r.NoError(err)
	desc.Component.Resources = []descriptor.Resource{*res}
	desc.Component.Sources = []descriptor.Source{*src}
	r.NoError(repo.AddComponentVersion(ctx, desc))

	got, err := repo.GetComponentVersion(ctx, normComponent, normVersion)
	r.NoError(err)
	r.Equal(normComponent, got.Component.Name)
	gotLayout, err := repo.GetComponentVersionLayout(ctx, normComponent, normVersion)
	r.NoError(err)
	r.Equal(string(layout.ComponentVersionLayoutNormalized), gotLayout)

	b, _, err := repo.GetLocalResource(ctx, normComponent, normVersion, runtime.Identity{"name": "res"})
	r.NoError(err)
	r.Equal(resContent, readAll(t, b))
	sb, _, err := repo.GetLocalSource(ctx, normComponent, normVersion, runtime.Identity{"name": "src"})
	r.NoError(err)
	r.Equal(srcContent, readAll(t, sb))
}

func TestNormalizedLayout_ReAddUsesConfiguredLayout(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()
	ctfStore := newCTFStore(t)
	normalized := Repository(t, ocictf.WithCTF(ctfStore), oci.WithComponentVersionLayout(oci.LayoutNormalized))
	v2Repo := Repository(t, ocictf.WithCTF(ctfStore))
	store, ref := componentStore(t, ctx, ctfStore, normComponent, normVersion)

	r.NoError(normalized.AddComponentVersion(ctx, normDescriptor()))
	original, err := store.Resolve(ctx, ref)
	r.NoError(err)

	// Persisting an OCM signature through a repository with the default layout rewrites the
	// version in the v2 layout.
	r.NoError(v2Repo.AddComponentVersion(ctx, withSignature(normDescriptor(), "ocm-sig")))
	rewritten, err := store.Resolve(ctx, ref)
	r.NoError(err)
	r.NotEqual(original.Digest, rewritten.Digest, "the tag must move to the v2 manifest")
	gotLayout, err := v2Repo.GetComponentVersionLayout(ctx, normComponent, normVersion)
	r.NoError(err)
	r.Equal(string(layout.ComponentVersionLayoutDefault), gotLayout)
	got, err := v2Repo.GetComponentVersion(ctx, normComponent, normVersion)
	r.NoError(err)
	r.Len(got.Signatures, 1)

	// Persisting it through a repository with the normalized layout restores the normalized
	// manifest: signatures are not part of the normalized descriptor, so its digest is unchanged.
	r.NoError(normalized.AddComponentVersion(ctx, withSignature(normDescriptor(), "ocm-sig")))
	restored, err := store.Resolve(ctx, ref)
	r.NoError(err)
	r.Equal(original.Digest, restored.Digest, "the tag must be back on the original normalized manifest")
	got, err = normalized.GetComponentVersion(ctx, normComponent, normVersion)
	r.NoError(err)
	r.Len(got.Signatures, 1, "the re-added descriptor must be read back, not the stale one")
}

// accessReferrerOf returns the access referrer manifest of the normalized manifest m, found through
// the referrers graph.
func accessReferrerOf(t *testing.T, ctx context.Context, store spec.Store, m ociImageSpecV1.Descriptor) ociImageSpecV1.Descriptor {
	t.Helper()
	r := require.New(t)
	predecessors, err := store.Predecessors(ctx, m)
	r.NoError(err)
	for _, p := range predecessors {
		var header struct {
			ArtifactType string `json:"artifactType"`
		}
		raw, err := content.FetchAll(ctx, store, p)
		r.NoError(err)
		r.NoError(json.Unmarshal(raw, &header))
		if header.ArtifactType == ocidescriptor.ArtifactTypeAccessDescriptor {
			return p
		}
	}
	r.FailNow("no access referrer found for normalized manifest")
	return ociImageSpecV1.Descriptor{}
}

// tamperAccessReferrer pushes a newer access referrer of the normalized version that carries desc
// and the local blob layers of the current access manifest, so it wins selection on read.
func tamperAccessReferrer(t *testing.T, ctx context.Context, store spec.Store, ref string, desc *descriptor.Descriptor) {
	t.Helper()
	r := require.New(t)
	m, err := store.Resolve(ctx, ref)
	r.NoError(err)
	acc := accessReferrerOf(t, ctx, store, m)
	var accManifest ociImageSpecV1.Manifest
	fetchJSON(t, ctx, store, acc, &accManifest)
	layers := slices.Clone(accManifest.Layers[1:])
	pushAccessManifest(t, ctx, store, m, desc, layers, "tampered")
}

func TestNormalizedLayout_LocalBlobVerification(t *testing.T) {
	addBlob := func(t *testing.T, ctx context.Context, repo *oci.Repository, name string, data []byte) descriptor.Resource {
		t.Helper()
		res, err := repo.AddLocalResource(ctx, normComponent, normVersion, &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: name, Version: "1.0.0"}},
			Type:        "blob", Relation: descriptor.LocalRelation,
			Access: &v2.LocalBlob{LocalReference: digest.FromBytes(data).String(), MediaType: "application/octet-stream"},
		}, inmemory.New(bytes.NewReader(data)))
		require.NoError(t, err)
		return *res
	}

	t.Run("local reference swapped to another blob of the access referrer", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()
		ctfStore := newCTFStore(t)
		repo := Repository(t, ocictf.WithCTF(ctfStore), oci.WithComponentVersionLayout(oci.LayoutNormalized))
		desc := normDescriptor()
		desc.Component.Resources = []descriptor.Resource{
			addBlob(t, ctx, repo, "one", []byte("signed content one")),
			addBlob(t, ctx, repo, "two", []byte("signed content two")),
		}
		r.NoError(repo.AddComponentVersion(ctx, desc))

		// Accesses are not normalized, so the swapped descriptor still passes the bind check.
		evil := cloneDescriptor(desc)
		evil.Component.Resources[0].Access = desc.Component.Resources[1].Access
		store, ref := componentStore(t, ctx, ctfStore, normComponent, normVersion)
		tamperAccessReferrer(t, ctx, store, ref, evil)

		_, err := repo.GetComponentVersion(ctx, normComponent, normVersion)
		r.NoError(err)
		_, _, err = repo.GetLocalResource(ctx, normComponent, normVersion, runtime.Identity{"name": "one"})
		r.ErrorContains(err, "digest mismatch")
	})

	t.Run("local reference to a blob outside the access referrer", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()
		ctfStore := newCTFStore(t)
		repo := Repository(t, ocictf.WithCTF(ctfStore), oci.WithComponentVersionLayout(oci.LayoutNormalized))
		desc := normDescriptor()
		desc.Component.Resources = []descriptor.Resource{addBlob(t, ctx, repo, "one", []byte("signed content one"))}
		r.NoError(repo.AddComponentVersion(ctx, desc))

		store, ref := componentStore(t, ctx, ctfStore, normComponent, normVersion)
		orphan := []byte("orphan blob in the same repository")
		orphanDesc := content.NewDescriptorFromBytes("application/octet-stream", orphan)
		pushIfAbsent(t, ctx, store, orphanDesc, orphan)
		evil := cloneDescriptor(desc)
		evil.Component.Resources[0].Access = &v2.LocalBlob{
			Type:           runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
			LocalReference: orphanDesc.Digest.String(),
			MediaType:      "application/octet-stream",
		}
		tamperAccessReferrer(t, ctx, store, ref, evil)

		_, _, err := repo.GetLocalResource(ctx, normComponent, normVersion, runtime.Identity{"name": "one"})
		r.ErrorContains(err, "no matching descriptor found")
	})
}

func TestNormalizedLayout_ListComponentVersions(t *testing.T) {
	for name, policy := range map[string]oci.ReferrerTrackingPolicy{
		"none":              oci.ReferrerTrackingPolicyNone,
		"index and subject": oci.ReferrerTrackingPolicyByIndexAndSubject,
	} {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			ctx := t.Context()
			ctfStore := newCTFStore(t)
			normalized := Repository(t, ocictf.WithCTF(ctfStore), oci.WithReferrerTrackingPolicy(policy), oci.WithComponentVersionLayout(oci.LayoutNormalized))
			v2Repo := Repository(t, ocictf.WithCTF(ctfStore), oci.WithReferrerTrackingPolicy(policy), oci.WithComponentVersionLayout(oci.LayoutV2))

			r.NoError(normalized.AddComponentVersion(ctx, normDescriptor()))
			v2Desc := normDescriptor()
			v2Desc.Component.Version = "2.0.0"
			r.NoError(v2Repo.AddComponentVersion(ctx, v2Desc))

			versions, err := normalized.ListComponentVersions(ctx, normComponent)
			r.NoError(err)
			r.Equal([]string{"2.0.0", normVersion}, versions)
		})
	}
}

func TestNormalizedLayout_CarryComponentSignatures(t *testing.T) {
	const cosignType = "application/vnd.dev.cosign.artifact.sig.v1+json"
	newRepos := func(t *testing.T, srcLayout, dstLayout oci.Layout) (src, dst *oci.Repository, srcCTF, dstCTF *ocictf.Store) {
		t.Helper()
		srcCTF, dstCTF = newCTFStore(t), newCTFStore(t)
		src = Repository(t, ocictf.WithCTF(srcCTF), oci.WithComponentVersionLayout(srcLayout))
		dst = Repository(t, ocictf.WithCTF(dstCTF), oci.WithComponentVersionLayout(dstLayout))
		return src, dst, srcCTF, dstCTF
	}
	pushSignature := func(t *testing.T, ctx context.Context, s *ocictf.Store) ociImageSpecV1.Descriptor {
		t.Helper()
		store, ref := componentStore(t, ctx, s, normComponent, normVersion)
		m, err := store.Resolve(ctx, ref)
		require.NoError(t, err)
		return pushReferrerManifest(t, ctx, store, m, cosignType)
	}

	t.Run("carries signatures but not access referrers", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()
		src, dst, srcCTF, dstCTF := newRepos(t, oci.LayoutNormalized, oci.LayoutNormalized)
		r.NoError(src.AddComponentVersion(ctx, normDescriptor()))
		r.NoError(dst.AddComponentVersion(ctx, withSignature(normDescriptor(), "differs-only-in-access-referrer")))
		sig := pushSignature(t, ctx, srcCTF)
		srcStore, srcRef := componentStore(t, ctx, srcCTF, normComponent, normVersion)
		m, err := srcStore.Resolve(ctx, srcRef)
		r.NoError(err)
		srcAcc := accessReferrerOf(t, ctx, srcStore, m)

		r.NoError(dst.CarryComponentSignatures(ctx, src, normComponent, normVersion))

		dstStore, _ := componentStore(t, ctx, dstCTF, normComponent, normVersion)
		exists, err := dstStore.Exists(ctx, sig)
		r.NoError(err)
		r.True(exists)
		exists, err = dstStore.Exists(ctx, srcAcc)
		r.NoError(err)
		r.False(exists, "access referrers are regenerated per repository and must not be carried")
	})

	t.Run("v2 target is skipped", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()
		src, dst, srcCTF, dstCTF := newRepos(t, oci.LayoutNormalized, oci.LayoutV2)
		r.NoError(src.AddComponentVersion(ctx, normDescriptor()))
		r.NoError(dst.AddComponentVersion(ctx, normDescriptor()))
		sig := pushSignature(t, ctx, srcCTF)

		r.NoError(dst.CarryComponentSignatures(ctx, src, normComponent, normVersion))

		dstStore, _ := componentStore(t, ctx, dstCTF, normComponent, normVersion)
		exists, err := dstStore.Exists(ctx, sig)
		r.NoError(err)
		r.False(exists, "signatures must not be carried into a v2 target")
	})

	t.Run("normalized digest mismatch is an error", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()
		src, dst, srcCTF, _ := newRepos(t, oci.LayoutNormalized, oci.LayoutNormalized)
		r.NoError(src.AddComponentVersion(ctx, normDescriptor()))
		other := normDescriptor()
		other.Component.Provider.Name = "someone-else"
		r.NoError(dst.AddComponentVersion(ctx, other))
		pushSignature(t, ctx, srcCTF)

		r.ErrorContains(dst.CarryComponentSignatures(ctx, src, normComponent, normVersion), "differs from normalized source manifest")
	})
}

// pushReferrerManifest pushes a minimal referrer of subject with the given artifact type, standing
// in for a cosign signature.
func pushReferrerManifest(t *testing.T, ctx context.Context, store spec.Store, subject ociImageSpecV1.Descriptor, artifactType string) ociImageSpecV1.Descriptor {
	t.Helper()
	r := require.New(t)
	payload := []byte("signature payload for " + subject.Digest.String())
	layer := content.NewDescriptorFromBytes("application/octet-stream", payload)
	pushIfAbsent(t, ctx, store, layer, payload)
	pushIfAbsent(t, ctx, store, ociImageSpecV1.DescriptorEmptyJSON, ociImageSpecV1.DescriptorEmptyJSON.Data)
	subject = ociImageSpecV1.Descriptor{MediaType: subject.MediaType, Digest: subject.Digest, Size: subject.Size}
	m := ociImageSpecV1.Manifest{
		MediaType:    ociImageSpecV1.MediaTypeImageManifest,
		ArtifactType: artifactType,
		Config:       ociImageSpecV1.DescriptorEmptyJSON,
		Layers:       []ociImageSpecV1.Descriptor{layer},
		Subject:      &subject,
	}
	m.SchemaVersion = 2
	raw, err := json.Marshal(m, runtime.JSONOptions)
	r.NoError(err)
	desc := content.NewDescriptorFromBytes(m.MediaType, raw)
	desc.ArtifactType = artifactType
	pushIfAbsent(t, ctx, store, desc, raw)
	return desc
}
