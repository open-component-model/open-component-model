package discovery

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
)

func filteredKeys(f *Filtered) []string {
	keys := make([]string, 0, len(f.Components))
	for _, c := range f.Components {
		keys = append(keys, c.Key.String())
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
	r.Empty(f.Components)
	r.NotNil(f.Components)
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
	r.Empty(f.Components)
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

	byKey := map[string]FilteredComponent{}
	for _, c := range f.Components {
		byKey[c.Key.String()] = c
	}
	r.Empty(byKey["empty:1.0.0"].Resources, "component with zero resources must be kept")
	r.Len(byKey["some:1.0.0"].Resources, 1)
	r.Equal("keep-me", byKey["some:1.0.0"].Resources[0]["name"])
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
	for _, res := range f.Components[0].Resources {
		names = append(names, res["name"].(string))
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
	d := newDescriptor("d", "1.0.0", withResources(
		newResource("keep"),
		newResource("drop"),
	))
	snapshot, err := json.Marshal(d.Component)
	r.NoError(err)

	q, err := Compile(t.Context(), &v1alpha1.DiscoverySpec{
		ResourceSelector: &v1alpha1.Selector{MatchIdentity: map[string]string{"name": "keep"}},
	})
	r.NoError(err)
	_, err = q.Filter(t.Context(), Graph{Root: ComponentKey{Name: "d", Version: "1.0.0"}, Descriptors: []*descriptor.Descriptor{d}})
	r.NoError(err)

	after, err := json.Marshal(d.Component)
	r.NoError(err)
	r.JSONEq(string(snapshot), string(after), "filtering must copy before mutation")
	r.Len(d.Component.Resources, 2)
}

func TestFilterKeepsV2JSONShape(t *testing.T) {
	r := require.New(t)
	d := newDescriptor("d", "1.0.0",
		withReferences(newReference("to-x", "x", "1.0.0")),
		withResources(newResource("image", withResourceExtras(map[string]string{"platform": "linux"}))))

	q, err := Compile(t.Context(), &v1alpha1.DiscoverySpec{})
	r.NoError(err)
	f, err := q.Filter(t.Context(), Graph{Root: ComponentKey{Name: "d", Version: "1.0.0"}, Descriptors: []*descriptor.Descriptor{d}})
	r.NoError(err)
	r.Len(f.Components, 1)

	var decoded map[string]any
	r.NoError(json.Unmarshal(f.Components[0].Raw, &decoded))
	component := decoded["component"].(map[string]any)
	r.Contains(component, "componentReferences", "v2 JSON field names must be preserved")

	res := component["resources"].([]any)[0].(map[string]any)
	r.Equal("linux", res["extraIdentity"].(map[string]any)["platform"])
	r.Contains(res, "access")
}

func TestFilterDuplicateDescriptorsResolveOnce(t *testing.T) {
	r := require.New(t)
	d1 := newDescriptor("d", "1.0.0")
	d1dup := newDescriptor("d", "1.0.0", withResources(newResource("x")))

	q, err := Compile(t.Context(), &v1alpha1.DiscoverySpec{})
	r.NoError(err)
	f, err := q.Filter(t.Context(), Graph{Root: ComponentKey{Name: "d", Version: "1.0.0"}, Descriptors: []*descriptor.Descriptor{d1, d1dup}})
	r.NoError(err)
	r.Len(f.Components, 1)
	r.Empty(f.Components[0].Resources, "first descriptor wins on duplicate keys")
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
