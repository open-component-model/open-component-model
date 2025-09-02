package lister_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	slogcontext "github.com/veqryn/slog-context"
	"oras.land/oras-go/v2/content"

	. "ocm.software/open-component-model/bindings/go/oci/internal/lister"
)

// mockReferrerStore implements content.ReadOnlyStorage and registry.ReferrerLister
type mockReferrerStore struct {
	content.ReadOnlyStorage
	referrers []v1.Descriptor
	err       error
}

func (m *mockReferrerStore) Exists(ctx context.Context, target v1.Descriptor) (bool, error) {
	return true, nil
}

func (m *mockReferrerStore) Fetch(ctx context.Context, target v1.Descriptor) (io.ReadCloser, error) {
	return io.NopCloser(io.NopCloser(nil)), nil
}

func (m *mockReferrerStore) Referrers(ctx context.Context, subject v1.Descriptor, artifactType string, fn func(referrers []v1.Descriptor) error) error {
	if m.err != nil {
		return m.err
	}
	return fn(m.referrers)
}

// mockTagStore implements content.ReadOnlyStorage and registry.TagLister
type mockTagStore struct {
	content.ReadOnlyStorage
	tags []string
	err  error
}

func (m *mockTagStore) Exists(ctx context.Context, target v1.Descriptor) (bool, error) {
	return true, nil
}

func (m *mockTagStore) Fetch(ctx context.Context, target v1.Descriptor) (io.ReadCloser, error) {
	return io.NopCloser(io.NopCloser(nil)), nil
}

func (m *mockTagStore) Tags(ctx context.Context, last string, fn func(tags []string) error) error {
	if m.err != nil {
		return m.err
	}
	return fn(m.tags)
}

// mockBasicStore implements only content.ReadOnlyStorage
type mockBasicStore struct {
	content.ReadOnlyStorage
}

func (m *mockBasicStore) Exists(ctx context.Context, target v1.Descriptor) (bool, error) {
	return true, nil
}

func (m *mockBasicStore) Fetch(ctx context.Context, target v1.Descriptor) (io.ReadCloser, error) {
	return io.NopCloser(io.NopCloser(nil)), nil
}

func TestNew(t *testing.T) {
	t.Run("success with referrer lister", func(t *testing.T) {
		store := &mockReferrerStore{}
		lister, err := New(store)
		require.NoError(t, err)
		assert.NotNil(t, lister)
	})

	t.Run("success with tag lister", func(t *testing.T) {
		store := &mockTagStore{}
		lister, err := New(store)
		require.NoError(t, err)
		assert.NotNil(t, lister)
	})

	t.Run("error when no supported lister", func(t *testing.T) {
		store := &mockBasicStore{}
		lister, err := New(store)
		assert.Error(t, err)
		assert.Nil(t, lister)
	})
}

func TestList(t *testing.T) {
	subject := v1.Descriptor{
		Digest: "sha256:123",
	}

	t.Run("list via referrers", func(t *testing.T) {
		store := &mockReferrerStore{
			referrers: []v1.Descriptor{
				{Digest: "sha256:abc", Annotations: map[string]string{"version": "v1.0.0"}},
				{Digest: "sha256:def", Annotations: map[string]string{"version": "v2.0.0"}},
			},
		}

		lister, err := New(store)
		require.NoError(t, err)

		opts := Options{
			LookupPolicy: LookupPolicyReferrerWithTagFallback,
			SortPolicy:   SortPolicyLooseSemverDescending,
			ReferrerListerOptions: ReferrerListerOptions{
				Subject:      subject,
				ArtifactType: "test",
				VersionResolver: func(ctx context.Context, desc v1.Descriptor) (string, error) {
					if ver, ok := desc.Annotations["version"]; ok {
						return ver, nil
					}
					return "", ErrSkip
				},
			},
		}

		versions, err := lister.List(t.Context(), opts)
		require.NoError(t, err)
		assert.Equal(t, []string{"v2.0.0", "v1.0.0"}, versions)
	})

	t.Run("list via tags when referrers fail", func(t *testing.T) {
		store := &mockTagStore{
			tags: []string{"v1.0.0", "v2.0.0"},
		}

		lister, err := New(store)
		require.NoError(t, err)

		opts := Options{
			LookupPolicy: LookupPolicyReferrerWithTagFallback,
			SortPolicy:   SortPolicyLooseSemverDescending,
			TagListerOptions: TagListerOptions{
				VersionResolver: func(ctx context.Context, tag string) (string, error) {
					return tag, nil
				},
			},
		}

		versions, err := lister.List(t.Context(), opts)
		require.NoError(t, err)
		assert.Equal(t, []string{"v2.0.0", "v1.0.0"}, versions)
	})

	t.Run("skip versions", func(t *testing.T) {
		store := &mockReferrerStore{
			referrers: []v1.Descriptor{
				{Digest: "sha256:abc"},
				{Digest: "sha256:def"},
			},
		}

		lister, err := New(store)
		require.NoError(t, err)

		opts := Options{
			LookupPolicy: LookupPolicyReferrerWithTagFallback,
			SortPolicy:   SortPolicyLooseSemverDescending,
			ReferrerListerOptions: ReferrerListerOptions{
				Subject:      subject,
				ArtifactType: "test",
				VersionResolver: func(ctx context.Context, desc v1.Descriptor) (string, error) {
					if desc.Digest == "sha256:def" {
						return "", ErrSkip
					}
					return "v1.0.0", nil
				},
			},
		}

		versions, err := lister.List(t.Context(), opts)
		require.NoError(t, err)
		assert.Equal(t, []string{"v1.0.0"}, versions)
	})

	t.Run("error in version resolver", func(t *testing.T) {
		store := &mockReferrerStore{
			referrers: []v1.Descriptor{
				{Digest: "sha256:abc"},
			},
		}

		lister, err := New(store)
		require.NoError(t, err)

		opts := Options{
			LookupPolicy: LookupPolicyReferrerWithTagFallback,
			SortPolicy:   SortPolicyLooseSemverDescending,
			ReferrerListerOptions: ReferrerListerOptions{
				Subject:      subject,
				ArtifactType: "test",
				VersionResolver: func(ctx context.Context, desc v1.Descriptor) (string, error) {
					return "", errors.New("version resolver error")
				},
			},
		}

		versions, err := lister.List(t.Context(), opts)
		assert.Error(t, err)
		assert.Nil(t, versions)
	})

	t.Run("unsupported lookup policy", func(t *testing.T) {
		store := &mockReferrerStore{}

		lister, err := New(store)
		require.NoError(t, err)

		opts := Options{
			LookupPolicy: LookupPolicy(999), // Invalid policy
			SortPolicy:   SortPolicyLooseSemverDescending,
		}

		versions, err := lister.List(t.Context(), opts)
		assert.Error(t, err)
		assert.Nil(t, versions)
	})

	t.Run("unsupported sort policy", func(t *testing.T) {
		store := &mockReferrerStore{
			referrers: []v1.Descriptor{
				{Digest: "sha256:abc"},
			},
		}

		lister, err := New(store)
		require.NoError(t, err)

		opts := Options{
			LookupPolicy: LookupPolicyReferrerWithTagFallback,
			SortPolicy:   SortPolicy(999), // Invalid policy
			ReferrerListerOptions: ReferrerListerOptions{
				Subject:      subject,
				ArtifactType: "test",
				VersionResolver: func(ctx context.Context, desc v1.Descriptor) (string, error) {
					return "v1.0.0", nil
				},
			},
		}

		versions, err := lister.List(t.Context(), opts)
		assert.Error(t, err)
		assert.Nil(t, versions)
	})

	t.Run("logs when tags are skipped", func(t *testing.T) {
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		ctx := slogcontext.NewCtx(context.Background(), logger)

		store := &mockTagStore{
			tags: []string{"v1.0.0", "invalid-tag", "v2.0.0"},
		}

		lister, err := New(store)
		require.NoError(t, err)

		opts := Options{
			LookupPolicy: LookupPolicyTagOnly,
			SortPolicy:   SortPolicyLooseSemverDescending,
			TagListerOptions: TagListerOptions{
				VersionResolver: func(ctx context.Context, tag string) (string, error) {
					if tag == "invalid-tag" {
						return "", ErrSkip
					}
					return tag, nil
				},
			},
		}

		versions, err := lister.List(ctx, opts)
		require.NoError(t, err)
		assert.Equal(t, []string{"v2.0.0", "v1.0.0"}, versions)

		logOutput := logBuf.String()
		assert.Contains(t, logOutput, "skipping tag")
		assert.Contains(t, logOutput, "invalid-tag")
		assert.Contains(t, logOutput, "realm")
		assert.Contains(t, logOutput, "oci")
	})
}
