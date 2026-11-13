package ocm

import (
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// matches returns the indices of ids that query selects under
// IdentityFuncIgnoreVersion, the matcher of ResolveReferencePath and the
// Resource controller.
func matches(query runtime.Identity, ids []runtime.Identity) []int {
	var out []int
	for j, id := range ids {
		if query.Match(id.Clone(), IdentityFuncIgnoreVersion()) {
			out = append(out, j)
		}
	}
	return out
}

// requireAddressable pins AddressableIdentities to the real matcher: a non-nil
// identity selects exactly its own element, and nil is only returned when
// neither the versionless nor the full identity would.
func requireAddressable(t *testing.T, ids []runtime.Identity) {
	t.Helper()
	r := require.New(t)
	got := AddressableIdentities(ids)
	r.Len(got, len(ids))
	for i, id := range ids {
		if got[i] != nil {
			r.Equal([]int{i}, matches(got[i], ids), "identity %v of element %d of %v", got[i], i, ids)
			if _, ok := got[i][descriptor.IdentityAttributeVersion]; ok {
				r.NotEqual([]int{i}, matches(withoutVersion(id), ids),
					"element %d of %v keeps a version it does not need", i, ids)
			}
			continue
		}
		r.NotEqual([]int{i}, matches(withoutVersion(id), ids), "element %d of %v is addressable without version", i, ids)
		r.NotEqual([]int{i}, matches(id, ids), "element %d of %v is addressable by its full identity", i, ids)
	}
}

func TestAddressableIdentities(t *testing.T) {
	tests := []struct {
		name string
		ids  []runtime.Identity
		want []runtime.Identity
	}{
		{
			name: "unique names drop the version",
			ids:  []runtime.Identity{{"name": "a", "version": "1"}, {"name": "b", "version": "1"}},
			want: []runtime.Identity{{"name": "a"}, {"name": "b"}},
		},
		{
			name: "elements that differ only in version keep it",
			ids:  []runtime.Identity{{"name": "a", "version": "1"}, {"name": "a", "version": "2"}},
			want: []runtime.Identity{{"name": "a", "version": "1"}, {"name": "a", "version": "2"}},
		},
		{
			name: "equal identities are not addressable",
			ids:  []runtime.Identity{{"name": "a", "version": "1"}, {"name": "a", "version": "1"}, {"name": "b", "version": "1"}},
			want: []runtime.Identity{nil, nil, {"name": "b"}},
		},
		{
			name: "an element without version is not addressable next to a versioned sibling",
			ids:  []runtime.Identity{{"name": "a"}, {"name": "a", "version": "1"}},
			want: []runtime.Identity{nil, {"name": "a", "version": "1"}},
		},
		{
			name: "values with separators are compared exactly",
			ids:  []runtime.Identity{{"name": "a", "x": "1,y=2", "version": "1"}, {"name": "a", "x": "1", "y": "2", "version": "1"}},
			want: []runtime.Identity{{"name": "a", "x": "1,y=2"}, {"name": "a", "x": "1", "y": "2"}},
		},
		{
			name: "no elements",
			ids:  nil,
			want: []runtime.Identity{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, AddressableIdentities(tt.ids))
			requireAddressable(t, tt.ids)
		})
	}
}

// TestAddressableIdentitiesAgainstMatcher checks every list of up to three
// elements drawn from a small pool, covering duplicates, missing versions and
// extra identity.
func TestAddressableIdentitiesAgainstMatcher(t *testing.T) {
	var pool []runtime.Identity
	for _, name := range []string{"a", "b"} {
		for _, version := range []string{"", "1", "2"} {
			for _, extra := range []string{"", "x"} {
				id := runtime.Identity{"name": name}
				if version != "" {
					id["version"] = version
				}
				if extra != "" {
					id["flavor"] = extra
				}
				pool = append(pool, id)
			}
		}
	}

	var lists [][]runtime.Identity
	var build func(prefix []runtime.Identity)
	build = func(prefix []runtime.Identity) {
		if len(prefix) > 0 {
			lists = append(lists, prefix)
		}
		if len(prefix) == 3 {
			return
		}
		for _, id := range pool {
			build(append(append([]runtime.Identity(nil), prefix...), id))
		}
	}
	build(nil)

	for _, ids := range lists {
		requireAddressable(t, ids)
	}
	t.Logf("checked %d identity lists", len(lists))
}
