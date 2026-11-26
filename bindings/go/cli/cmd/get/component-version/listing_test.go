package componentversion

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime/versioning"
)

func TestEffectiveDetailLevel(t *testing.T) {
	tests := []struct {
		name  string
		sel   detailSelection
		want  string
		error bool
	}{
		{name: "no flags defaults to names", sel: detailSelection{}, want: detailNames},
		{name: "latest implies versions", sel: detailSelection{latestSet: true}, want: detailVersions},
		{name: "constraint implies versions", sel: detailSelection{constraintSet: true}, want: detailVersions},
		{name: "recursive implies full", sel: detailSelection{recursive: -1}, want: detailFull},
		{name: "tree output implies full", sel: detailSelection{output: "tree", outputSet: true}, want: detailFull},
		{name: "widetree output implies full", sel: detailSelection{output: "widetree", outputSet: true}, want: detailFull},
		{name: "explicit table output stays names", sel: detailSelection{output: "table", outputSet: true}, want: detailNames},
		{name: "unset tree default stays names", sel: detailSelection{output: "tree", outputSet: false}, want: detailNames},
		{name: "recursive wins over latest", sel: detailSelection{latestSet: true, recursive: -1}, want: detailFull},

		{name: "explicit names", sel: detailSelection{detailSet: true, detail: detailNames}, want: detailNames},
		{name: "explicit versions", sel: detailSelection{detailSet: true, detail: detailVersions}, want: detailVersions},
		{name: "explicit full", sel: detailSelection{detailSet: true, detail: detailFull}, want: detailFull},
		{name: "explicit full with latest", sel: detailSelection{detailSet: true, detail: detailFull, latestSet: true}, want: detailFull},

		{name: "names with latest errors", sel: detailSelection{detailSet: true, detail: detailNames, latestSet: true}, error: true},
		{name: "names with constraint errors", sel: detailSelection{detailSet: true, detail: detailNames, constraintSet: true}, error: true},
		{name: "names with recursive errors", sel: detailSelection{detailSet: true, detail: detailNames, recursive: -1}, error: true},
		{name: "names with tree errors", sel: detailSelection{detailSet: true, detail: detailNames, output: "tree", outputSet: true}, error: true},
		{name: "versions with recursive errors", sel: detailSelection{detailSet: true, detail: detailVersions, recursive: -1}, error: true},
		{name: "versions with tree errors", sel: detailSelection{detailSet: true, detail: detailVersions, output: "tree", outputSet: true}, error: true},
		{name: "versions with latest is fine", sel: detailSelection{detailSet: true, detail: detailVersions, latestSet: true}, want: detailVersions},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := effectiveDetailLevel(tt.sel)
			if tt.error {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRenderNames(t *testing.T) {
	names := []string{"ocm.software/a", "ocm.software/b"}
	for _, format := range []string{"table", "json", "yaml", "ndjson"} {
		t.Run(format, func(t *testing.T) {
			var buf strings.Builder
			require.NoError(t, renderNames(&buf, format, names))
			out := buf.String()
			assert.Contains(t, out, "ocm.software/a")
			assert.Contains(t, out, "ocm.software/b")
			if format != "table" {
				assert.Contains(t, out, "component")
			}
		})
	}
}

func TestRenderVersions(t *testing.T) {
	entries := []componentVersionEntry{
		{Component: "ocm.software/a", Version: "1.0.0"},
		{Component: "ocm.software/a", Version: "0.9.0"},
	}
	for _, format := range []string{"table", "json", "yaml", "ndjson"} {
		t.Run(format, func(t *testing.T) {
			var buf strings.Builder
			require.NoError(t, renderVersions(&buf, format, entries))
			out := buf.String()
			assert.Contains(t, out, "ocm.software/a")
			assert.Contains(t, out, "1.0.0")
			assert.Contains(t, out, "0.9.0")
		})
	}
}

func TestCollectComponentVersions_SkipsUnreadableAndOrders(t *testing.T) {
	captureLogs(t)

	repoA := &fakeCVRepo{versions: []string{"1.9.0", "1.10.0"}}
	repoB := &fakeCVRepo{versions: []string{"2.0.0"}}
	resolver := &fakeResolver{
		repos: map[string]repository.ComponentVersionRepository{
			"comp/a": repoA,
			"comp/b": repoB,
		},
		errs: map[string]error{"no/access": &unauthorizedError{}},
	}

	entries, err := collectComponentVersions(t.Context(), resolver,
		[]string{"comp/b", "comp/a", "no/access"}, Params{}, versioning.Default())
	require.NoError(t, err)

	// Ordered by component name, then version newest first.
	require.Equal(t, []componentVersionEntry{
		{Component: "comp/a", Version: "1.10.0"},
		{Component: "comp/a", Version: "1.9.0"},
		{Component: "comp/b", Version: "2.0.0"},
	}, entries)

	// The versions level must not fetch descriptors.
	assert.False(t, repoA.getCalled, "versions level must not call GetComponentVersion")
	assert.False(t, repoB.getCalled, "versions level must not call GetComponentVersion")
}

func TestCollectComponentVersions_AllFail(t *testing.T) {
	captureLogs(t)
	resolver := &fakeResolver{errs: map[string]error{
		"a": &unauthorizedError{},
		"b": &unauthorizedError{},
	}}
	_, err := collectComponentVersions(t.Context(), resolver, []string{"a", "b"}, Params{}, versioning.Default())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "a:")
	assert.Contains(t, err.Error(), "b:")
}
