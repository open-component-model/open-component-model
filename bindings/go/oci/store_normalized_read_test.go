package oci_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/oras-project/oras-go/v3/content"
	"github.com/oras-project/oras-go/v3/content/memory"
	"github.com/oras-project/oras-go/v3/registry"
	"github.com/stretchr/testify/require"
	slogcontext "github.com/veqryn/slog-context"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	ocidescriptor "ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// pushAccessReferrer pushes an access referrer of subject carrying desc, stamped with created. An
// empty created leaves it unstamped.
func pushAccessReferrer(t *testing.T, ctx context.Context, store spec.Store, subject ociImageSpecV1.Descriptor, desc *descriptor.Descriptor, created string) ociImageSpecV1.Descriptor {
	t.Helper()
	return pushAccessManifestWith(t, ctx, store, subject, desc, nil, func(m *ociImageSpecV1.Manifest) {
		if created != "" {
			m.Annotations[ociImageSpecV1.AnnotationCreated] = created
		}
	})
}

// fetchRecorder records the digests fetched from a store. It forwards the referrers listing of the
// store, which embedding the interface alone would hide.
type fetchRecorder struct {
	spec.Store
	fetched []digest.Digest
}

func (s *fetchRecorder) Fetch(ctx context.Context, target ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	s.fetched = append(s.fetched, target.Digest)
	return s.Store.Fetch(ctx, target)
}

func (s *fetchRecorder) Referrers(ctx context.Context, desc ociImageSpecV1.Descriptor, artifactType string, fn func([]ociImageSpecV1.Descriptor) error) error {
	return s.Store.(registry.ReferrerLister).Referrers(ctx, desc, artifactType, fn)
}

// cancelOnFetch cancels the read once target is fetched.
type cancelOnFetch struct {
	*fetchRecorder
	target digest.Digest
	cancel context.CancelFunc
}

func (s *cancelOnFetch) Fetch(ctx context.Context, target ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	if target.Digest == s.target {
		s.cancel()
		return nil, ctx.Err()
	}
	return s.fetchRecorder.Fetch(ctx, target)
}

// captureWarnings returns a context logging to the returned function, which yields the warnings
// logged so far.
func captureWarnings(t *testing.T) (context.Context, func() []string) {
	t.Helper()
	var buf bytes.Buffer
	ctx := slogcontext.NewCtx(t.Context(), slog.New(slog.NewJSONHandler(&buf, nil)))
	return ctx, func() []string {
		var warnings []string
		for line := range strings.Lines(buf.String()) {
			var record struct {
				Level string `json:"level"`
			}
			require.NoError(t, json.Unmarshal([]byte(line), &record))
			if record.Level == slog.LevelWarn.String() {
				warnings = append(warnings, line)
			}
		}
		return warnings
	}
}

func TestNormalizedLayout_AccessReferrerSelection(t *testing.T) {
	const (
		longAgo   = "2000-01-01T00:00:00Z"
		farFuture = "9999-01-01T00:00:00Z"
	)
	tampered := normDescriptor()
	tampered.Component.Provider.Name = "attacker"
	signed := func(name string) *descriptor.Descriptor { return withSignature(normDescriptor(), name) }
	in := func(d time.Duration) string { return time.Now().Add(d).UTC().Format(time.RFC3339Nano) }
	// selected names the access referrer a read returned by the signature its descriptor carries.
	// The one AddDescriptorToStore writes, stamped with the current time, is unsigned.
	selected := func(got *descriptor.Descriptor) string {
		if len(got.Signatures) == 0 {
			return ""
		}
		return got.Signatures[0].Name
	}
	add := func(t *testing.T, store spec.Store) ociImageSpecV1.Descriptor {
		t.Helper()
		m, err := oci.AddDescriptorToStore(t.Context(), store, normDescriptor(), oci.AddDescriptorOptions{Scheme: testScheme, Layout: oci.LayoutNormalized})
		require.NoError(t, err)
		return *m
	}
	// normalizedOnly returns a store holding the normalized manifest without any access referrer.
	normalizedOnly := func(t *testing.T) (spec.Store, ociImageSpecV1.Descriptor) {
		t.Helper()
		ctx := t.Context()
		src, dst := memory.New(), memory.New()
		m := add(t, src)
		raw, err := content.FetchAll(ctx, src, m)
		require.NoError(t, err)
		var manifest ociImageSpecV1.Manifest
		require.NoError(t, json.Unmarshal(raw, &manifest))
		for _, blob := range append([]ociImageSpecV1.Descriptor{manifest.Config}, manifest.Layers...) {
			data, err := content.FetchAll(ctx, src, blob)
			require.NoError(t, err)
			pushIfAbsent(t, ctx, dst, blob, data)
		}
		pushIfAbsent(t, ctx, dst, m, raw)
		return dst, m
	}
	ctfStore := func(t *testing.T) spec.Store {
		t.Helper()
		store, _ := componentStore(t, t.Context(), newCTFStore(t), normComponent, normVersion)
		return store
	}

	type referrer struct {
		desc    *descriptor.Descriptor
		created string
	}
	for storeName, newStore := range map[string]func(*testing.T) spec.Store{
		"referrers read from predecessors": func(*testing.T) spec.Store { return memory.New() },
		"referrers listed by the store":    ctfStore,
	} {
		for _, tc := range []struct {
			name string
			// referrers are pushed next to the unsigned one AddDescriptorToStore wrote.
			referrers []referrer
			want      string
			// skipped indexes the referrers a read must skip with a warning.
			skipped []int
		}{
			{
				name:      "the newest by created wins",
				referrers: []referrer{{signed("old"), longAgo}, {signed("new"), in(2 * time.Hour)}, {signed("mid"), in(time.Hour)}},
				want:      "new",
			},
			{
				name:      "a missing or malformed created sorts oldest",
				referrers: []referrer{{signed("unstamped"), ""}, {signed("malformed"), "yesterday"}},
				want:      "",
			},
			{
				name:      "an invalid newest one is skipped for the next newest",
				referrers: []referrer{{tampered, in(2 * time.Hour)}, {signed("next"), in(time.Hour)}},
				want:      "next",
				skipped:   []int{0},
			},
			{
				name:      "an invalid one created in the far future is skipped",
				referrers: []referrer{{tampered, farFuture}},
				want:      "",
				skipped:   []int{0},
			},
		} {
			t.Run(storeName+"/"+tc.name, func(t *testing.T) {
				r := require.New(t)
				store := newStore(t)
				m := add(t, store)
				var pushed []ociImageSpecV1.Descriptor
				for _, ref := range tc.referrers {
					pushed = append(pushed, pushAccessReferrer(t, t.Context(), store, m, ref.desc, ref.created))
				}

				ctx, warnings := captureWarnings(t)
				got, err := oci.GetNormalizedComponentVersion(ctx, store, m, ocidescriptor.DefaultDescriptorUnmarshalFunc)
				r.NoError(err)
				r.Equal("internal", got.Component.Provider.Name, "a bind-check failure must never win over a valid referrer")
				r.Equal(tc.want, selected(got))

				logged := warnings()
				r.Len(logged, len(tc.skipped))
				for i, skipped := range tc.skipped {
					r.Contains(logged[i], pushed[skipped].Digest.String())
					r.Contains(logged[i], "bind check failed")
				}
			})
		}
	}

	t.Run("equal created breaks the tie by the lowest digest", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()
		store := memory.New()
		m := add(t, store)
		created := in(time.Hour)
		a := pushAccessReferrer(t, ctx, store, m, signed("a"), created)
		b := pushAccessReferrer(t, ctx, store, m, signed("b"), created)
		want := "a"
		if b.Digest < a.Digest {
			want = "b"
		}

		got, err := oci.GetNormalizedComponentVersion(ctx, store, m, ocidescriptor.DefaultDescriptorUnmarshalFunc)
		r.NoError(err)
		r.Equal(want, selected(got))
	})

	t.Run("no valid access referrer fails naming each one", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()
		store, m := normalizedOnly(t)
		first := pushAccessReferrer(t, ctx, store, m, tampered, in(time.Hour))
		second := pushAccessReferrer(t, ctx, store, m, tampered, in(2*time.Hour))

		_, err := oci.GetNormalizedComponentVersion(ctx, store, m, ocidescriptor.DefaultDescriptorUnmarshalFunc)
		r.ErrorContains(err, "no valid access referrer found")
		r.ErrorContains(err, first.Digest.String())
		r.ErrorContains(err, second.Digest.String())
	})

	t.Run("no access referrer fails", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()
		store, m := normalizedOnly(t)
		pushReferrerManifest(t, ctx, store, m, "application/vnd.dev.cosign.artifact.sig.v1+json")

		_, err := oci.GetNormalizedComponentVersion(ctx, store, m, ocidescriptor.DefaultDescriptorUnmarshalFunc)
		r.ErrorContains(err, "no access referrer found")
	})

	t.Run("only the newest access referrer is fetched", func(t *testing.T) {
		r := require.New(t)
		ctx := t.Context()
		store := &fetchRecorder{Store: ctfStore(t)}
		m := add(t, store.Store)
		notFetched := []ociImageSpecV1.Descriptor{
			accessReferrerOf(t, ctx, store.Store, m),
			pushAccessReferrer(t, ctx, store.Store, m, signed("superseded"), longAgo),
			pushReferrerManifest(t, ctx, store.Store, m, "application/vnd.dev.cosign.artifact.sig.v1+json"),
		}
		newest := pushAccessReferrer(t, ctx, store.Store, m, signed("newest"), in(time.Hour))

		got, err := oci.GetNormalizedComponentVersion(ctx, store, m, ocidescriptor.DefaultDescriptorUnmarshalFunc)
		r.NoError(err)
		r.Equal("newest", selected(got))
		r.Contains(store.fetched, newest.Digest)
		for _, desc := range notFetched {
			r.NotContains(store.fetched, desc.Digest)
		}
		r.Len(store.fetched, 4, "normalized manifest and its layer, access manifest and its descriptor layer")
	})

	t.Run("a cancelled read does not fall back to an older access referrer", func(t *testing.T) {
		r := require.New(t)
		recorder := &fetchRecorder{Store: ctfStore(t)}
		m := add(t, recorder.Store)
		older := accessReferrerOf(t, t.Context(), recorder.Store, m)
		newest := pushAccessReferrer(t, t.Context(), recorder.Store, m, signed("newest"), in(time.Hour))

		logCtx, warnings := captureWarnings(t)
		ctx, cancel := context.WithCancel(logCtx)
		defer cancel()
		store := &cancelOnFetch{fetchRecorder: recorder, target: newest.Digest, cancel: cancel}
		_, err := oci.GetNormalizedComponentVersion(ctx, store, m, ocidescriptor.DefaultDescriptorUnmarshalFunc)
		r.ErrorIs(err, context.Canceled)
		r.NotContains(recorder.fetched, older.Digest)
		r.Empty(warnings())
	})

	t.Run("a listed referrer without artifactType is ignored", func(t *testing.T) {
		r := require.New(t)
		store := &fetchRecorder{Store: ctfStore(t)}
		m := add(t, store.Store)
		// The referrers listing omits artifactType for an image index that does not set it.
		subject := ociImageSpecV1.Descriptor{MediaType: m.MediaType, Digest: m.Digest, Size: m.Size}
		index := ociImageSpecV1.Index{
			MediaType:   ociImageSpecV1.MediaTypeImageIndex,
			Manifests:   []ociImageSpecV1.Descriptor{},
			Subject:     &subject,
			Annotations: map[string]string{ociImageSpecV1.AnnotationCreated: farFuture},
		}
		index.SchemaVersion = 2
		raw, err := json.Marshal(index, runtime.JSONOptions)
		r.NoError(err)
		untyped := content.NewDescriptorFromBytes(index.MediaType, raw)
		pushIfAbsent(t, t.Context(), store.Store, untyped, raw)

		ctx, warnings := captureWarnings(t)
		got, err := oci.GetNormalizedComponentVersion(ctx, store, m, ocidescriptor.DefaultDescriptorUnmarshalFunc)
		r.NoError(err)
		r.Empty(selected(got), "the access referrer AddDescriptorToStore wrote must be selected")
		r.NotContains(store.fetched, untyped.Digest)
		r.Empty(warnings())
	})
}
