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
	singlePage := []string{
		"component-descriptors/componentC",
		"component-descriptors/componentB",
		"not-a-component",
		"component-descriptors/duplicate",
		"component-descriptors/componentD",
		"component-descriptors/componentA",
		"component-descriptors/duplicate",
		"not-a-component-again",
	}

	tests := []struct {
		name     string
		subPath  string
		pages    [][]string
		expected []string
	}{
		{
			name:     "keeps component-descriptors and strips the prefix",
			pages:    [][]string{singlePage},
			expected: []string{"componentA", "componentB", "componentC", "componentD", "duplicate"},
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
			name:    "subPath prefix is stripped",
			subPath: "my/sub",
			pages: [][]string{{
				"my/sub/component-descriptors/componentA",
				"component-descriptors/componentB",
				"my/sub/component-descriptors/componentC",
				"other/component-descriptors/componentD",
			}},
			expected: []string{"componentA", "componentC"},
		},
		{
			name: "pages are sorted and deduped per page",
			pages: [][]string{
				{"component-descriptors/componentB", "component-descriptors/componentA"},
				{"component-descriptors/componentD", "component-descriptors/componentC"},
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

func TestComponentLister_ListComponentsFnNil(t *testing.T) {
	cataloger := &fakeCataloger{}
	var lister repo.ComponentLister = NewComponentLister(cataloger, "")
	err := lister.ListComponents(t.Context(), "", nil)
	assert.EqualError(t, err, ErrFnNil.Error())
}

func TestComponentLister_LastCursorIsMappedToRepositoryName(t *testing.T) {
	tests := []struct {
		name     string
		subPath  string
		last     string
		expected string
	}{
		{
			name:     "empty last stays empty",
			last:     "",
			expected: "",
		},
		{
			name:     "component cursor maps to component-descriptors repository",
			last:     "componentA",
			expected: "component-descriptors/componentA",
		},
		{
			name:     "subPath is included in the cursor",
			subPath:  "my/sub",
			last:     "componentA",
			expected: "my/sub/component-descriptors/componentA",
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
