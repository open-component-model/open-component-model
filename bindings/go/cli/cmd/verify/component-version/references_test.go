package componentversion

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
)

func newDesc(name, version string, refs ...descruntime.Reference) *descruntime.Descriptor {
	return &descruntime.Descriptor{Component: descruntime.Component{
		ComponentMeta: descruntime.ComponentMeta{ObjectMeta: descruntime.ObjectMeta{Name: name, Version: version}},
		Provider:      descruntime.Provider{Name: "acme"},
		References:    refs,
	}}
}

func refTo(t *testing.T, child *descruntime.Descriptor) descruntime.Reference {
	t.Helper()
	dg, err := signing.GenerateDigest(t.Context(), child, slog.Default(), signing.DefaultNormalisationAlgorithm, "SHA-256")
	require.NoError(t, err)
	return descruntime.Reference{
		ElementMeta: descruntime.ElementMeta{ObjectMeta: descruntime.ObjectMeta{Name: "r-" + child.Component.Name, Version: child.Component.Version}},
		Component:   child.Component.Name,
		Digest:      *dg,
	}
}

func mapResolver(descs ...*descruntime.Descriptor) descriptorResolver {
	return func(_ context.Context, c, v string) (*descruntime.Descriptor, error) {
		for _, d := range descs {
			if d.Component.Name == c && d.Component.Version == v {
				return d, nil
			}
		}
		return nil, errors.New("not found")
	}
}

func TestVerifyReferences(t *testing.T) {
	logger := slog.Default()
	leaf := newDesc("acme.org/leaf", "1.0.0")
	base := newDesc("acme.org/base", "1.0.0", refTo(t, leaf))
	app := newDesc("acme.org/app", "1.0.0", refTo(t, base))

	t.Run("all present", func(t *testing.T) {
		require.NoError(t, verifyReferences(t.Context(), logger, app, mapResolver(base, leaf)))
	})
	t.Run("no references", func(t *testing.T) {
		require.NoError(t, verifyReferences(t.Context(), logger, leaf, mapResolver()))
	})
	t.Run("missing reference", func(t *testing.T) {
		err := verifyReferences(t.Context(), logger, app, mapResolver())
		require.ErrorContains(t, err, "failed resolving component reference")
	})
	t.Run("missing transitive reference", func(t *testing.T) {
		err := verifyReferences(t.Context(), logger, app, mapResolver(base))
		require.ErrorContains(t, err, "acme.org/leaf")
	})
	t.Run("changed reference", func(t *testing.T) {
		changed := newDesc("acme.org/base", "1.0.0", refTo(t, leaf))
		changed.Component.Provider.Name = "evil"
		err := verifyReferences(t.Context(), logger, app, mapResolver(changed, leaf))
		require.ErrorContains(t, err, "does not match the digest")
	})
	t.Run("missing digest", func(t *testing.T) {
		noDigest := newDesc("acme.org/app", "1.0.0", descruntime.Reference{
			ElementMeta: descruntime.ElementMeta{ObjectMeta: descruntime.ObjectMeta{Name: "b", Version: "1.0.0"}},
			Component:   "acme.org/base",
		})
		err := verifyReferences(t.Context(), logger, noDigest, mapResolver(base))
		require.ErrorContains(t, err, "carries no digest")
	})
	t.Run("unsupported hash algorithm", func(t *testing.T) {
		ref := refTo(t, base)
		ref.Digest.HashAlgorithm = "SHA-1"
		weak := newDesc("acme.org/app", "1.0.0", ref)
		err := verifyReferences(t.Context(), logger, weak, mapResolver(base, leaf))
		require.ErrorContains(t, err, "cannot be checked against the digest")
		require.NotContains(t, err.Error(), "does not match")
	})
	t.Run("diamond walks shared subtree once", func(t *testing.T) {
		left := newDesc("acme.org/left", "1.0.0", refTo(t, base))
		right := newDesc("acme.org/right", "1.0.0", refTo(t, base))
		top := newDesc("acme.org/top", "1.0.0", refTo(t, left), refTo(t, right))
		calls := map[string]int{}
		inner := mapResolver(left, right, base, leaf)
		resolve := func(ctx context.Context, c, v string) (*descruntime.Descriptor, error) {
			calls[c]++
			return inner(ctx, c, v)
		}
		require.NoError(t, verifyReferences(t.Context(), logger, top, resolve))
		require.Equal(t, 2, calls["acme.org/base"], "shared reference is checked against each parent")
		require.Equal(t, 1, calls["acme.org/leaf"], "subtree of the shared reference is walked once")
	})
	t.Run("cycle terminates", func(t *testing.T) {
		a := newDesc("acme.org/a", "1.0.0")
		b := newDesc("acme.org/b", "1.0.0", refTo(t, a))
		// a points back to b; the digest of b was computed before, so it mismatches
		// once a carries the reference, which stops the walk.
		a.Component.References = []descruntime.Reference{refTo(t, b)}
		err := verifyReferences(t.Context(), logger, a, mapResolver(a, b))
		require.ErrorContains(t, err, "does not match the digest")
	})
}
