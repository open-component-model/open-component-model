package discovery

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestCompileSelectors(t *testing.T) {
	r := require.New(t)
	q, err := Compile(t.Context(), &v1alpha1.DiscoverySpec{
		ReferenceSelector: &v1alpha1.Selector{
			MatchIdentity: map[string]string{"componentName": "ocm.software/x"},
			MatchLabels:   map[string]string{"tier": "platform"},
			Expression:    `semverCheck(identity.version, ">=1.0.0")`,
		},
		ComponentSelector: &v1alpha1.Selector{Expression: `has(labels.scope)`},
		ResourceSelector:  &v1alpha1.Selector{MatchIdentity: map[string]string{"name": "image"}},
	})
	r.NoError(err)
	r.NotNil(q.references)
	r.NotNil(q.components)
	r.NotNil(q.resources)
	r.Nil(q.extract)
}

func TestCompileEmptySelectorsCompileToNil(t *testing.T) {
	r := require.New(t)
	q, err := Compile(t.Context(), &v1alpha1.DiscoverySpec{
		ReferenceSelector: &v1alpha1.Selector{},
		ComponentSelector: nil,
	})
	r.NoError(err)
	r.Nil(q.references)
	r.Nil(q.components)
	r.Nil(q.resources)
}

func TestCompileInvalidSelectorExpressions(t *testing.T) {
	for name, mutate := range map[string]func(*v1alpha1.DiscoverySpec){
		StageReference: func(s *v1alpha1.DiscoverySpec) { s.ReferenceSelector = &v1alpha1.Selector{Expression: "!!!"} },
		StageComponent: func(s *v1alpha1.DiscoverySpec) { s.ComponentSelector = &v1alpha1.Selector{Expression: "!!!"} },
		StageResource:  func(s *v1alpha1.DiscoverySpec) { s.ResourceSelector = &v1alpha1.Selector{Expression: "!!!"} },
	} {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			var spec v1alpha1.DiscoverySpec
			mutate(&spec)
			_, err := Compile(t.Context(), &spec)
			r.Error(err)
			var selErr *SelectorError
			r.ErrorAs(err, &selErr)
			r.Equal(name, selErr.Stage)
		})
	}
}

// TestCompileBindingIsolation proves expressions only see their mode's bindings.
func TestCompileBindingIsolation(t *testing.T) {
	r := require.New(t)

	_, err := Compile(t.Context(), &v1alpha1.DiscoverySpec{
		ResourceSelector: &v1alpha1.Selector{Expression: `resource.name == "x"`},
	})
	r.Error(err, "selectors must not see extraction bindings")
	r.Contains(err.Error(), "undeclared reference to 'resource'")

	_, err = Compile(t.Context(), specWithExtract(&v1alpha1.Extract{
		ByComponents: map[string]string{"bad": `identity.name`},
	}))
	r.Error(err, "byComponents must not see selector bindings")
	r.Contains(err.Error(), "undeclared reference to 'identity'")

	_, err = Compile(t.Context(), specWithExtract(&v1alpha1.Extract{
		ByComponents: map[string]string{"bad": `resource.name`},
	}))
	r.Error(err, "byComponents must not see the resource binding")
	r.Contains(err.Error(), "undeclared reference to 'resource'")

	_, err = Compile(t.Context(), specWithExtract(&v1alpha1.Extract{
		ByResources: map[string]string{"bad": `components[0]`},
	}))
	r.Error(err, "byResources must not see the components binding")
	r.Contains(err.Error(), "undeclared reference")

	_, err = Compile(t.Context(), specWithExtract(&v1alpha1.Extract{
		Expression: `component.name`,
	}))
	r.Error(err, "expression mode must not see the component binding")
	r.Contains(err.Error(), "undeclared reference to 'component'")
}

func TestCompileExtractRequiresExactlyOneMode(t *testing.T) {
	r := require.New(t)

	_, err := Compile(t.Context(), specWithExtract(&v1alpha1.Extract{}))
	r.Error(err)
	var extErr *ExtractError
	r.ErrorAs(err, &extErr)

	_, err = Compile(t.Context(), specWithExtract(&v1alpha1.Extract{
		ByResources:  map[string]string{},
		ByComponents: map[string]string{},
	}))
	r.Error(err)
	r.ErrorAs(err, &extErr)

	_, err = Compile(t.Context(), specWithExtract(&v1alpha1.Extract{
		ByComponents: map[string]string{},
		Expression:   `components`,
	}))
	r.Error(err)
	r.ErrorAs(err, &extErr)
}

// TestCompileExtractErrorsAreDeterministic ensures the lexicographically first
// invalid field is reported regardless of map iteration order.
func TestCompileExtractErrorsAreDeterministic(t *testing.T) {
	r := require.New(t)
	for range 20 {
		_, err := Compile(t.Context(), specWithExtract(&v1alpha1.Extract{
			ByResources: map[string]string{"zeta": "!!!", "alpha": "???"},
		}))
		r.Error(err)
		var extErr *ExtractError
		r.ErrorAs(err, &extErr)
		r.Equal("alpha", extErr.Field)
	}
}

func TestCompileNilSpec(t *testing.T) {
	r := require.New(t)
	_, err := Compile(t.Context(), nil)
	r.Error(err)
}

func mustCompileSelector(t *testing.T, sel *v1alpha1.Selector) *compiledSelector {
	t.Helper()
	q, err := Compile(t.Context(), &v1alpha1.DiscoverySpec{ResourceSelector: sel})
	require.NoError(t, err)
	return q.resources
}

func TestSelectorMatchesClausesAreANDed(t *testing.T) {
	r := require.New(t)
	identity := runtime.Identity{"name": "image", "version": "1.0.0"}
	labels := map[string]any{"tier": "platform"}

	q := mustCompileSelector(t, &v1alpha1.Selector{
		MatchIdentity: map[string]string{"name": "image"},
		MatchLabels:   map[string]string{"tier": "platform"},
		Expression:    `identity.version == "1.0.0"`,
	})
	for _, tc := range []struct {
		name     string
		identity runtime.Identity
		labels   map[string]any
		want     bool
	}{
		{"all match", identity, labels, true},
		{"identity mismatch", runtime.Identity{"name": "other", "version": "1.0.0"}, labels, false},
		{"label mismatch", identity, map[string]any{"tier": "edge"}, false},
		{"missing label", identity, map[string]any{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			match, err := q.matches(t.Context(), tc.identity, tc.labels)
			r.NoError(err)
			r.Equal(tc.want, match)
		})
	}
}

func TestSelectorMatchIdentityRequiresKeyPresence(t *testing.T) {
	r := require.New(t)
	q := mustCompileSelector(t, &v1alpha1.Selector{MatchIdentity: map[string]string{"build": ""}})

	match, err := q.matches(t.Context(), runtime.Identity{"name": "x", "build": ""}, nil)
	r.NoError(err)
	r.True(match, "present key must match empty value")

	match, err = q.matches(t.Context(), runtime.Identity{"name": "x"}, nil)
	r.NoError(err)
	r.False(match, "absent key must not match empty value")
}

func TestSelectorMatchLabelsStringOnly(t *testing.T) {
	r := require.New(t)
	q := mustCompileSelector(t, &v1alpha1.Selector{MatchLabels: map[string]string{"feature": "on"}})

	match, err := q.matches(t.Context(), runtime.Identity{"name": "x"}, map[string]any{"feature": "on"})
	r.NoError(err)
	r.True(match)

	match, err = q.matches(t.Context(), runtime.Identity{"name": "x"}, map[string]any{"feature": map[string]any{"enabled": true}})
	r.NoError(err)
	r.False(match, "structured label values must not match matchLabels")
}

func TestSelectorMissingAccessIsNonmatch(t *testing.T) {
	r := require.New(t)
	q := mustCompileSelector(t, &v1alpha1.Selector{Expression: `labels.tier == "platform"`})

	match, err := q.matches(t.Context(), runtime.Identity{"name": "x"}, map[string]any{})
	r.NoError(err)
	r.False(match, "missing label access must be a nonmatch, not an error")
}

func TestSelectorHasAndOptionalSemantics(t *testing.T) {
	r := require.New(t)
	q := mustCompileSelector(t, &v1alpha1.Selector{
		Expression: `has(labels.tier) && labels.tier == "platform" || labels.?kind.orValue("none") == "os"`,
	})

	match, err := q.matches(t.Context(), runtime.Identity{"name": "x"}, map[string]any{})
	r.NoError(err)
	r.False(match)

	match, err = q.matches(t.Context(), runtime.Identity{"name": "x"}, map[string]any{"tier": "platform"})
	r.NoError(err)
	r.True(match)

	match, err = q.matches(t.Context(), runtime.Identity{"name": "x"}, map[string]any{"kind": "os"})
	r.NoError(err)
	r.True(match)
}

func TestSelectorNonbooleanResultIsError(t *testing.T) {
	r := require.New(t)
	q := mustCompileSelector(t, &v1alpha1.Selector{Expression: `identity.name`})

	_, err := q.matches(t.Context(), runtime.Identity{"name": "x"}, nil)
	r.Error(err)
	var selErr *SelectorError
	r.ErrorAs(err, &selErr)
	r.Equal(StageResource, selErr.Stage)
	r.Contains(err.Error(), "boolean")
}

func TestSelectorGenuineErrorsAreNotMisclassified(t *testing.T) {
	r := require.New(t)
	q := mustCompileSelector(t, &v1alpha1.Selector{Expression: `labels.tier + 1`})

	_, err := q.matches(t.Context(), runtime.Identity{"name": "x"}, map[string]any{"tier": "platform"})
	r.Error(err)
	var selErr *SelectorError
	r.ErrorAs(err, &selErr)
	r.Contains(err.Error(), "no such overload", "type errors must surface as selector errors, not nonmatches")
}

func TestSelectorSemverErrorsPropagate(t *testing.T) {
	r := require.New(t)
	q := mustCompileSelector(t, &v1alpha1.Selector{Expression: `semverCheck(identity.version, ">=2.0.0")`})

	_, err := q.matches(t.Context(), runtime.Identity{"name": "x", "version": "not-semver"}, nil)
	r.Error(err)
	r.Contains(err.Error(), "invalid version")
}

func TestSelectorCancellationIsNotNonmatch(t *testing.T) {
	r := require.New(t)
	q := mustCompileSelector(t, &v1alpha1.Selector{Expression: `labels.tier == "platform"`})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := q.matches(ctx, runtime.Identity{"name": "x"}, map[string]any{"tier": "platform"})
	r.Error(err)
	r.ErrorIs(err, context.Canceled)
}

func TestCompileCancellation(t *testing.T) {
	r := require.New(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := Compile(ctx, &v1alpha1.DiscoverySpec{
		ComponentSelector: &v1alpha1.Selector{Expression: `true`},
	})
	r.Error(err)
	r.ErrorIs(err, context.Canceled)
}
