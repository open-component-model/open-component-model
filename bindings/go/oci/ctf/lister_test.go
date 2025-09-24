package oci

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/ctf"
	"ocm.software/open-component-model/bindings/go/ctf/index/v1"
	repo "ocm.software/open-component-model/bindings/go/repository"
)

// MockCTF is a mock implementation of the CTF interface
type MockCTF struct {
	// idx contains test data.
	idx v1.Index
}

var _ ctf.CTF = (*MockCTF)(nil)

func TestListComponents(t *testing.T) {
	ctx := t.Context()
	testData := []string{
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
		sort     bool
		pageSize int
		last     string
		input    []string
		expected []string
	}{
		{
			name:     "default behavior - store order preserved",
			input:    testData,
			expected: []string{"componentC", "componentB", "duplicate", "componentD", "componentA"},
		},
		{
			name:     "NameListPageSize option should be ignored",
			pageSize: 2,
			input:    testData,
			expected: []string{"componentC", "componentB", "duplicate", "componentD", "componentA"},
		},
		{
			name:     "last parameter should be ignored",
			last:     "2",
			input:    testData,
			expected: []string{"componentC", "componentB", "duplicate", "componentD", "componentA"},
		},
		{
			name:     "single component in the store - one result",
			input:    []string{"component-descriptors/componentA"},
			expected: []string{"componentA"},
		},
		{
			name:     "empty store - empty result",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "store does not contain components - empty result",
			input:    []string{"not-a-component", "not-a-component-again"},
			expected: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock CTF store with the test data.
			archive := NewMockCTF(tt.input)

			// Set listing options.
			var opts []ComponentListerOption
			if tt.pageSize > 0 {
				opts = append(opts, WithPageSize(tt.pageSize))
			}

			// Create an instance of the CTFComponentLister.
			var lister repo.ComponentLister
			lister, _ = NewComponentLister(archive, opts...)

			// Collect the returned component names.
			result := []string{}
			err := lister.ListComponents(ctx, tt.last, func(names []string) error {
				result = append(result, names...)
				return nil
			})

			assert.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestWithPageSize(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected int
	}{
		{"page size equals one", 1, 1},
		{"page size equals ten", 10, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a ComponentListerOptions instance
			opts := &ComponentListerOptions{}

			// Apply the option function
			optionFunc := WithPageSize(tt.input)
			optionFunc(opts)

			// Verify the field was set correctly
			if opts.NameListPageSize != tt.expected {
				t.Errorf("Expected NameListPageSize to be %v, got %v", tt.expected, opts.NameListPageSize)
			}
		})
	}
}

// NewMockCTF creates a new empty mock CTF.
func NewMockCTF(compNames []string) *MockCTF {
	m := &MockCTF{}
	m.idx = v1.NewIndex()

	for _, r := range compNames {
		a := v1.ArtifactMetadata{
			Repository: r,
			Tag:        "v1",
			Digest:     "sha256:abc",
			MediaType:  "type1",
		}
		m.idx.AddArtifact(a)
	}

	return m
}

func (m *MockCTF) Format() ctf.FileFormat {
	var format ctf.FileFormat
	return format
}

func (m *MockCTF) GetIndex(ctx context.Context) (v1.Index, error) {
	return m.idx, nil
}

func (m *MockCTF) SetIndex(ctx context.Context, index v1.Index) error {
	return nil
}

func (m *MockCTF) ListBlobs(ctx context.Context) ([]string, error) {
	return []string{}, nil
}

func (m *MockCTF) GetBlob(ctx context.Context, digest string) (blob.ReadOnlyBlob, error) {
	var b blob.ReadOnlyBlob
	return b, nil
}

func (m *MockCTF) SaveBlob(ctx context.Context, blob blob.ReadOnlyBlob) error {
	return nil
}

func (m *MockCTF) DeleteBlob(ctx context.Context, digest string) error {
	return nil
}
