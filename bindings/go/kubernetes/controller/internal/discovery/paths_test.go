package discovery

import (
	"encoding/json/v2"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/ocm"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func keyed(descriptors ...*descriptor.Descriptor) map[ComponentKey]*descriptor.Descriptor {
	byKey := make(map[ComponentKey]*descriptor.Descriptor, len(descriptors))
	for _, d := range descriptors {
		byKey[ComponentKey{Name: d.Component.Name, Version: d.Component.Version}] = d
	}
	return byKey
}

func key(name, version string) ComponentKey {
	return ComponentKey{Name: name, Version: version}
}

func TestReferencePaths(t *testing.T) {
	root := key("root", "1.0.0")

	tests := []struct {
		name        string
		descriptors []*descriptor.Descriptor
		want        map[ComponentKey][]runtime.Identity
	}{
		{
			name:        "root only",
			descriptors: []*descriptor.Descriptor{newDescriptor("root", "1.0.0")},
			want:        map[ComponentKey][]runtime.Identity{root: {}},
		},
		{
			name: "chain uses reference names, not component names",
			descriptors: []*descriptor.Descriptor{
				newDescriptor("root", "1.0.0", withReferences(newReference("platform", "acme.org/platform", "1.0.0"))),
				newDescriptor("acme.org/platform", "1.0.0", withReferences(newReference("chart", "acme.org/chart", "2.0.0"))),
				newDescriptor("acme.org/chart", "2.0.0"),
			},
			want: map[ComponentKey][]runtime.Identity{
				root:                              {},
				key("acme.org/platform", "1.0.0"): {{"name": "platform"}},
				key("acme.org/chart", "2.0.0"):    {{"name": "platform"}, {"name": "chart"}},
			},
		},
		{
			name: "shortest path wins over a lexicographically smaller longer one",
			descriptors: []*descriptor.Descriptor{
				newDescriptor("root", "1.0.0", withReferences(
					newReference("a", "a", "1.0.0"),
					newReference("z", "c", "1.0.0"),
				)),
				newDescriptor("a", "1.0.0", withReferences(newReference("c", "c", "1.0.0"))),
				newDescriptor("c", "1.0.0"),
			},
			want: map[ComponentKey][]runtime.Identity{
				root:              {},
				key("a", "1.0.0"): {{"name": "a"}},
				key("c", "1.0.0"): {{"name": "z"}},
			},
		},
		{
			name: "diamond picks the lexicographically smallest equally short path",
			descriptors: []*descriptor.Descriptor{
				newDescriptor("root", "1.0.0", withReferences(
					newReference("y", "y", "1.0.0"),
					newReference("x", "x", "1.0.0"),
				)),
				newDescriptor("x", "1.0.0", withReferences(newReference("via-x", "c", "1.0.0"))),
				newDescriptor("y", "1.0.0", withReferences(newReference("via-y", "c", "1.0.0"))),
				newDescriptor("c", "1.0.0"),
			},
			want: map[ComponentKey][]runtime.Identity{
				root:              {},
				key("x", "1.0.0"): {{"name": "x"}},
				key("y", "1.0.0"): {{"name": "y"}},
				key("c", "1.0.0"): {{"name": "x"}, {"name": "via-x"}},
			},
		},
		{
			name: "duplicate reference entries to one target pick the smallest",
			descriptors: []*descriptor.Descriptor{
				newDescriptor("root", "1.0.0", withReferences(
					newReference("c-dupe", "c", "1.0.0"),
					newReference("c", "c", "1.0.0"),
				)),
				newDescriptor("c", "1.0.0"),
			},
			want: map[ComponentKey][]runtime.Identity{
				root:              {},
				key("c", "1.0.0"): {{"name": "c"}},
			},
		},
		{
			name: "extra identity is part of the path element",
			descriptors: []*descriptor.Descriptor{
				newDescriptor("root", "1.0.0", withReferences(
					newReference("lib", "lib", "1.0.0", withRefExtras(map[string]string{"flavor": "fips"})),
				)),
				newDescriptor("lib", "1.0.0"),
			},
			want: map[ComponentKey][]runtime.Identity{
				root:                {},
				key("lib", "1.0.0"): {{"name": "lib", "flavor": "fips"}},
			},
		},
		{
			name: "version is kept only where it disambiguates",
			descriptors: []*descriptor.Descriptor{
				newDescriptor("root", "1.0.0", withReferences(
					newReference("lib", "lib", "1.0.0"),
					newReference("lib", "lib", "2.0.0"),
					newReference("tool", "tool", "3.0.0"),
				)),
				newDescriptor("lib", "1.0.0"),
				newDescriptor("lib", "2.0.0"),
				newDescriptor("tool", "3.0.0"),
			},
			want: map[ComponentKey][]runtime.Identity{
				root:                 {},
				key("lib", "1.0.0"):  {{"name": "lib", "version": "1.0.0"}},
				key("lib", "2.0.0"):  {{"name": "lib", "version": "2.0.0"}},
				key("tool", "3.0.0"): {{"name": "tool"}},
			},
		},
		{
			name: "cycles terminate and unresolved targets are skipped",
			descriptors: []*descriptor.Descriptor{
				newDescriptor("root", "1.0.0", withReferences(
					newReference("a", "a", "1.0.0"),
					newReference("missing", "missing", "1.0.0"),
				)),
				newDescriptor("a", "1.0.0", withReferences(newReference("back", "root", "1.0.0"))),
			},
			want: map[ComponentKey][]runtime.Identity{
				root:              {},
				key("a", "1.0.0"): {{"name": "a"}},
			},
		},
		{
			// The target is reachable via left (final hop "z") and via right
			// (final hop "a"). Both reach it in two hops. The winner is the
			// path whose complete element sequence is smallest: the left
			// prefix "a" beats the right prefix "b", so the target's path is
			// [a, z] even though its final hop "z" is larger than "a". An
			// implementation ordering only final hops would wrongly pick [b, a].
			name: "earlier prefix wins despite larger final hop",
			descriptors: []*descriptor.Descriptor{
				newDescriptor("root", "1.0.0", withReferences(
					newReference("a", "left", "1.0.0"),
					newReference("b", "right", "1.0.0"),
				)),
				newDescriptor("left", "1.0.0", withReferences(newReference("z", "target", "1.0.0"))),
				newDescriptor("right", "1.0.0", withReferences(newReference("a", "target", "1.0.0"))),
				newDescriptor("target", "1.0.0"),
			},
			want: map[ComponentKey][]runtime.Identity{
				root:                   {},
				key("left", "1.0.0"):   {{"name": "a"}},
				key("right", "1.0.0"):  {{"name": "b"}},
				key("target", "1.0.0"): {{"name": "a"}, {"name": "z"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			r.Equal(tt.want, referencePaths(root, keyed(tt.descriptors...)))
		})
	}
}

func TestReferencePathsWithoutRoot(t *testing.T) {
	tests := []struct {
		name string
		root ComponentKey
	}{
		{name: "root not in graph", root: key("root", "1.0.0")},
		{name: "root unset", root: ComponentKey{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			r.Nil(referencePaths(tt.root, keyed(newDescriptor("other", "1.0.0"))))
		})
	}
}

// projectGraph compiles spec and runs Filter and Project over a graph rooted
// at the first descriptor.
func projectGraph(t *testing.T, spec *v1alpha1.DiscoverySpec, descriptors ...*descriptor.Descriptor) *Payload {
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

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v, runtime.JSONOptions)
	require.NoError(t, err)
	return string(raw)
}

// nestedGraph is root -> platform -> chart, with the resources on chart.
func nestedGraph(resources ...descriptor.Resource) []*descriptor.Descriptor {
	return []*descriptor.Descriptor{
		newDescriptor("acme.org/root", "1.0.0",
			withReferences(newReference("platform", "acme.org/platform", "1.0.0")),
			withComponentLabels(stringLabel("tier", "root"))),
		newDescriptor("acme.org/platform", "1.0.0",
			withReferences(newReference("chart", "acme.org/chart", "1.0.0")),
			withComponentLabels(stringLabel("tier", "platform"))),
		newDescriptor("acme.org/chart", "1.0.0",
			withResources(resources...),
			withComponentLabels(stringLabel("tier", "leaf"))),
	}
}

func TestProjectByResourcesReferencePathAndResourceIdentity(t *testing.T) {
	r := require.New(t)
	descriptors := nestedGraph(
		newResource("image", withResourceExtras(map[string]string{"architecture": "amd64"})),
		newResource("image", withResourceExtras(map[string]string{"architecture": "arm64"})),
	)

	// The component selector drops the intermediate platform component; the
	// path through it must survive because it is computed on the full graph.
	p := projectGraph(t, &v1alpha1.DiscoverySpec{
		ComponentSelector: &v1alpha1.Selector{MatchLabels: map[string]string{"tier": "leaf"}},
		Extract: &v1alpha1.Extract{ByResources: map[string]string{
			"referencePath": `referencePath`,
			"resource":      `resourceIdentity`,
			"leaf":          `referencePath[size(referencePath) - 1].name`,
		}},
	}, descriptors...)

	r.Len(p.Extracted, 2)
	r.JSONEq(`[{"name":"platform"},{"name":"chart"}]`, mustJSON(t, p.Extracted[0]["referencePath"]))
	r.JSONEq(`{"name":"image","architecture":"amd64"}`, mustJSON(t, p.Extracted[0]["resource"]))
	r.JSONEq(`{"name":"image","architecture":"arm64"}`, mustJSON(t, p.Extracted[1]["resource"]))
	r.Equal("chart", p.Extracted[0]["leaf"], "typed bindings support list and map access")
}

func TestProjectByComponentsReferencePath(t *testing.T) {
	r := require.New(t)
	p := projectGraph(t, specWithExtract(&v1alpha1.Extract{ByComponents: map[string]string{
		"name": `component.name`,
		"path": `referencePath`,
	}}), nestedGraph()...)

	r.Len(p.Extracted, 3)
	got := map[string]string{}
	for _, record := range p.Extracted {
		got[record["name"].(string)] = mustJSON(t, record["path"])
	}
	r.JSONEq(`[]`, got["acme.org/root"], "the root has an empty, not an absent, path")
	r.JSONEq(`[{"name":"platform"}]`, got["acme.org/platform"])
	r.JSONEq(`[{"name":"platform"},{"name":"chart"}]`, got["acme.org/chart"])
}

func TestProjectWithoutRootOmitsReferencePath(t *testing.T) {
	r := require.New(t)
	p := projectForSpec(t, specWithExtract(&v1alpha1.Extract{ByResources: map[string]string{
		"name": `resource.name`,
		"path": `referencePath`,
	}}), newDescriptor("comp", "1.0.0", withResources(newResource("one"))))

	r.Len(p.Extracted, 1)
	r.Equal(map[string]any{"name": "one"}, p.Extracted[0], "an unknown path is a missing access")
}

func TestCompileRejectsReferenceBindingMisuse(t *testing.T) {
	tests := []struct {
		name    string
		extract *v1alpha1.Extract
	}{
		{name: "expression mode has no referencePath", extract: &v1alpha1.Extract{Expression: `[{"p": referencePath}]`}},
		{name: "byComponents has no resourceIdentity", extract: &v1alpha1.Extract{ByComponents: map[string]string{"r": `resourceIdentity`}}},
		{name: "referencePath is a list", extract: &v1alpha1.Extract{ByResources: map[string]string{"p": `referencePath.name`}}},
		{name: "resourceIdentity values are strings", extract: &v1alpha1.Extract{ByResources: map[string]string{"p": `resourceIdentity.name + 1`}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			_, err := Compile(t.Context(), specWithExtract(tt.extract))
			r.Error(err)
			var extractErr *ExtractError
			r.ErrorAs(err, &extractErr)
		})
	}
}

// TestProjectedRecordResolvesLikeResourceController feeds projected records
// through the same identity matching the Resource controller applies to
// spec.resource.byReference (ocm.ResolveReferencePath walks references with
// ocm.IdentityFuncIgnoreVersion, taking the first match, then the resource is
// matched the same way), so the bindings are proven usable verbatim in a
// Resource.
func TestProjectedRecordResolvesLikeResourceController(t *testing.T) {
	versioned := func(name, version string) descriptor.Resource {
		res := newResource(name)
		res.Version = version
		return res
	}

	versionGraph := func() []*descriptor.Descriptor {
		return []*descriptor.Descriptor{
			newDescriptor("root", "1.0.0", withReferences(
				newReference("lib", "libfips", "1.0.0", withRefExtras(map[string]string{"flavor": "fips"})),
				newReference("lib", "lib", "2.0.0"),
				newReference("lib", "lib", "1.0.0"),
				newReference("tool", "tool", "1.0.0"),
			), withResources(newResource("cfg"))),
			newDescriptor("lib", "1.0.0", withResources(newResource("image"))),
			newDescriptor("lib", "2.0.0", withResources(versioned("chart", "2.0.0"), versioned("chart", "1.0.0"))),
			newDescriptor("libfips", "1.0.0", withResources(newResource("image"))),
			newDescriptor("tool", "1.0.0",
				withReferences(newReference("dep", "lib", "1.0.0")),
				withResources(newResource("bin"))),
		}
	}

	tests := []struct {
		name        string
		descriptors []*descriptor.Descriptor
		// selector is an optional resource selector. The Resource controller
		// always matches against all resources of a component, so a selected
		// subset must not make a needed version look unnecessary.
		selector    *v1alpha1.Selector
		wantRecords int // 0 means one record per resource
		// wantOmitted counts records without resourceIdentity because no
		// identity selects their resource alone.
		wantOmitted int
	}{
		{
			name: "resources that differ only in extra identity",
			descriptors: nestedGraph(
				newResource("image", withResourceExtras(map[string]string{"architecture": "amd64"})),
				newResource("image", withResourceExtras(map[string]string{"architecture": "arm64"})),
			),
		},
		{
			// The references are ordered so that matching on name alone, or
			// on name and extra identity alone, would pick the wrong one.
			name:        "references and resources that differ only in version",
			descriptors: versionGraph(),
		},
		{
			name: "resource selector keeps the later of two versions",
			descriptors: []*descriptor.Descriptor{
				newDescriptor("root", "1.0.0", withResources(versioned("chart", "1.0.0"), versioned("chart", "2.0.0"))),
			},
			selector:    &v1alpha1.Selector{MatchIdentity: map[string]string{"name": "chart", "version": "2.0.0"}},
			wantRecords: 1,
		},
		{
			// A resource without a version has no version attribute in its
			// identity, so its full identity also matches a versioned sibling.
			name: "resource without version next to a versioned sibling",
			descriptors: []*descriptor.Descriptor{
				newDescriptor("root", "1.0.0", withResources(versioned("chart", ""), versioned("chart", "1.0.0"))),
			},
			wantOmitted: 1,
		},
		{
			name:        "resource selector keeps one version in a nested component",
			descriptors: versionGraph(),
			selector:    &v1alpha1.Selector{MatchIdentity: map[string]string{"name": "chart", "version": "1.0.0"}},
			wantRecords: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			byKey := keyed(tt.descriptors...)
			want := tt.wantRecords
			if want == 0 {
				for _, d := range tt.descriptors {
					want += len(d.Component.Resources)
				}
			}

			p := projectGraph(t, &v1alpha1.DiscoverySpec{
				ResourceSelector: tt.selector,
				Extract: &v1alpha1.Extract{ByResources: map[string]string{
					"referencePath": `referencePath`,
					"resource":      `resourceIdentity`,
					"component":     `component.name + ":" + component.version`,
					"full":          `resource`,
				}},
			}, tt.descriptors...)
			r.Len(p.Extracted, want)

			omitted := 0
			for _, record := range p.Extracted {
				var ref v1alpha1.ResourceReference
				r.NoError(json.Unmarshal([]byte(mustJSON(t, record)), &ref, runtime.JSONOptions))

				current := tt.descriptors[0]
				for _, hop := range ref.ReferencePath {
					var next *descriptor.Reference
					for j := range current.Component.References {
						if hop.Match(current.Component.References[j].ToIdentity(), ocm.IdentityFuncIgnoreVersion()) {
							next = &current.Component.References[j]
							break
						}
					}
					r.NotNil(next, "hop %v must match a reference of %s", hop, current.Component.Name)
					current = byKey[ComponentKey{Name: next.Component, Version: next.Version}]
					r.NotNil(current)
				}
				r.Equal(record["component"], current.Component.Name+":"+current.Component.Version,
					"the path must lead to the record's component")
				if _, ok := record["resource"]; !ok {
					omitted++
					continue
				}

				var matched []*descriptor.Resource
				for j := range current.Component.Resources {
					if ref.Resource.Match(current.Component.Resources[j].ToIdentity(), ocm.IdentityFuncIgnoreVersion()) {
						matched = append(matched, &current.Component.Resources[j])
					}
				}
				r.Len(matched, 1, "identity %v must select exactly one resource", ref.Resource)
				full := record["full"].(map[string]any)
				r.Equal(full["name"], matched[0].Name)
				r.Equal(full["version"], matched[0].Version)
				if extras, ok := full["extraIdentity"].(map[string]any); ok {
					for k, v := range extras {
						r.Equal(v, matched[0].ExtraIdentity[k])
					}
				}
			}
			r.Equal(tt.wantOmitted, omitted, "records without resourceIdentity")
		})
	}
}

// TestBindingsOnlyWhenRead pins that a binding is only computed when an
// expression reads it.
func TestBindingsOnlyWhenRead(t *testing.T) {
	tests := []struct {
		name      string
		extract   *v1alpha1.Extract
		wantReads map[string]bool
	}{
		{
			name:      "neither binding",
			extract:   &v1alpha1.Extract{ByResources: map[string]string{"n": `resource.name`}},
			wantReads: map[string]bool{},
		},
		{
			name:      "referencePath in a nested expression",
			extract:   &v1alpha1.Extract{ByComponents: map[string]string{"n": `size(referencePath) > 0 ? "nested" : "root"`}},
			wantReads: map[string]bool{bindingReferencePath: true},
		},
		{
			name: "both bindings in different fields",
			extract: &v1alpha1.Extract{ByResources: map[string]string{
				"p": `referencePath`,
				"r": `resourceIdentity.name`,
			}},
			wantReads: map[string]bool{bindingReferencePath: true, bindingResourceIdentity: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			q, err := Compile(t.Context(), specWithExtract(tt.extract))
			r.NoError(err)
			for _, binding := range []string{bindingReferencePath, bindingResourceIdentity} {
				r.Equal(tt.wantReads[binding], q.extract.reads[binding], binding)
			}
		})
	}
}

// TestUnaddressableElementsOmitBindings pins that an element no identity
// addresses on its own gets no binding: the field is omitted, the record is
// kept, and the rest of the Discovery is unaffected.
func TestUnaddressableElementsOmitBindings(t *testing.T) {
	spec := specWithExtract(&v1alpha1.Extract{ByResources: map[string]string{
		"component": `component.name`,
		"resource":  `resource.name`,
		"path":      `referencePath`,
		"identity":  `resourceIdentity`,
	}})

	t.Run("component unreachable from the root", func(t *testing.T) {
		r := require.New(t)
		p := projectGraph(t, spec,
			newDescriptor("root", "1.0.0", withResources(newResource("cfg"))),
			newDescriptor("unreachable", "1.0.0", withResources(newResource("image"))),
		)
		r.JSONEq(`[
			{"component":"root","resource":"cfg","path":[],"identity":{"name":"cfg"}},
			{"component":"unreachable","resource":"image","identity":{"name":"image"}}
		]`, mustJSON(t, p.Extracted))
	})

	t.Run("references with equal identities", func(t *testing.T) {
		r := require.New(t)
		// Both references are x@1.0.0; the Resource controller would resolve
		// either one to compA. Neither component gets a path through them,
		// compB keeps one through its unambiguous second reference.
		p := projectGraph(t, spec,
			newDescriptor("root", "1.0.0", withReferences(
				newReference("x", "compA", "1.0.0"),
				newReference("x", "compB", "1.0.0"),
				newReference("y", "compB", "1.0.0"),
			)),
			newDescriptor("compA", "1.0.0", withResources(newResource("ra"))),
			newDescriptor("compB", "1.0.0", withResources(newResource("rb"))),
		)
		r.JSONEq(`[
			{"component":"compA","resource":"ra","identity":{"name":"ra"}},
			{"component":"compB","resource":"rb","path":[{"name":"y"}],"identity":{"name":"rb"}}
		]`, mustJSON(t, p.Extracted))
	})

	t.Run("resources with equal identities", func(t *testing.T) {
		r := require.New(t)
		p := projectGraph(t, spec,
			newDescriptor("root", "1.0.0", withResources(
				newResource("twin", withResourceLabels(stringLabel("n", "1"))),
				newResource("twin", withResourceLabels(stringLabel("n", "2"))),
				newResource("single"),
			)),
		)
		r.JSONEq(`[
			{"component":"root","resource":"twin","path":[]},
			{"component":"root","resource":"twin","path":[]},
			{"component":"root","resource":"single","path":[],"identity":{"name":"single"}}
		]`, mustJSON(t, p.Extracted))
	})
}

// collidingReferenceGraph builds a diamond whose two first-hop reference
// elements render to the same runtime.Identity.String() ("aa=x,bb=y,name=image")
// yet are different identities: left carries the single extra attribute
// {"aa":"x,bb=y"}, right carries {"aa":"x","bb":"y"}. Both reach a common
// target, left via reference "z" and right via reference "a". Because the two
// prefixes serialise equally, the smallest complete element sequence is the one
// ending in "a" (the right branch), so the target's path must be
// [{aa=x,bb=y,name=image},{name=a}] regardless of reference order. The target
// also references a tail component, which must inherit that chosen prefix.
// Descriptors are returned root-first, all at version 1.0.0.
func collidingReferenceGraph() []*descriptor.Descriptor {
	return []*descriptor.Descriptor{
		newDescriptor("example.org/root", "1.0.0", withReferences(
			newReference("image", "example.org/left", "1.0.0", withRefExtras(map[string]string{"aa": "x,bb=y"})),
			newReference("image", "example.org/right", "1.0.0", withRefExtras(map[string]string{"aa": "x", "bb": "y"})),
		)),
		newDescriptor("example.org/left", "1.0.0", withReferences(newReference("z", "example.org/target", "1.0.0"))),
		newDescriptor("example.org/right", "1.0.0", withReferences(newReference("a", "example.org/target", "1.0.0"))),
		newDescriptor("example.org/target", "1.0.0",
			withResources(newResource("payload")),
			withReferences(newReference("tail", "example.org/tail", "1.0.0"))),
		newDescriptor("example.org/tail", "1.0.0"),
	}
}

func TestReferencePathsCollidingIdentityStrings(t *testing.T) {
	// Precondition: the two first-hop versionless elements really do collide on
	// String() yet are unequal maps. If this ever stops holding the fixture no
	// longer exercises the collision and the test is meaningless.
	left := collidingReferenceGraph()[0].Component.References[0].ToIdentity()
	right := collidingReferenceGraph()[0].Component.References[1].ToIdentity()
	delete(left, descriptor.IdentityAttributeVersion)
	delete(right, descriptor.IdentityAttributeVersion)
	require.Equal(t, left.String(), right.String(), "precondition: the first-hop elements must collide on String()")
	require.NotEqual(t, left, right, "precondition: the first-hop elements must be different identities")

	// The right branch ("a") wins over the left branch ("z") because the
	// serialised prefixes tie and "a" < "z".
	chosenFirstHop := runtime.Identity{"aa": "x", "bb": "y", "name": "image"}

	tests := []struct {
		name    string
		nested  bool
		reverse bool
	}{
		{name: "root collision, original order"},
		{name: "root collision, reversed order", reverse: true},
		{name: "collision below an extra root, original order", nested: true},
		{name: "collision below an extra root, reversed order", nested: true, reverse: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			descriptors := collidingReferenceGraph()
			if tt.reverse {
				slices.Reverse(descriptors[0].Component.References)
			}

			var prefix []runtime.Identity
			rootKey := key("example.org/root", "1.0.0")
			if tt.nested {
				outer := newDescriptor("example.org/outer", "1.0.0",
					withReferences(newReference("entry", "example.org/root", "1.0.0")))
				descriptors = append([]*descriptor.Descriptor{outer}, descriptors...)
				rootKey = key("example.org/outer", "1.0.0")
				prefix = []runtime.Identity{{"name": "entry"}}
			}

			paths := referencePaths(rootKey, keyed(descriptors...))

			wantTarget := append(append([]runtime.Identity{}, prefix...), chosenFirstHop, runtime.Identity{"name": "a"})
			r.Equal(wantTarget, paths[key("example.org/target", "1.0.0")], "target takes the smallest complete path")

			wantTail := append(append([]runtime.Identity{}, wantTarget...), runtime.Identity{"name": "tail"})
			r.Equal(wantTail, paths[key("example.org/tail", "1.0.0")], "tail inherits the chosen prefix")
		})
	}
}

func TestProjectByResourcesCollidingIdentityStrings(t *testing.T) {
	tests := []struct {
		name    string
		reverse bool
	}{
		{name: "original order"},
		{name: "reversed order", reverse: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			descriptors := collidingReferenceGraph()
			if tt.reverse {
				slices.Reverse(descriptors[0].Component.References)
			}
			p := projectGraph(t, &v1alpha1.DiscoverySpec{
				Extract: &v1alpha1.Extract{ByResources: map[string]string{
					"referencePath": `referencePath`,
					"resource":      `resourceIdentity`,
				}},
			}, descriptors...)
			r.JSONEq(
				`[{"referencePath":[{"aa":"x","bb":"y","name":"image"},{"name":"a"}],"resource":{"name":"payload"}}]`,
				mustJSON(t, p.Extracted))
		})
	}
}
