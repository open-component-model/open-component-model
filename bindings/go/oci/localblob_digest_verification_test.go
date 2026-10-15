package oci_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	"ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Local-blob digest verification: a resource's recorded digest is checked against
// the actual content at every repository step that handles a local blob, for both
// normalisations:
//
//   - genericBlobDigest/v1 : plain blob, digest is the hash of the raw bytes
//   - ociArtifactDigest/v1 : OCI image layout, digest is the manifest/index root
//
// Steps covered:
//
//   - AddLocalResource (upload): a nil digest is computed and recorded; a correct
//     digest is accepted; a wrong digest is rejected; a digest the pack path cannot
//     verify is rejected rather than stored verbatim.
//   - GetLocalResource (local read): the recorded digest is verified against the
//     stored content.
//   - DownloadResource (global read, via GlobalAccess/OCIImage): same contract.
//
// Construction and transfer resolve local blobs through these same repository
// methods, so they inherit the contract and are not re-exercised here.

// localBlobFixture builds a local-blob resource's content and the digest
// AddLocalResource must record for it, pinned independently of the pack path so a
// wrong computation is caught instead of confirmed.
type localBlobFixture struct {
	name  string
	build func(t *testing.T) (content []byte, mediaType string, expected *descriptor.Digest)
}

var localBlobFixtures = []localBlobFixture{
	{
		name: "genericBlobDigest",
		// genericBlobDigest/v1 is the SHA-256 of the raw blob bytes.
		build: func(t *testing.T) ([]byte, string, *descriptor.Digest) {
			t.Helper()
			return []byte("hello local blob"), "text/plain", &descriptor.Digest{
				HashAlgorithm:          "SHA-256",
				NormalisationAlgorithm: "genericBlobDigest/v1",
				Value:                  "c37fca6a695a9c6b03d313faaaebc29a11f39d46919290325d5649583ec8162b",
			}
		},
	},
	{
		name:  "ociArtifactDigest",
		build: ociLayoutBlob,
	},
}

// TestLocalBlobDigest_AddLocalResource covers upload-time digest handling for both
// normalisations. The expected digest is pinned independently of the pack path, so
// each row checks what Add records against a value Add did not itself produce.
func TestLocalBlobDigest_AddLocalResource(t *testing.T) {
	cases := []struct {
		name string
		// digest builds the digest preset on the resource before Add, given the
		// correct digest for the content. nil means no digest is preset.
		digest  func(correct *descriptor.Digest) *descriptor.Digest
		wantErr bool
	}{
		{
			name:   "nil is computed and recorded",
			digest: func(*descriptor.Digest) *descriptor.Digest { return nil },
		},
		{
			name:   "correct is accepted",
			digest: func(correct *descriptor.Digest) *descriptor.Digest { return correct },
		},
		{
			name: "wrong value is rejected",
			digest: func(correct *descriptor.Digest) *descriptor.Digest {
				d := correct.DeepCopy()
				d.Value = corrupt(d.Value)
				return d
			},
			wantErr: true,
		},
		{
			name: "wrong value under mismatched normalisation is rejected",
			digest: func(correct *descriptor.Digest) *descriptor.Digest {
				d := correct.DeepCopy()
				d.NormalisationAlgorithm = otherNormalisation(d.NormalisationAlgorithm)
				d.Value = corrupt(d.Value)
				return d
			},
			wantErr: true,
			// A wrong value must be rejected whichever normalisation label it claims,
			// so neither the generic nor the layout path stores it.
		},
	}

	for _, f := range localBlobFixtures {
		t.Run(f.name, func(t *testing.T) {
			content, mediaType, expected := f.build(t)
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					r := require.New(t)
					ctx := t.Context()
					repo := newCTFRepository(t)

					res := newLocalBlobResource(mediaType)
					res.Digest = tc.digest(expected)

					added, err := repo.AddLocalResource(ctx, "ocm.software/repro", "1.0.0", res,
						inmemory.New(bytes.NewReader(content)))

					if tc.wantErr {
						r.ErrorContains(err, "resource blob digest", "AddLocalResource must reject this digest")
						return
					}
					r.NoError(err)
					r.NotNil(added.Digest, "AddLocalResource must record a digest")
					r.Equal(expected.NormalisationAlgorithm, added.Digest.NormalisationAlgorithm)
					r.Equal(expected.Value, added.Digest.Value, "recorded digest must match the content")
				})
			}
		})
	}
}

// TestLocalBlobDigest_AddAcceptsSwappedNormalisationLabel asserts the generic<->oci
// interchange: a correct value is accepted whichever of the two labels it carries,
// and the label is preserved, not rewritten, on an already-complete digest.
func TestLocalBlobDigest_AddAcceptsSwappedNormalisationLabel(t *testing.T) {
	for _, f := range localBlobFixtures {
		t.Run(f.name, func(t *testing.T) {
			r := require.New(t)
			content, mediaType, expected := f.build(t)

			swapped := expected.DeepCopy()
			swapped.NormalisationAlgorithm = otherNormalisation(swapped.NormalisationAlgorithm)

			res := newLocalBlobResource(mediaType)
			res.Digest = swapped

			added, err := newCTFRepository(t).AddLocalResource(t.Context(), "ocm.software/repro", "1.0.0", res,
				inmemory.New(bytes.NewReader(content)))
			r.NoError(err, "a correct value must be accepted under either normalisation label")
			r.Equal(expected.Value, added.Digest.Value)
			r.Equal(swapped.NormalisationAlgorithm, added.Digest.NormalisationAlgorithm,
				"a complete digest's label must be preserved, not rewritten")
		})
	}
}

// TestLocalBlobDigest_GetLocalResource_V1ArtifactSet reads the legacy v1 fixture
// whose OCI artifact is stored as a tar.gz artifact set: its localReference is the
// archive hash, while the recorded ociArtifactDigest is the inner manifest root. The
// read must verify the recorded digest against the archive's declared root, not the
// archive hash, and so must succeed.
func TestLocalBlobDigest_GetLocalResource_V1ArtifactSet(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	fs, err := filesystem.NewFS(
		filepath.Join("..", "ctf", "testdata", "compatibility", "02", "with-resource"), os.O_RDONLY)
	r.NoError(err)
	repo := Repository(t, ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))))

	cv, err := repo.GetComponentVersion(ctx, "github.com/acme.org/helloworld", "1.0.0")
	r.NoError(err)
	r.Len(cv.Component.Resources, 1)
	res := cv.Component.Resources[0]
	r.Equal("ociArtifactDigest/v1", res.Digest.NormalisationAlgorithm)

	blob, _, err := repo.GetLocalResource(ctx, "github.com/acme.org/helloworld", "1.0.0", res.ToIdentity())
	r.NoError(err, "a v1 artifact set must verify against its declared root, not the archive hash")
	_, err = io.ReadAll(mustReader(t, blob))
	r.NoError(err)
}

// TestLocalBlobDigest_AddLocalResource_V1ArtifactSet adds the real v1 artifact-set
// archive through the generic pack path (its media type is not a layout type). The
// layer digest is the archive hash, but the recorded ociArtifactDigest is the inner
// manifest root, so the pack must verify against the archive's declared root. A wrong
// root must still be rejected.
func TestLocalBlobDigest_AddLocalResource_V1ArtifactSet(t *testing.T) {
	r := require.New(t)
	const root = "62be4af3382a4493cb7f1dd4ec47bcb28f1863b615fc9e4a1dceefbe93898dd0"
	archive, err := os.ReadFile(filepath.Join("..", "ctf", "testdata", "compatibility", "02", "with-resource",
		"blobs", "sha256.e40e3a2f1ab1a98328dfd14539a79d27aff5c4d5c34cd16a85f0288bfa76490b"))
	r.NoError(err)

	newV1Resource := func(value string) *descriptor.Resource {
		return &descriptor.Resource{
			Relation:    descriptor.LocalRelation,
			ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "mychart", Version: "6.7.1"}},
			Type:        "test-type",
			Access:      &v2.LocalBlob{MediaType: "application/vnd.oci.image.manifest.v1+tar+gzip"},
			Digest:      &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "ociArtifactDigest/v1", Value: value},
		}
	}

	added, err := newCTFRepository(t).AddLocalResource(t.Context(), "ocm.software/repro", "1.0.0",
		newV1Resource(root), inmemory.New(bytes.NewReader(archive)))
	r.NoError(err, "a v1 artifact set must verify against its declared root, not the archive hash")
	r.Equal(root, added.Digest.Value, "the recorded root must be preserved, not rewritten to the archive hash")

	_, err = newCTFRepository(t).AddLocalResource(t.Context(), "ocm.software/repro", "1.0.0",
		newV1Resource(corrupt(root)), inmemory.New(bytes.NewReader(archive)))
	r.ErrorContains(err, "resource blob digest", "a wrong artifact-set root must be rejected")
}

// TestLocalBlobDigest_GetLocalResource covers local read-time verification. The
// content stays addressed by its real localReference, so only res.Digest is
// corrupted, exactly what a tampered or forged signed descriptor would carry.
func TestLocalBlobDigest_GetLocalResource(t *testing.T) {
	cases := []struct {
		name    string
		tamper  func(*descriptor.Digest) // mutate the recorded digest before commit; nil = untouched
		wantErr bool
	}{
		{
			name: "correct digest round-trips",
		},
		{
			name:    "corrupted digest is rejected",
			tamper:  func(d *descriptor.Digest) { d.Value = corrupt(d.Value) },
			wantErr: true,
		},
		{
			// A normalisation this package cannot recompute from content carries a
			// value that is not the content hash, so it is served unverified, not rejected.
			name:   "non-recomputable normalisation is not verified",
			tamper: func(d *descriptor.Digest) { d.NormalisationAlgorithm = "jsonNormalisation/v1" },
		},
	}

	for _, f := range localBlobFixtures {
		t.Run(f.name, func(t *testing.T) {
			content, mediaType, _ := f.build(t)
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					r := require.New(t)
					ctx := t.Context()
					repo := newCTFRepository(t)
					addAndCommit(t, repo, newLocalBlobResource(mediaType), content, tc.tamper)

					blob, _, err := repo.GetLocalResource(ctx, "ocm.software/repro", "1.0.0",
						map[string]string{"name": "repro", "version": "1.0.0"})

					if tc.wantErr {
						r.ErrorContains(err, "resource digest verification failed", "GetLocalResource must reject content whose digest does not match res.Digest")
						return
					}
					r.NoError(err, "a resource with a correct digest must be retrievable")
					_, err = io.ReadAll(mustReader(t, blob))
					r.NoError(err)
				})
			}
		})
	}
}

// TestLocalBlobDigest_DownloadResource is the global-read counterpart, covering both
// download branches: an OCIImage access (ociArtifactDigest/v1) resolved as a stream,
// and an OCIImageLayer access (genericBlobDigest/v1) fetched as a raw layer.
func TestLocalBlobDigest_DownloadResource(t *testing.T) {
	cases := []struct {
		name    string
		tamper  func(*descriptor.Digest)
		wantErr bool
	}{
		{
			name: "correct digest round-trips",
		},
		{
			name:    "corrupted digest is rejected",
			tamper:  func(d *descriptor.Digest) { d.Value = corrupt(d.Value) },
			wantErr: true,
		},
	}

	scenarios := []struct {
		name string
		// setup provisions a repository and returns a resource whose access resolves
		// in it, with the recorded digest it must be verified against.
		setup func(t *testing.T) (*oci.Repository, *descriptor.Resource)
	}{
		{
			name:  "OCIImage stream (ociArtifactDigest)",
			setup: downloadStreamResource,
		},
		{
			name:  "OCIImageLayer raw (genericBlobDigest)",
			setup: downloadLayerResource,
		},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					r := require.New(t)
					repo, added := sc.setup(t)
					if tc.tamper != nil {
						tc.tamper(added.Digest)
					}

					blob, err := repo.DownloadResource(t.Context(), added)
					if tc.wantErr {
						r.ErrorContains(err, "resource digest verification failed", "DownloadResource must reject an artifact whose digest does not match res.Digest")
						return
					}
					r.NoError(err, "a resource with a correct digest must be downloadable")
					_, err = io.ReadAll(mustReader(t, blob))
					r.NoError(err)
				})
			}
		})
	}
}

// downloadStreamResource uploads an OCI layout and returns a resource whose OCIImage
// access resolves to it, exercising the stream branch of download().
func downloadStreamResource(t *testing.T) (*oci.Repository, *descriptor.Resource) {
	t.Helper()
	r := require.New(t)
	repo := newCTFRepository(t)
	layoutBytes, _, expected := ociLayoutBlob(t)

	res := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: "repro", Version: "1.0.0"},
		},
		Type: "test-type",
		Access: &v1.OCIImage{
			Type:           runtime.NewVersionedType(v1.OCIImageType, v1.Version),
			ImageReference: "test-image:latest",
		},
	}
	added, err := repo.UploadResource(t.Context(), res, inmemory.New(bytes.NewReader(layoutBytes)))
	r.NoError(err)
	r.Equal(expected.NormalisationAlgorithm, added.Digest.NormalisationAlgorithm)
	r.Equal(expected.Value, added.Digest.Value, "recorded digest must match the artifact")
	return repo, added
}

// downloadLayerResource pushes a raw layer and returns a resource whose OCIImageLayer
// access resolves to it, exercising the layer branch of download(). The recorded
// genericBlobDigest value is the layer's own hash, pinned independently of download.
func downloadLayerResource(t *testing.T) (*oci.Repository, *descriptor.Resource) {
	t.Helper()
	r := require.New(t)
	ctx := t.Context()

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo := Repository(t, ocictf.WithCTF(store))

	payload := []byte("layer download content")
	dig := digest.FromBytes(payload)
	layerStore, err := store.StoreForReference(ctx, "test-registry/test-layer")
	r.NoError(err)
	r.NoError(layerStore.Push(ctx, ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageLayer,
		Digest:    dig,
		Size:      int64(len(payload)),
	}, bytes.NewReader(payload)))

	return repo, &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: "repro", Version: "1.0.0"},
		},
		Type: "test-type",
		Access: &v1.OCIImageLayer{
			Type:      runtime.NewVersionedType(v1.OCIImageLayerType, v1.Version),
			Reference: "test-registry/test-layer@" + dig.String(),
			MediaType: ociImageSpecV1.MediaTypeImageLayer,
			Digest:    dig,
			Size:      int64(len(payload)),
		},
		Digest: &descriptor.Digest{
			HashAlgorithm:          "SHA-256",
			NormalisationAlgorithm: "genericBlobDigest/v1",
			Value:                  dig.Encoded(),
		},
	}
}

func newLocalBlobResource(mediaType string) *descriptor.Resource {
	return &descriptor.Resource{
		Relation: descriptor.LocalRelation,
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: "repro", Version: "1.0.0"},
		},
		Type:   "test-type",
		Access: &v2.LocalBlob{MediaType: mediaType},
	}
}

func newCTFRepository(t *testing.T) *oci.Repository {
	t.Helper()
	r := require.New(t)
	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	return Repository(t, ocictf.WithCTF(store))
}

// addAndCommit adds the resource, optionally mutates the recorded digest (to simulate
// a tampered descriptor), then stores a component version referencing it.
func addAndCommit(t *testing.T, repo *oci.Repository, res *descriptor.Resource, content []byte, tamper func(*descriptor.Digest)) *descriptor.Resource {
	t.Helper()
	r := require.New(t)
	ctx := t.Context()

	added, err := repo.AddLocalResource(ctx, "ocm.software/repro", "1.0.0", res,
		inmemory.New(bytes.NewReader(content)))
	r.NoError(err)
	if tamper != nil {
		tamper(added.Digest)
	}

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			Provider:      descriptor.Provider{Name: "acme"},
			ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: "ocm.software/repro", Version: "1.0.0"}},
			Resources:     []descriptor.Resource{*added},
		},
	}
	r.NoError(repo.AddComponentVersion(ctx, desc))
	return added
}

// otherNormalisation swaps between the two local-blob normalisations.
func otherNormalisation(n string) string {
	if n == "genericBlobDigest/v1" {
		return "ociArtifactDigest/v1"
	}
	return "genericBlobDigest/v1"
}

// corrupt returns a same-length but different hex digest value.
func corrupt(v string) string {
	b := []byte(v)
	for i := range b {
		if b[i] == '0' {
			b[i] = '1'
		} else {
			b[i] = '0'
		}
	}
	return string(b)
}

func mustReader(t *testing.T, b interface{ ReadCloser() (io.ReadCloser, error) }) io.ReadCloser {
	t.Helper()
	rc, err := b.ReadCloser()
	require.NoError(t, err)
	t.Cleanup(func() { _ = rc.Close() })
	return rc
}

// ociLayoutBlob builds a minimal single-layer OCI image layout tar, returning its
// bytes, the layout media type, and the digest AddLocalResource must record for it.
// ociArtifactDigest/v1 is the manifest root digest, taken from the manifest oras packs
// here, an oracle independent of the repository pack path. The manifest carries a
// created timestamp, so this value cannot be a hand-written literal.
func ociLayoutBlob(t *testing.T) ([]byte, string, *descriptor.Digest) {
	t.Helper()
	r := require.New(t)
	buf := bytes.NewBuffer(nil)
	w, err := tar.NewOCILayoutWriterWithTempFile(buf, t.TempDir())
	r.NoError(err)

	payload := []byte("oci layout content")
	desc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageLayer,
		Digest:    digest.FromBytes(payload),
		Size:      int64(len(payload)),
	}
	r.NoError(w.Push(t.Context(), desc, bytes.NewReader(payload)))
	manifest, err := oras.PackManifest(t.Context(), w, oras.PackManifestVersion1_1,
		ociImageSpecV1.MediaTypeImageManifest, oras.PackManifestOptions{
			Layers: []ociImageSpecV1.Descriptor{desc},
		})
	r.NoError(err)
	r.NoError(w.Tag(t.Context(), manifest, "test-tag"))
	r.NoError(w.Close())

	return buf.Bytes(), layout.MediaTypeOCIImageLayoutTarGzipV1, &descriptor.Digest{
		HashAlgorithm:          "SHA-256",
		NormalisationAlgorithm: "ociArtifactDigest/v1",
		Value:                  manifest.Digest.Encoded(),
	}
}
