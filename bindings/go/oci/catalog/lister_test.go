package catalog

import (
	"context"
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
}

var _ RepositoryCataloger = (*fakeCataloger)(nil)

func (f *fakeCataloger) Repositories(ctx context.Context, last string, fn func(repos []string) error) error {
	f.last = last
	for _, page := range f.pages {
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
