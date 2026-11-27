package catalog

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	repo "ocm.software/open-component-model/bindings/go/repository"
)

// fakeCataloger is a RepositoryCataloger that returns preconfigured pages of
// raw repository names. Each element of pages is one catalog page.
type fakeCataloger struct {
	pages [][]string
	// last records the last cursor the lister passed in.
	last string
	// served counts how many pages were handed to the callback, so a test can
	// assert that the scan stopped before requesting a later page.
	served int
}

var _ RepositoryCataloger = (*fakeCataloger)(nil)

func (f *fakeCataloger) Repositories(ctx context.Context, last string, fn func(repos []string) error) error {
	f.last = last
	for _, page := range f.pages {
		f.served++
		if err := fn(page); err != nil {
			return err
		}
	}
	return nil
}

func TestComponentLister_ListComponents(t *testing.T) {
	tests := []struct {
		name     string
		subPath  string
		pages    [][]string
		expected []string
	}{
		{
			name: "keeps component-descriptors, skips repos before the block and stops after it",
			pages: [][]string{{
				"aaa/before-the-block",
				"component-descriptors/componentA",
				"component-descriptors/componentB",
				"component-descriptors/componentC",
				"not-a-component",
			}},
			expected: []string{"componentA", "componentB", "componentC"},
		},
		{
			name:     "single component",
			pages:    [][]string{{"component-descriptors/componentA"}},
			expected: []string{"componentA"},
		},
		{
			name:     "empty catalog yields empty result",
			pages:    [][]string{{}},
			expected: []string{},
		},
		{
			name:     "no component-descriptors yields empty result",
			pages:    [][]string{{"not-a-component", "not-a-component-again"}},
			expected: []string{},
		},
		{
			name:     "overlapping component names",
			pages:    [][]string{{"component-descriptors/foo/bar", "component-descriptors/foo/bar/baz"}},
			expected: []string{"foo/bar", "foo/bar/baz"},
		},
		{
			name:    "subPath prefix is stripped and other prefixes are skipped",
			subPath: "my/sub",
			pages: [][]string{{
				"component-descriptors/componentB",
				"my/sub/component-descriptors/componentA",
				"my/sub/component-descriptors/componentC",
				"other/component-descriptors/componentD",
			}},
			expected: []string{"componentA", "componentC"},
		},
		{
			name: "matches accumulate across pages",
			pages: [][]string{
				{"component-descriptors/componentA", "component-descriptors/componentB"},
				{"component-descriptors/componentC", "component-descriptors/componentD"},
			},
			expected: []string{"componentA", "componentB", "componentC", "componentD"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cataloger := &fakeCataloger{pages: tt.pages}
			var lister repo.ComponentLister = NewComponentLister(cataloger, tt.subPath)

			result := []string{}
			err := lister.ListComponents(t.Context(), "", func(names []string) error {
				result = append(result, names...)
				return nil
			})

			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestComponentLister_StopsScanAfterTheBlock(t *testing.T) {
	// A page that sorts entirely after the block must stop the scan, so a later
	// page is never requested.
	cataloger := &fakeCataloger{pages: [][]string{
		{"component-descriptors/componentA", "zzz-after-the-block"},
		{"component-descriptors/componentB"},
	}}
	lister := NewComponentLister(cataloger, "")

	var names []string
	require.NoError(t, lister.ListComponents(t.Context(), "", func(found []string) error {
		names = append(names, found...)
		return nil
	}))

	// componentB from the second page is not listed because the scan stopped.
	assert.Equal(t, []string{"componentA"}, names)
	assert.Equal(t, 1, cataloger.served, "the second page must not be requested")
}

func TestComponentLister_UnsortedCatalogFallsBackToFullScan(t *testing.T) {
	// The first page is not byte-sorted ("helm/chart" sorts after a later
	// component-descriptors entry), so early stopping is turned off and every
	// page is scanned. No match may be dropped.
	cataloger := &fakeCataloger{pages: [][]string{
		{"component-descriptors/a", "helm/chart", "component-descriptors/b"},
		{"component-descriptors/c"},
	}}
	lister := NewComponentLister(cataloger, "")

	var names []string
	require.NoError(t, lister.ListComponents(t.Context(), "", func(found []string) error {
		names = append(names, found...)
		return nil
	}))

	assert.Equal(t, []string{"a", "b", "c"}, names)
	assert.Equal(t, 2, cataloger.served, "every page must be scanned when a page is unsorted")
}

func TestComponentLister_BlockEndsExactlyAtPageEnd(t *testing.T) {
	// The block ends exactly at the end of the first page, so the next page is
	// requested and proves the block is over. All matches are returned.
	cataloger := &fakeCataloger{pages: [][]string{
		{"component-descriptors/a", "component-descriptors/b"},
		{"zzz-after-the-block"},
	}}
	lister := NewComponentLister(cataloger, "")

	var names []string
	require.NoError(t, lister.ListComponents(t.Context(), "", func(found []string) error {
		names = append(names, found...)
		return nil
	}))

	assert.Equal(t, []string{"a", "b"}, names)
	assert.Equal(t, 2, cataloger.served)
}

func TestComponentLister_SinglePageWithEverything(t *testing.T) {
	// A cataloger that ignores last and returns the whole catalog in one sorted
	// page must still produce the correct, duplicate-free result.
	cataloger := &fakeCataloger{pages: [][]string{{
		"aaa/before-the-block",
		"component-descriptors/a",
		"component-descriptors/b",
		"zzz-after-the-block",
	}}}
	lister := NewComponentLister(cataloger, "")

	var names []string
	require.NoError(t, lister.ListComponents(t.Context(), "", func(found []string) error {
		names = append(names, found...)
		return nil
	}))

	assert.Equal(t, []string{"a", "b"}, names)
	assert.Equal(t, 1, cataloger.served)
}

func TestComponentLister_ListComponentsFnNil(t *testing.T) {
	cataloger := &fakeCataloger{}
	var lister repo.ComponentLister = NewComponentLister(cataloger, "")
	err := lister.ListComponents(t.Context(), "", nil)
	assert.EqualError(t, err, ErrFnNil.Error())
}

func TestComponentLister_StartCursorBeginsAtThePrefix(t *testing.T) {
	tests := []struct {
		name     string
		subPath  string
		last     string
		expected string
	}{
		{
			name:     "empty last starts at the prefix",
			last:     "",
			expected: "component-descriptors/",
		},
		{
			name:     "component cursor continues after the component",
			last:     "componentA",
			expected: "component-descriptors/componentA",
		},
		{
			name:     "subPath is included in the cursor",
			subPath:  "my/sub",
			last:     "componentA",
			expected: "my/sub/component-descriptors/componentA",
		},
		{
			name:     "empty last with subPath starts at the subPath prefix",
			subPath:  "my/sub",
			last:     "",
			expected: "my/sub/component-descriptors/",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cataloger := &fakeCataloger{pages: [][]string{{}}}
			lister := NewComponentLister(cataloger, tt.subPath)
			err := lister.ListComponents(t.Context(), tt.last, func(names []string) error { return nil })
			require.NoError(t, err)
			assert.Equal(t, tt.expected, cataloger.last)
		})
	}
}

// endlessCataloger returns non-matching pages forever (ignoring last) until the
// callback stops it, to exercise the scan page cap.
type endlessCataloger struct{ served int }

func (e *endlessCataloger) Repositories(ctx context.Context, last string, fn func(repos []string) error) error {
	for {
		e.served++
		if err := fn([]string{fmt.Sprintf("other/repo-%d", e.served)}); err != nil {
			return err
		}
	}
}

func TestComponentLister_OwnerFirstOrderStopsAfterBlock(t *testing.T) {
	// The pages are in a registry's owner-first order, which is not byte sorted
	// ("open-contracting/x" before "open-contracting-automator/y"), but the
	// matching repositories are contiguous, so early stopping still works.
	cataloger := &fakeCataloger{pages: [][]string{
		{"open-contracting/x", "open-contracting-automator/y"},
		{"org/component-descriptors/a", "org/component-descriptors/b"},
		{"zzz/after-the-block"},
	}}
	lister := NewComponentLister(cataloger, "org")

	var names []string
	require.NoError(t, lister.ListComponents(t.Context(), "", func(found []string) error {
		names = append(names, found...)
		return nil
	}))

	assert.Equal(t, []string{"a", "b"}, names)
	assert.Equal(t, 3, cataloger.served, "scan must stop on the page that ends the block")
}

func TestComponentLister_NonContiguousBlockFullScan(t *testing.T) {
	// A match appears after a non-match, so the block is not contiguous. No
	// match may be dropped and the scan continues through every page.
	cataloger := &fakeCataloger{pages: [][]string{
		{"org/component-descriptors/a", "other/x", "org/component-descriptors/b"},
		{"org/component-descriptors/c"},
	}}
	lister := NewComponentLister(cataloger, "org")

	var names []string
	require.NoError(t, lister.ListComponents(t.Context(), "", func(found []string) error {
		names = append(names, found...)
		return nil
	}))

	assert.Equal(t, []string{"a", "b", "c"}, names)
	assert.Equal(t, 2, cataloger.served)
}

func TestComponentLister_ScanLimitWhenNothingMatches(t *testing.T) {
	cataloger := &endlessCataloger{}
	lister := NewComponentLister(cataloger, "org", WithScanPageLimit(3))

	err := lister.ListComponents(t.Context(), "", func(found []string) error { return nil })
	require.ErrorIs(t, err, ErrCatalogScanLimit)
	assert.Equal(t, 3, cataloger.served, "scan must stop at the page cap")
}

func TestComponentLister_BlockFoundWhenCatalogerIgnoresLast(t *testing.T) {
	// The fake cataloger ignores last and returns from the top; the block is
	// still found as long as it is within the cap.
	cataloger := &fakeCataloger{pages: [][]string{
		{"aaa/before"},
		{"org/component-descriptors/a"},
		{"zzz/after"},
	}}
	lister := NewComponentLister(cataloger, "org", WithScanPageLimit(10))

	var names []string
	require.NoError(t, lister.ListComponents(t.Context(), "", func(found []string) error {
		names = append(names, found...)
		return nil
	}))
	assert.Equal(t, []string{"a"}, names)
}
