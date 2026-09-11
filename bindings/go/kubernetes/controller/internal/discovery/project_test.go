package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"

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
	r.Len(f.Descriptors, 2)
}

// descriptorWithProvider builds a descriptor with an explicit provider so tests
// can construct one whose v2 conversion fails (empty provider name).
func descriptorWithProvider(name, version, provider string, opts ...descriptorOption) *descriptor.Descriptor {
	d := newDescriptor(name, version, opts...)
	d.Component.Provider = descriptor.Provider{Name: provider}
	return d
}

// TestProjectSerializationFailureNotClassified: a descriptor whose v2 conversion
// fails (empty provider name) does not make Filter fail. Both raw and extraction
// Project fail with the component context and neither SelectorError nor
// ExtractError, so the controller retries instead of stalling.
func TestProjectSerializationFailureNotClassified(t *testing.T) {
	bad := descriptorWithProvider("bad", "1.0.0", "", withResources(newResource("res")))

	for _, tc := range []struct {
		name string
		spec *v1alpha1.DiscoverySpec
	}{
		{"raw", &v1alpha1.DiscoverySpec{}},
		{"byResources", specWithExtract(&v1alpha1.Extract{ByResources: map[string]string{"n": `resource.name`}})},
		{"byComponents", specWithExtract(&v1alpha1.Extract{ByComponents: map[string]string{"n": `component.name`}})},
		{"expression", specWithExtract(&v1alpha1.Extract{Expression: `components.map(c, {"n": c.component.name})`})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			q, err := Compile(t.Context(), tc.spec)
			r.NoError(err)
			f, err := q.Filter(t.Context(), Graph{Root: ComponentKey{Name: "bad", Version: "1.0.0"}, Descriptors: []*descriptor.Descriptor{bad}})
			r.NoError(err, "conversion is not performed during filtering")

			_, err = q.Project(t.Context(), f)
			r.Error(err)
			r.Contains(err.Error(), "bad:1.0.0", "projection error carries component context")
			var selErr *SelectorError
			var extErr *ExtractError
			r.False(errors.As(err, &selErr), "must not be a SelectorError")
			r.False(errors.As(err, &extErr), "must not be an ExtractError")
		})
	}
}

// TestProjectSelectingBadResourceAwayPermitsProjection: a resource whose access
// conversion would fail is not serialized once selected away.
func TestProjectSelectingBadResourceAwayPermitsProjection(t *testing.T) {
	r := require.New(t)
	badAccess := newResource("drop")
	badAccess.Access = &runtime.Raw{
		Type: runtime.NewVersionedType("OCIImage", "v1"),
		Data: []byte(`{not valid json`),
	}
	d := newDescriptor("d", "1.0.0", withResources(badAccess, newResource("keep")))

	// Selecting the malformed resource away permits raw projection.
	q, err := Compile(t.Context(), &v1alpha1.DiscoverySpec{
		ResourceSelector: &v1alpha1.Selector{MatchIdentity: map[string]string{"name": "keep"}},
	})
	r.NoError(err)
	f, err := q.Filter(t.Context(), Graph{Root: ComponentKey{Name: "d", Version: "1.0.0"}, Descriptors: []*descriptor.Descriptor{d}})
	r.NoError(err)
	p, err := q.Project(t.Context(), f)
	r.NoError(err)
	r.Len(p.Components, 1)

	// Retaining the malformed resource fails projection with component context.
	q, err = Compile(t.Context(), &v1alpha1.DiscoverySpec{})
	r.NoError(err)
	f, err = q.Filter(t.Context(), Graph{Root: ComponentKey{Name: "d", Version: "1.0.0"}, Descriptors: []*descriptor.Descriptor{d}})
	r.NoError(err)
	_, err = q.Project(t.Context(), f)
	r.Error(err)
	r.Contains(err.Error(), "d:1.0.0")
	var selErr *SelectorError
	var extErr *ExtractError
	r.False(errors.As(err, &selErr))
	r.False(errors.As(err, &extErr))
}

// TestProjectRawWireFormatFidelity: raw output matches marshaling the expected
// typed v2 descriptor, not a re-marshaled generic map.
func TestProjectRawWireFormatFidelity(t *testing.T) {
	r := require.New(t)
	d := newDescriptor("d", "1.0.0",
		withReferences(newReference("to-x", "x", "1.0.0", withRefExtras(map[string]string{"channel": "stable"}))),
		withResources(newResource("image", withResourceExtras(map[string]string{"platform": "linux"}))))

	p := projectForSpec(t, &v1alpha1.DiscoverySpec{}, d)
	r.Len(p.Components, 1)

	expected, err := json.Marshal(mustConvertV2(t, d))
	r.NoError(err)
	r.JSONEq(string(expected), string(p.Components[0]), "raw bytes equal marshaling the typed v2 descriptor")

	var decoded map[string]any
	r.NoError(json.Unmarshal(p.Components[0], &decoded))
	comp := decoded["component"].(map[string]any)
	r.Equal("d", comp["name"], "root component metadata uses v2 field names")
	refs := comp["componentReferences"].([]any)
	r.Equal("stable", refs[0].(map[string]any)["extraIdentity"].(map[string]any)["channel"])
	res := comp["resources"].([]any)[0].(map[string]any)
	r.Equal("linux", res["extraIdentity"].(map[string]any)["platform"])
	r.Contains(res, "access")
}

// TestProjectEmptyResourcesWireShape: resource null-versus-[] distinction reaches
// both raw output and CEL bindings.
func TestProjectEmptyResourcesWireShape(t *testing.T) {
	for _, tc := range []struct {
		name       string
		resources  []descriptor.Resource
		selector   *v1alpha1.Selector
		wantSuffix string
	}{
		{"nil resources, no selector", nil, nil, `"resources":null`},
		{"empty resources, no selector", []descriptor.Resource{}, nil, `"resources":[]`},
		{"nil resources, active selector", nil, &v1alpha1.Selector{MatchIdentity: map[string]string{"name": "absent"}}, `"resources":[]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			d := newDescriptor("d", "1.0.0")
			d.Component.Resources = tc.resources

			p := projectForSpec(t, &v1alpha1.DiscoverySpec{ResourceSelector: tc.selector}, d)
			r.Len(p.Components, 1)
			r.Contains(string(p.Components[0]), tc.wantSuffix)

			// by-component sees the resource list; by-resource emits no records.
			pc := projectForSpec(t, specWithExtract(&v1alpha1.Extract{ByComponents: map[string]string{
				"name": `component.name`,
			}}), cloneForSelector(d, tc.resources))
			r.Len(pc.Extracted, 1)
			r.Equal("d", pc.Extracted[0]["name"])

			pr := projectForSpec(t, specWithExtract(&v1alpha1.Extract{ByResources: map[string]string{
				"n": `resource.name`,
			}}), cloneForSelector(d, tc.resources))
			r.Empty(pr.Extracted)
		})
	}
}

func cloneForSelector(d *descriptor.Descriptor, resources []descriptor.Resource) *descriptor.Descriptor {
	c := newDescriptor(d.Component.Name, d.Component.Version)
	c.Component.Resources = resources
	return c
}

// TestProjectConsistencyAcrossModes: one filtered result projected through every
// mode sees the same filtered resources; inputs and result remain unchanged.
func TestProjectConsistencyAcrossModes(t *testing.T) {
	r := require.New(t)
	d := newDescriptor("comp", "1.0.0", withResources(newResource("keep"), newResource("drop")))

	specs := map[string]*v1alpha1.DiscoverySpec{
		"raw":         {ResourceSelector: &v1alpha1.Selector{MatchIdentity: map[string]string{"name": "keep"}}},
		"byResources": {ResourceSelector: &v1alpha1.Selector{MatchIdentity: map[string]string{"name": "keep"}}, Extract: &v1alpha1.Extract{ByResources: map[string]string{"n": `resource.name`}}},
		"byComponent": {ResourceSelector: &v1alpha1.Selector{MatchIdentity: map[string]string{"name": "keep"}}, Extract: &v1alpha1.Extract{ByComponents: map[string]string{"c": `size(component.resources)`}}},
		"expression":  {ResourceSelector: &v1alpha1.Selector{MatchIdentity: map[string]string{"name": "keep"}}, Extract: &v1alpha1.Extract{Expression: `components.map(c, {"c": size(c.component.resources)})`}},
	}

	before, err := json.Marshal(mustConvertV2(t, d))
	r.NoError(err)

	q, err := Compile(t.Context(), specs["raw"])
	r.NoError(err)
	f, err := q.Filter(t.Context(), Graph{Root: ComponentKey{Name: "comp", Version: "1.0.0"}, Descriptors: []*descriptor.Descriptor{d}})
	r.NoError(err)
	r.Len(f.Descriptors[0].Component.Resources, 1)
	r.Equal("keep", f.Descriptors[0].Component.Resources[0].Name)

	for name, spec := range specs {
		qq, err := Compile(t.Context(), spec)
		r.NoError(err)
		ff, err := qq.Filter(t.Context(), Graph{Root: ComponentKey{Name: "comp", Version: "1.0.0"}, Descriptors: []*descriptor.Descriptor{d}})
		r.NoError(err)
		p, err := qq.Project(t.Context(), ff)
		r.NoError(err, name)
		switch name {
		case "raw":
			r.Len(p.Components, 1)
		default:
			r.NotEmpty(p.Extracted, name)
		}
	}

	after, err := json.Marshal(mustConvertV2(t, d))
	r.NoError(err)
	r.JSONEq(string(before), string(after), "inputs unchanged across all projection modes")
}

// TestProjectCancellation: canceled Project in every mode returns an error that
// retains the context cause, not an empty-success result.
func TestProjectCancellation(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec *v1alpha1.DiscoverySpec
	}{
		{"raw", &v1alpha1.DiscoverySpec{}},
		{"byResources", specWithExtract(&v1alpha1.Extract{ByResources: map[string]string{"n": `resource.name`}})},
		{"byComponents", specWithExtract(&v1alpha1.Extract{ByComponents: map[string]string{"n": `component.name`}})},
		{"expression", specWithExtract(&v1alpha1.Extract{Expression: `components.map(c, {"n": c.component.name})`})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			d := newDescriptor("d", "1.0.0", withResources(newResource("res")))
			q, err := Compile(t.Context(), tc.spec)
			r.NoError(err)
			f := filterForResources(t, &v1alpha1.DiscoverySpec{}, d)

			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			_, err = q.Project(ctx, f)
			r.Error(err)
			r.ErrorIs(err, context.Canceled)
		})
	}
}

func TestFilterCancellation(t *testing.T) {
	r := require.New(t)
	d := newDescriptor("d", "1.0.0", withResources(newResource("res")))
	q, err := Compile(t.Context(), &v1alpha1.DiscoverySpec{
		ResourceSelector: &v1alpha1.Selector{MatchIdentity: map[string]string{"name": "res"}},
	})
	r.NoError(err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = q.Filter(ctx, Graph{Root: ComponentKey{Name: "d", Version: "1.0.0"}, Descriptors: []*descriptor.Descriptor{d}})
	r.Error(err)
	r.ErrorIs(err, context.Canceled)
}
