package discovery

import (
	"encoding/json"
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			r.Equal(tt.want, referencePaths(root, keyed(tt.descriptors...)))
		})
	}
}

func TestReferencePathsWithoutRoot(t *testing.T) {
	r := require.New(t)
	r.Nil(referencePaths(key("root", "1.0.0"), keyed(newDescriptor("other", "1.0.0"))))
	r.Nil(referencePaths(ComponentKey{}, keyed(newDescriptor("other", "1.0.0"))))
}

func TestVersionlessIdentity(t *testing.T) {
	r := require.New(t)
	in := []runtime.Identity{
		{"name": "a", "version": "1.0.0"},
		{"name": "b", "version": "1.0.0"},
		{"name": "b", "version": "2.0.0"},
		{"name": "b", "version": "1.0.0", "arch": "arm64"},
		nil,
	}
	needed := versionNeeded(in)
	var out []runtime.Identity
	for _, id := range in {
		out = append(out, versionlessIdentity(id, needed))
	}
	r.Equal([]runtime.Identity{
		{"name": "a"},
		{"name": "b", "version": "1.0.0"},
		{"name": "b", "version": "2.0.0"},
		{"name": "b", "arch": "arm64"},
		{},
	}, out)
	r.Equal(runtime.Identity{"name": "a", "version": "1.0.0"}, in[0], "the input must not be modified")
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
	raw, err := json.Marshal(v)
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

			for _, record := range p.Extracted {
				var ref v1alpha1.ResourceReference
				r.NoError(json.Unmarshal([]byte(mustJSON(t, record)), &ref))

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
		})
	}
}

// TestBindingsAreLazy pins that the bindings are only computed when an
// expression reads them. An unreachable component has no reference path:
// reading it is an extraction error, not reading it must not fail.
func TestBindingsAreLazy(t *testing.T) {
	r := require.New(t)
	descriptors := []*descriptor.Descriptor{
		newDescriptor("root", "1.0.0", withResources(newResource("cfg"))),
		newDescriptor("unreachable", "1.0.0", withResources(newResource("image"))),
	}
	graph := Graph{Root: ComponentKey{Name: "root", Version: "1.0.0"}, Descriptors: descriptors}

	unused, err := Compile(t.Context(), specWithExtract(&v1alpha1.Extract{ByResources: map[string]string{"n": `resource.name`}}))
	r.NoError(err)
	f, err := unused.Filter(t.Context(), graph)
	r.NoError(err)
	_, err = unused.Project(t.Context(), f)
	r.NoError(err, "an unread binding must not be computed")

	used, err := Compile(t.Context(), specWithExtract(&v1alpha1.Extract{ByResources: map[string]string{"p": `referencePath`}}))
	r.NoError(err)
	f, err = used.Filter(t.Context(), graph)
	r.NoError(err)
	_, err = used.Project(t.Context(), f)
	var extractErr *ExtractError
	r.ErrorAs(err, &extractErr)
	r.ErrorContains(err, "not reachable from the root component")
}
