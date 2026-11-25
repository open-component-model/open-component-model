package internal

import (
	"context"
	"crypto"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

const (
	pinComponent = "ocm.software/pinned"
	pinVersion   = "1.0.0"
	pinKey       = pinComponent + ":" + pinVersion
)

func newCTFStore(t *testing.T) *ocictf.Store {
	t.Helper()
	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	require.NoError(t, err)
	return ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
}

func newCTFRepository(t *testing.T, store *ocictf.Store, l oci.Layout) *oci.Repository {
	t.Helper()
	repo, err := oci.NewRepository(ocictf.WithCTF(store), oci.WithTempDir(t.TempDir()), oci.WithComponentVersionLayout(l))
	require.NoError(t, err)
	return repo
}

func pinDescriptor(provider string) *descriptor.Descriptor {
	desc := testDescriptor(pinComponent, pinVersion, nil, nil)
	desc.Component.Provider.Name = provider
	return desc
}

// resolveFrom resolves the root [pinKey] from repo, pinned to pin if it is not nil.
func resolveFrom(t *testing.T, repo repository.ComponentVersionRepository, pin *descriptor.Digest) (*discoveryValue, error) {
	t.Helper()
	resolver := &mockCVRepoResolver{
		specs:      map[string]runtime.Typed{pinKey: testCTFRepo("/tmp/source")},
		sharedRepo: repo,
	}
	r := &multiResolver{
		mu:              &sync.Mutex{},
		resolverMap:     map[string]resolvers.ComponentVersionRepositoryResolver{pinKey: resolver},
		expectedDigests: func(runtime.Identity) []descriptor.Digest { return nil },
		rootPin:         func(string) *descriptor.Digest { return pin },
	}
	return r.Resolve(t.Context(), pinKey)
}

func TestResolver_PinsNormalizedSource(t *testing.T) {
	// addVersion stores a version of [pinComponent] and returns the digest its tag then resolves to.
	addVersion := func(t *testing.T, repo *oci.Repository, desc *descriptor.Descriptor) string {
		t.Helper()
		require.NoError(t, repo.AddComponentVersion(t.Context(), desc))
		dgst, err := repo.ResolveComponentVersionDigest(t.Context(), pinComponent, pinVersion)
		require.NoError(t, err)
		return dgst
	}

	t.Run("normalized source without pin is pinned to the artifact its tag resolves to", func(t *testing.T) {
		r := require.New(t)
		repo := newCTFRepository(t, newCTFStore(t), oci.LayoutNormalized)
		tagged := addVersion(t, repo, pinDescriptor("acme"))

		val, err := resolveFrom(t, repo, nil)
		r.NoError(err)
		r.Equal(layout.ComponentVersionLayoutNormalized, val.SourceLayout)
		r.Equal(tagged, val.Pin)
	})

	t.Run("normalized source keeps the pin of the user", func(t *testing.T) {
		r := require.New(t)
		repo := newCTFRepository(t, newCTFStore(t), oci.LayoutNormalized)
		pinned := addVersion(t, repo, pinDescriptor("acme"))
		// The tag moves to another normalized artifact, so the tag digest differs from the pin.
		moved := addVersion(t, repo, pinDescriptor("other"))
		r.NotEqual(pinned, moved)
		pin, err := repository.ParseDigestPin(pinned)
		r.NoError(err)

		val, err := resolveFrom(t, repo, pin)
		r.NoError(err)
		r.Equal(layout.ComponentVersionLayoutNormalized, val.SourceLayout)
		r.Equal(pinned, val.Pin)
		r.Equal("acme", val.Descriptor.Component.Provider.Name, "the descriptor must be read from the pinned artifact")
	})

	t.Run("normalized source pinned to a normalized digest is pinned to the artifact its tag resolves to", func(t *testing.T) {
		r := require.New(t)
		repo := newCTFRepository(t, newCTFStore(t), oci.LayoutNormalized)
		desc := pinDescriptor("acme")
		tagged := addVersion(t, repo, desc)
		pin, err := signing.GenerateDigest(t.Context(), desc, slog.Default(), signing.DefaultNormalisationAlgorithm, crypto.SHA256.String())
		r.NoError(err)

		val, err := resolveFrom(t, repo, pin)
		r.NoError(err)
		r.Equal(tagged, val.Pin, "a normalized digest names no artifact, so the tag digest is the pin")
	})

	t.Run("default layout source without pin stays unpinned", func(t *testing.T) {
		r := require.New(t)
		repo := newCTFRepository(t, newCTFStore(t), oci.LayoutV2)
		addVersion(t, repo, pinDescriptor("acme"))

		val, err := resolveFrom(t, repo, nil)
		r.NoError(err)
		r.Equal(layout.ComponentVersionLayoutDefault, val.SourceLayout)
		r.Empty(val.Pin)
	})

	t.Run("normalized source that cannot resolve digests stays unpinned", func(t *testing.T) {
		desc := pinDescriptor("acme")
		descriptors := map[string]*descriptor.Descriptor{pinKey: desc}
		for name, repo := range map[string]repository.ComponentVersionRepository{
			"not digest pinnable": &layoutCVRepo{mockCVRepo: mockCVRepo{descriptors: descriptors}, layout: layout.ComponentVersionLayoutNormalized},
			"unsupported by the wrapped repository": &tagLayoutRepo{
				mockCVRepo: mockCVRepo{descriptors: descriptors},
				tagLayout:  layout.ComponentVersionLayoutNormalized,
				resolveErr: fmt.Errorf("wrapped repository: %w", errors.ErrUnsupported),
			},
		} {
			t.Run(name, func(t *testing.T) {
				r := require.New(t)
				val, err := resolveFrom(t, repo, nil)
				r.NoError(err)
				r.Equal(layout.ComponentVersionLayoutNormalized, val.SourceLayout)
				r.Empty(val.Pin)
			})
		}
	})

	t.Run("failing to resolve the tag digest of a normalized source fails", func(t *testing.T) {
		r := require.New(t)
		repo := &tagLayoutRepo{
			mockCVRepo: mockCVRepo{descriptors: map[string]*descriptor.Descriptor{pinKey: pinDescriptor("acme")}},
			tagLayout:  layout.ComponentVersionLayoutNormalized,
			resolveErr: errors.New("registry unavailable"),
		}
		_, err := resolveFrom(t, repo, nil)
		r.ErrorContains(err, "registry unavailable")
	})
}

// tagLayoutRepo is a digest pinnable repository whose version tag and pinned artifact differ:
// the unpinned repository reports tagLayout (or tagLayoutErr) for the tag, the view
// PinComponentVersion returns reports pinnedLayout for the pinned artifact.
type tagLayoutRepo struct {
	mockCVRepo
	tagLayout    layout.ComponentVersionLayout
	tagLayoutErr error
	tagDigest    string
	resolveErr   error
	pinnedLayout layout.ComponentVersionLayout
}

var (
	_ repository.DigestPinnableRepository       = (*tagLayoutRepo)(nil)
	_ repository.ComponentVersionLayoutDetector = (*tagLayoutRepo)(nil)
)

func (m *tagLayoutRepo) GetComponentVersionLayout(context.Context, string, string) (string, error) {
	return string(m.tagLayout), m.tagLayoutErr
}

func (m *tagLayoutRepo) PinComponentVersion(string, string, string) (repository.ComponentVersionRepository, error) {
	return &layoutCVRepo{mockCVRepo: m.mockCVRepo, layout: m.pinnedLayout}, nil
}

func (m *tagLayoutRepo) ResolveComponentVersionDigest(context.Context, string, string) (string, error) {
	return m.tagDigest, m.resolveErr
}

// The pin of a normalized source also binds the reads of its local blobs to the artifact the
// descriptor was read from.
func TestBuildGraphDefinition_NormalizedSourcePinsLocalBlobReads(t *testing.T) {
	const tagged = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	r := require.New(t)
	desc := testDescriptor(pinComponent, pinVersion, []descriptor.Resource{localBlobResource("blob", "1.0.0")}, nil)
	repo := &tagLayoutRepo{
		mockCVRepo: mockCVRepo{descriptors: map[string]*descriptor.Descriptor{pinKey: desc}},
		tagLayout:  layout.ComponentVersionLayoutNormalized,
		tagDigest:  tagged,
	}
	resolver := &mockCVRepoResolver{
		specs:      map[string]runtime.Typed{pinKey: testCTFRepo("/tmp/source")},
		sharedRepo: repo,
	}
	roots := testTransferRoots(pinComponent, pinVersion, testOCIRepo("ghcr.io/target"), resolver)

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, nil)
	r.NoError(err)

	transfers := findTransformations(tgd, ociv1alpha1.TransferCTFLocalBlobToOCILocalBlobV1alpha1)
	r.Len(transfers, 1)
	r.Equal(tagged, transfers[0].Spec.Data["source"].(map[string]any)["digest"])

	_, err = NewDefaultBuilder(nil, nil, nil, nil, "").BuildAndCheck(tgd)
	r.NoError(err)
}

// A root pinned by OCI digest names an artifact its version tag need not point at (anymore),
// so the layout has to come from the repository view pinned to that artifact.
func TestResolver_DetectsLayoutOnPinnedRepository(t *testing.T) {
	const userPin = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	tests := []struct {
		name string
		repo *tagLayoutRepo
	}{
		{
			name: "version tag points at an artifact in another layout",
			repo: &tagLayoutRepo{
				tagLayout:    layout.ComponentVersionLayoutDefault,
				tagDigest:    "sha256:1111111111111111111111111111111111111111111111111111111111111111",
				pinnedLayout: layout.ComponentVersionLayoutNormalized,
			},
		},
		{
			name: "version tag does not exist",
			repo: &tagLayoutRepo{
				tagLayoutErr: repository.ErrNotFound,
				resolveErr:   repository.ErrNotFound,
				pinnedLayout: layout.ComponentVersionLayoutNormalized,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			tt.repo.descriptors = map[string]*descriptor.Descriptor{pinKey: pinDescriptor("acme")}
			pin, err := repository.ParseDigestPin(userPin)
			r.NoError(err)

			val, err := resolveFrom(t, tt.repo, pin)
			r.NoError(err)
			r.Equal(layout.ComponentVersionLayoutNormalized, val.SourceLayout)
			r.Equal(userPin, val.Pin)
		})
	}
}
