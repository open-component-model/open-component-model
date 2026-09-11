package discovery

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
)

func filteredKeys(f *Filtered) []string {
	keys := make([]string, 0, len(f.Descriptors))
	for _, d := range f.Descriptors {
		keys = append(keys, ComponentKey{Name: d.Component.Name, Version: d.Component.Version}.String())
	}
	return keys
}

func TestFilterEmptySelectorsKeepEverything(t *testing.T) {
	r := require.New(t)
	root := newDescriptor("root", "1.0.0",
		withReferences(newReference("child-ref", "child", "2.0.0")))
	child := newDescriptor("child", "2.0.0")

	q, err := Compile(t.Context(), &v1alpha1.DiscoverySpec{ReferenceSelector: &v1alpha1.Selector{}})
	r.NoError(err)
	f, err := q.Filter(t.Context(), Graph{
		Root:        ComponentKey{Name: "root", Version: "1.0.0"},
		Descriptors: []*descriptor.Descriptor{root, child},
	})
	r.NoError(err)
	r.Equal(EmptyReasonNone, f.Reason)
	r.ElementsMatch([]string{"root:1.0.0", "child:2.0.0"}, filteredKeys(f))
}

func TestFilterReferenceSelectorExcludesRoot(t *testing.T) {
	r := require.New(t)
	root := newDescriptor("root", "1.0.0", withReferences(
		newReference("to-child", "child", "2.0.0"),
		newReference("to-other", "other", "1.0.0")))
	child := newDescriptor("child", "2.0.0")
	other := newDescriptor("other", "1.0.0")

	q, err := Compile(t.Context(), &v1alpha1.DiscoverySpec{
		ReferenceSelector: &v1alpha1.Selector{MatchIdentity: map[string]string{"componentName": "child"}},
	})
	r.NoError(err)
	f, err := q.Filter(t.Context(), Graph{Root: ComponentKey{Name: "root", Version: "1.0.0"}, Descriptors: []*descriptor.Descriptor{root, child, other}})
	r.NoError(err)
	r.Equal([]string{"child:2.0.0"}, filteredKeys(f))
}

// TestFilterNestedDiamondGraph: A references B and C; B and C both reference D.
// A selector matching D must keep D exactly once, even behind unmatched parents.
func TestFilterNestedDiamondGraph(t *testing.T) {
	r := require.New(t)
	a := newDescriptor("a", "1.0.0", withReferences(
		newReference("to-b", "b", "1.0.0"),
		newReference("to-c", "c", "1.0.0")))
	b := newDescriptor("b", "1.0.0", withReferences(newReference("to-d", "d", "1.0.0")))
	c := newDescriptor("c", "1.0.0", withReferences(newReference("to-d", "d", "1.0.0")))
	d := newDescriptor("d", "1.0.0")

	q, err := Compile(t.Context(), &v1alpha1.DiscoverySpec{
		ReferenceSelector: &v1alpha1.Selector{MatchIdentity: map[string]string{"componentName": "d"}},
	})
	r.NoError(err)
	f, err := q.Filter(t.Context(), Graph{Root: ComponentKey{Name: "a", Version: "1.0.0"}, Descriptors: []*descriptor.Descriptor{a, b, c, d}})
	r.NoError(err)
	r.Equal([]string{"d:1.0.0"}, filteredKeys(f), "D matches behind unmatched ancestors and must appear once")
}

// TestFilterMultipleIncomingReferences: a target with several incoming
// references survives when any of them matches.
func TestFilterMultipleIncomingReferences(t *testing.T) {
	r := require.New(t)
	root := newDescriptor("root", "1.0.0", withReferences(
		newReference("first", "target", "1.0.0", withRefLabels(stringLabel("kind", "runtime"))),
		newReference("second", "target", "1.0.0", withRefLabels(stringLabel("kind", "buildtime"))),
	))
	target := newDescriptor("target", "1.0.0")

	q, err := Compile(t.Context(), &v1alpha1.DiscoverySpec{
		ReferenceSelector: &v1alpha1.Selector{MatchLabels: map[string]string{"kind": "buildtime"}},
	})
	r.NoError(err)
	f, err := q.Filter(t.Context(), Graph{Root: ComponentKey{Name: "root", Version: "1.0.0"}, Descriptors: []*descriptor.Descriptor{root, target}})
	r.NoError(err)
	r.Equal([]string{"target:1.0.0"}, filteredKeys(f))
}

func TestFilterReferenceIdentityAndExtras(t *testing.T) {
	r := require.New(t)
	root := newDescriptor("root", "1.0.0", withReferences(
		newReference("local-name", "target", "9.9.9",
			withRefExtras(map[string]string{"channel": "stable"})),
	))
	target := newDescriptor("target", "9.9.9")

	q, err := Compile(t.Context(), &v1alpha1.DiscoverySpec{
		ReferenceSelector: &v1alpha1.Selector{Expression: `
			identity.name == "local-name" &&
			identity.componentName == "target" &&
			identity.channel == "stable" &&
			semverCheck(identity.version, ">=9.0.0")`},
	})
	r.NoError(err)
	f, err := q.Filter(t.Context(), Graph{Root: ComponentKey{Name: "root", Version: "1.0.0"}, Descriptors: []*descriptor.Descriptor{root, target}})
	r.NoError(err)
	r.Equal([]string{"target:9.9.9"}, filteredKeys(f))
}

func TestFilterNoReferencesMatchedReason(t *testing.T) {
	r := require.New(t)
	root := newDescriptor("root", "1.0.0", withReferences(newReference("to-child", "child", "1.0.0")))
	child := newDescriptor("child", "1.0.0")

	q, err := Compile(t.Context(), &v1alpha1.DiscoverySpec{
		ReferenceSelector: &v1alpha1.Selector{MatchIdentity: map[string]string{"componentName": "absent"}},
	})
	r.NoError(err)
	f, err := q.Filter(t.Context(), Graph{Root: ComponentKey{Name: "root", Version: "1.0.0"}, Descriptors: []*descriptor.Descriptor{root, child}})
	r.NoError(err)
	r.Empty(f.Descriptors)
	r.NotNil(f.Descriptors)
	r.Equal(EmptyReasonNoReferencesMatched, f.Reason)
}

func TestFilterComponentSelectorStructuredLabelsAndReason(t *testing.T) {
	r := require.New(t)
	root := newDescriptor("root", "1.0.0",
		withComponentLabels(structuredLabel("feature", map[string]any{"enabled": true})))
	child := newDescriptor("child", "1.0.0")

	q, err := Compile(t.Context(), &v1alpha1.DiscoverySpec{
		ComponentSelector: &v1alpha1.Selector{Expression: `labels.feature.enabled == true`},
	})
	r.NoError(err)
	f, err := q.Filter(t.Context(), Graph{Root: ComponentKey{Name: "root", Version: "1.0.0"}, Descriptors: []*descriptor.Descriptor{root, child}})
	r.NoError(err)
	r.Equal([]string{"root:1.0.0"}, filteredKeys(f))

	q, err = Compile(t.Context(), &v1alpha1.DiscoverySpec{
		ComponentSelector: &v1alpha1.Selector{Expression: `has(labels.absent)`},
	})
	r.NoError(err)
	f, err = q.Filter(t.Context(), Graph{Root: ComponentKey{Name: "root", Version: "1.0.0"}, Descriptors: []*descriptor.Descriptor{root, child}})
	r.NoError(err)
	r.Empty(f.Descriptors)
	r.Equal(EmptyReasonNoComponentsMatched, f.Reason)
}

func TestFilterResourceSelectorKeepsZeroResourceComponents(t *testing.T) {
	r := require.New(t)
	empty := newDescriptor("empty", "1.0.0")
	some := newDescriptor("some", "1.0.0", withResources(
		newResource("keep-me"),
		newResource("drop-me"),
	))

	q, err := Compile(t.Context(), &v1alpha1.DiscoverySpec{
		ResourceSelector: &v1alpha1.Selector{MatchIdentity: map[string]string{"name": "keep-me"}},
	})
	r.NoError(err)
	f, err := q.Filter(t.Context(), Graph{Root: ComponentKey{Name: "empty", Version: "1.0.0"}, Descriptors: []*descriptor.Descriptor{empty, some}})
	r.NoError(err)
	r.Equal(EmptyReasonNone, f.Reason, "resource-empty stages are ordinary success")
	r.ElementsMatch([]string{"empty:1.0.0", "some:1.0.0"}, filteredKeys(f))

	byKey := map[string]*descriptor.Descriptor{}
	for _, d := range f.Descriptors {
		byKey[ComponentKey{Name: d.Component.Name, Version: d.Component.Version}.String()] = d
	}
	r.Empty(byKey["empty:1.0.0"].Component.Resources, "component with zero resources must be kept")
	r.Len(byKey["some:1.0.0"].Component.Resources, 1)
	r.Equal("keep-me", byKey["some:1.0.0"].Component.Resources[0].Name)
}

func TestFilterResourceDeclarationOrderPreserved(t *testing.T) {
	r := require.New(t)
	d := newDescriptor("d", "1.0.0", withResources(
		newResource("z-first"),
		newResource("a-second"),
		newResource("m-third"),
	))

	q, err := Compile(t.Context(), &v1alpha1.DiscoverySpec{})
	r.NoError(err)
	f, err := q.Filter(t.Context(), Graph{Root: ComponentKey{Name: "d", Version: "1.0.0"}, Descriptors: []*descriptor.Descriptor{d}})
	r.NoError(err)
	names := make([]string, 0, 3)
	for _, res := range f.Descriptors[0].Component.Resources {
		names = append(names, res.Name)
	}
	r.Equal([]string{"z-first", "a-second", "m-third"}, names)
}

func TestFilterSortsLexicographicallyNotBySemver(t *testing.T) {
	r := require.New(t)
	d1 := newDescriptor("b", "1.2.0")
	d2 := newDescriptor("a", "1.10.0")
	d3 := newDescriptor("a", "1.2.0")

	q, err := Compile(t.Context(), &v1alpha1.DiscoverySpec{})
	r.NoError(err)
	f, err := q.Filter(t.Context(), Graph{Root: ComponentKey{Name: "b", Version: "1.2.0"}, Descriptors: []*descriptor.Descriptor{d1, d2, d3}})
	r.NoError(err)
	r.Equal([]string{"a:1.10.0", "a:1.2.0", "b:1.2.0"}, filteredKeys(f), "plain lexicographic order: 1.10.0 sorts before 1.2.0")
}

func TestFilterDoesNotMutateInputs(t *testing.T) {
	r := require.New(t)
	d := newDescriptor("d", "1.0.0",
		withComponentLabels(structuredLabel("feature", map[string]any{"enabled": true})),
		withReferences(newReference("to-x", "x", "1.0.0")),
		withResources(
			newResource("drop", withResourceLabels(stringLabel("kind", "buildtime"))),
			newResource("keep"),
		))
	snapshot, err := json.Marshal(mustConvertV2(t, d))
	r.NoError(err)

	q, err := Compile(t.Context(), &v1alpha1.DiscoverySpec{
		ResourceSelector: &v1alpha1.Selector{MatchIdentity: map[string]string{"name": "keep"}},
	})
	r.NoError(err)

	// Filter multiple times against the same graph, then project every mode.
	for range 3 {
		f, err := q.Filter(t.Context(), Graph{Root: ComponentKey{Name: "d", Version: "1.0.0"}, Descriptors: []*descriptor.Descriptor{d}})
		r.NoError(err)
		_, err = q.Project(t.Context(), f)
		r.NoError(err)
	}

	// Typed assertions: original resource order and count are unchanged.
	r.Len(d.Component.Resources, 2)
	r.Equal("drop", d.Component.Resources[0].Name)
	r.Equal("keep", d.Component.Resources[1].Name)
	r.Len(d.Component.Resources[0].Labels, 1)

	after, err := json.Marshal(mustConvertV2(t, d))
	r.NoError(err)
	r.JSONEq(string(snapshot), string(after), "filtering and projection must not mutate inputs")
}

func TestFilterDuplicateDescriptorsResolveOnce(t *testing.T) {
	r := require.New(t)
	d1 := newDescriptor("d", "1.0.0")
	d1dup := newDescriptor("d", "1.0.0", withResources(newResource("x")))

	q, err := Compile(t.Context(), &v1alpha1.DiscoverySpec{})
	r.NoError(err)
	f, err := q.Filter(t.Context(), Graph{Root: ComponentKey{Name: "d", Version: "1.0.0"}, Descriptors: []*descriptor.Descriptor{d1, d1dup}})
	r.NoError(err)
	r.Len(f.Descriptors, 1)
	r.Empty(f.Descriptors[0].Component.Resources, "first descriptor wins on duplicate keys")
}

func TestFilterIgnoresNilAndEmptyGraph(t *testing.T) {
	r := require.New(t)
	d := newDescriptor("d", "1.0.0")

	q, err := Compile(t.Context(), &v1alpha1.DiscoverySpec{})
	r.NoError(err)

	f, err := q.Filter(t.Context(), Graph{Root: ComponentKey{Name: "d", Version: "1.0.0"}, Descriptors: []*descriptor.Descriptor{nil, d, nil}})
	r.NoError(err)
	r.Equal([]string{"d:1.0.0"}, filteredKeys(f))

	f, err = q.Filter(t.Context(), Graph{Descriptors: nil})
	r.NoError(err)
	r.NotNil(f.Descriptors)
	r.Empty(f.Descriptors)
	r.Equal(EmptyReasonNoComponentsMatched, f.Reason)
}

func TestFilterPreservesGraphOrder(t *testing.T) {
	r := require.New(t)
	d1 := newDescriptor("b", "1.0.0")
	d2 := newDescriptor("a", "1.0.0")
	graph := Graph{Root: ComponentKey{Name: "b", Version: "1.0.0"}, Descriptors: []*descriptor.Descriptor{d1, d2}}

	q, err := Compile(t.Context(), &v1alpha1.DiscoverySpec{})
	r.NoError(err)
	_, err = q.Filter(t.Context(), graph)
	r.NoError(err)
	r.Same(d1, graph.Descriptors[0], "Graph.Descriptors order must not change")
	r.Same(d2, graph.Descriptors[1])
}

func TestFilterSelectorErrorSurfacesStage(t *testing.T) {
	r := require.New(t)
	d := newDescriptor("d", "1.0.0", withResources(newResource("res")))

	q, err := Compile(t.Context(), &v1alpha1.DiscoverySpec{
		ResourceSelector: &v1alpha1.Selector{Expression: `semverCheck(identity.version, ">=")`},
	})
	r.NoError(err)
	_, err = q.Filter(t.Context(), Graph{Root: ComponentKey{Name: "d", Version: "1.0.0"}, Descriptors: []*descriptor.Descriptor{d}})
	r.Error(err)
	var selErr *SelectorError
	r.ErrorAs(err, &selErr)
	r.Equal(StageResource, selErr.Stage)
	r.Contains(err.Error(), "invalid constraint")
}
