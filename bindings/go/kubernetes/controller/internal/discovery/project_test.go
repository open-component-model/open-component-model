package discovery

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
)

func filterForResources(t *testing.T, spec *v1alpha1.DiscoverySpec, descriptors ...*descriptor.Descriptor) *Filtered {
	t.Helper()
	r := require.New(t)
	q, err := Compile(t.Context(), spec)
	r.NoError(err)
	root := ComponentKey{Name: descriptors[0].Component.Name, Version: descriptors[0].Component.Version}
	f, err := q.Filter(t.Context(), Graph{Root: root, Descriptors: descriptors})
	r.NoError(err)
	return f
}

func projectForSpec(t *testing.T, spec *v1alpha1.DiscoverySpec, descriptors ...*descriptor.Descriptor) *Payload {
	t.Helper()
	r := require.New(t)
	q, err := Compile(t.Context(), spec)
	r.NoError(err)
	root := ComponentKey{Name: descriptors[0].Component.Name, Version: descriptors[0].Component.Version}
	f, err := q.Filter(t.Context(), Graph{Root: root, Descriptors: descriptors})
	r.NoError(err)
	p, err := q.Project(t.Context(), f)
	r.NoError(err)
	return p
}

func TestProjectRaw(t *testing.T) {
	r := require.New(t)
	d1 := newDescriptor("b", "1.0.0", withResources(newResource("keep"), newResource("drop")))
	d2 := newDescriptor("a", "2.0.0")

	p := projectForSpec(t, &v1alpha1.DiscoverySpec{
		ResourceSelector: &v1alpha1.Selector{MatchIdentity: map[string]string{"name": "keep"}},
	}, d1, d2)

	r.Empty(p.Extracted)
	r.Equal(EmptyReasonNone, p.Reason)
	r.Len(p.Components, 2)

	var first map[string]any
	r.NoError(json.Unmarshal(p.Components[0], &first))
	r.Equal("a", first["component"].(map[string]any)["name"])

	var second map[string]any
	r.NoError(json.Unmarshal(p.Components[1], &second))
	resources := second["component"].(map[string]any)["resources"].([]any)
	r.Len(resources, 1, "raw output contains the filtered resource list")
	r.Equal("keep", resources[0].(map[string]any)["name"])
}

func TestProjectByResources(t *testing.T) {
	r := require.New(t)
	p := projectForSpec(t, specWithExtract(&v1alpha1.Extract{ByResources: map[string]string{
		"imageRef":         `resource.access.imageReference`,
		"resourceName":     `resource.name`,
		"componentName":    `component.name`,
		"componentVersion": `component.version`,
	}}), newDescriptor("comp", "1.0.0", withResources(newResource("one"), newResource("two"))))

	r.Empty(p.Components)
	r.Len(p.Extracted, 2, "one record per (component, resource) pair")
	r.Equal(map[string]any{
		"componentName":    "comp",
		"componentVersion": "1.0.0",
		"resourceName":     "one",
		"imageRef":         "ghcr.io/ocm/one:1.0.0",
	}, p.Extracted[0])
	r.Equal("two", p.Extracted[1]["resourceName"])
}

func TestProjectByResourcesMissingAccessOmitsFieldKeepsRecord(t *testing.T) {
	r := require.New(t)
	p := projectForSpec(t, specWithExtract(&v1alpha1.Extract{ByResources: map[string]string{
		"name":    `resource.name`,
		"missing": `resource.access.nonexistent.deeper`,
	}}), newDescriptor("comp", "1.0.0", withResources(newResource("one"))))

	r.Len(p.Extracted, 1)
	r.Equal(map[string]any{"name": "one"}, p.Extracted[0], "missing access omits the field but keeps the record")
}

func TestProjectByResourcesAllFieldsMissingKeepsEmptyRecord(t *testing.T) {
	r := require.New(t)
	p := projectForSpec(t, specWithExtract(&v1alpha1.Extract{ByResources: map[string]string{
		"only": `resource.absent`,
	}}), newDescriptor("comp", "1.0.0", withResources(newResource("one"))))

	r.Len(p.Extracted, 1)
	r.Empty(p.Extracted[0])
	r.NotNil(p.Extracted[0])
}

func TestProjectByComponents(t *testing.T) {
	r := require.New(t)
	d1 := newDescriptor("b", "1.0.0")
	d2 := newDescriptor("a", "1.0.0")

	p := projectForSpec(t, specWithExtract(&v1alpha1.Extract{ByComponents: map[string]string{
		"qualified": `component.name + ":" + component.version`,
	}}), d1, d2)

	r.Len(p.Extracted, 2)
	r.Equal("a:1.0.0", p.Extracted[0]["qualified"], "records follow the lexicographic component order")
	r.Equal("b:1.0.0", p.Extracted[1]["qualified"])
}

func TestProjectExpression(t *testing.T) {
	r := require.New(t)
	d1 := newDescriptor("b", "1.0.0")
	d2 := newDescriptor("a", "2.0.0")

	p := projectForSpec(t, specWithExtract(&v1alpha1.Extract{
		Expression: `components.filter(c, semverCheck(c.component.version, ">=2.0.0"))
			.map(c, {"name": c.component.name, "version": c.component.version})`,
	}), d1, d2, newDescriptor("z", "3.0.0", withReferences(newReference("to-a", "a", "2.0.0"))))

	r.Len(p.Extracted, 2)
	r.Equal("a", p.Extracted[0]["name"])
	r.Equal("z", p.Extracted[1]["name"])
	r.Equal("3.0.0", p.Extracted[1]["version"], "whole-expression mode binds full descriptors")
}

func TestProjectExpressionRecordOrderIsRetained(t *testing.T) {
	r := require.New(t)
	d1 := newDescriptor("a", "1.0.0")
	d2 := newDescriptor("b", "1.0.0")

	p := projectForSpec(t, specWithExtract(&v1alpha1.Extract{
		Expression: `components.map(c, {"n": c.component.name}).reverse()`,
	}), d1, d2)

	r.Equal("b", p.Extracted[0]["n"], "whole-expression record order must not be re-sorted")
	r.Equal("a", p.Extracted[1]["n"])
}

func TestProjectExpressionOutputTypes(t *testing.T) {
	for _, tc := range []struct {
		name string
		expr string
		want string
	}{
		{"non-list output", `{"n": "x"}`, "list of objects"},
		{"list of scalars", `["a", "b"]`, "list of objects"},
		{"missing access in expression", `components[0].component.absent.map(x, {"a": x})`, "no such key"},
		{"int-keyed map in list", `[{1: "x"}]`, "map key must be string"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			q, err := Compile(t.Context(), specWithExtract(&v1alpha1.Extract{Expression: tc.expr}))
			r.NoError(err)
			f := filterForResources(t, &v1alpha1.DiscoverySpec{}, newDescriptor("d", "1.0.0"))
			_, err = q.Project(t.Context(), f)
			r.Error(err)
			var extErr *ExtractError
			r.ErrorAs(err, &extErr)
			r.Contains(err.Error(), tc.want)
		})
	}
}

// TestProjectEmptyStageDoesNotFabricate: an empty selector stage must yield an
// empty extracted list and keep its reason even when the whole-expression
// extraction would produce records for an empty input list.
func TestProjectEmptyStageDoesNotFabricate(t *testing.T) {
	r := require.New(t)
	spec := &v1alpha1.DiscoverySpec{
		ComponentSelector: &v1alpha1.Selector{MatchLabels: map[string]string{"absent": "yes"}},
		Extract:           &v1alpha1.Extract{Expression: `[{"fabricated": true}]`},
	}
	d := newDescriptor("d", "1.0.0")
	q, err := Compile(t.Context(), spec)
	r.NoError(err)
	f, err := q.Filter(t.Context(), Graph{Root: ComponentKey{Name: "d", Version: "1.0.0"}, Descriptors: []*descriptor.Descriptor{d}})
	r.NoError(err)
	p, err := q.Project(t.Context(), f)
	r.NoError(err)
	r.NotNil(p.Extracted)
	r.Empty(p.Extracted)
	r.Empty(p.Components)
	r.Equal(EmptyReasonNoComponentsMatched, p.Reason)
}

func TestProjectExtractEvalErrorWrapsField(t *testing.T) {
	r := require.New(t)
	q, err := Compile(t.Context(), specWithExtract(&v1alpha1.Extract{ByResources: map[string]string{
		"zz": `semverCheck(resource.version, "not-a-constraint")`,
	}}))
	r.NoError(err)
	f := filterForResources(t, &v1alpha1.DiscoverySpec{}, newDescriptor("d", "1.0.0", withResources(newResource("res"))))
	_, err = q.Project(t.Context(), f)
	r.Error(err)
	var extErr *ExtractError
	r.ErrorAs(err, &extErr)
	r.Equal("zz", extErr.Field)
	r.Contains(err.Error(), "invalid constraint")
}

// TestProjectDeterminism: identical payloads regardless of descriptor input order.
func TestProjectDeterminism(t *testing.T) {
	r := require.New(t)
	spec := specWithExtract(&v1alpha1.Extract{ByComponents: map[string]string{
		"id": `component.name + ":" + component.version`,
	}})

	var reference []byte
	for seed := range int64(10) {
		descriptors := []*descriptor.Descriptor{
			newDescriptor("c", "1.0.0"),
			newDescriptor("a", "2.0.0"),
			newDescriptor("b", "1.10.0"),
			newDescriptor("b", "1.2.0"),
		}
		rng := rand.New(rand.NewSource(seed))
		rng.Shuffle(len(descriptors), func(i, j int) { descriptors[i], descriptors[j] = descriptors[j], descriptors[i] })

		p := projectForSpec(t, spec, descriptors...)
		marshaled, err := json.Marshal(p.Extracted)
		r.NoError(err)
		if reference == nil {
			reference = marshaled
			continue
		}
		r.JSONEq(string(reference), string(marshaled), fmt.Sprintf("seed %d", seed))
	}
}

func TestProjectEmptySelectorStagesKeepEmptyReasonsDistinct(t *testing.T) {
	r := require.New(t)
	root := newDescriptor("root", "1.0.0", withReferences(newReference("to-child", "child", "1.0.0")))
	child := newDescriptor("child", "1.0.0")
	graph := func() Graph {
		return Graph{Root: ComponentKey{Name: "root", Version: "1.0.0"}, Descriptors: []*descriptor.Descriptor{root, child}}
	}

	q, err := Compile(t.Context(), &v1alpha1.DiscoverySpec{
		ReferenceSelector: &v1alpha1.Selector{MatchIdentity: map[string]string{"componentName": "absent"}},
	})
	r.NoError(err)
	f, err := q.Filter(t.Context(), graph())
	r.NoError(err)
	p, err := q.Project(t.Context(), f)
	r.NoError(err)
	r.Equal(EmptyReasonNoReferencesMatched, p.Reason)
	r.NotNil(p.Components, "selected empty output is [], not omitted")
	r.Empty(p.Components)

	q, err = Compile(t.Context(), &v1alpha1.DiscoverySpec{
		ComponentSelector: &v1alpha1.Selector{MatchLabels: map[string]string{"absent": "yes"}},
	})
	r.NoError(err)
	f, err = q.Filter(t.Context(), graph())
	r.NoError(err)
	r.Equal(EmptyReasonNoComponentsMatched, f.Reason)

	q, err = Compile(t.Context(), &v1alpha1.DiscoverySpec{
		ResourceSelector: &v1alpha1.Selector{MatchIdentity: map[string]string{"name": "absent"}},
	})
	r.NoError(err)
	f, err = q.Filter(t.Context(), graph())
	r.NoError(err)
	r.Equal(EmptyReasonNone, f.Reason, "resource-empty stages have no distinct reason")
	r.Len(f.Components, 2)
}
