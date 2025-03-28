package oci

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"oras.land/oras-go/v2/registry/remote"
)

func TestNewURLPathResolver(t *testing.T) {
	baseURL := "http://example.com"
	resolver := NewURLPathResolver(baseURL)
	assert.NotNil(t, resolver)
	assert.Equal(t, baseURL, resolver.BaseURL)
}

func TestURLPathResolver_SetClient(t *testing.T) {
	resolver := NewURLPathResolver("http://example.com")
	repo, err := remote.NewRepository("example.com/test")
	assert.NoError(t, err)

	// Set the client
	resolver.SetClient(repo.Client)

	// Verify the client was set by using it
	store, err := resolver.StoreForReference(context.Background(), "example.com/test")
	assert.NoError(t, err)
	assert.NotNil(t, store)
}

func TestURLPathResolver_BaseReference(t *testing.T) {
	resolver := NewURLPathResolver("http://example.com")
	component := "test-component"
	expected := "http://example.com/component-descriptors/test-component"
	result := resolver.BaseReference(component)
	assert.Equal(t, expected, result)
}

func TestURLPathResolver_ComponentVersionReference(t *testing.T) {
	resolver := NewURLPathResolver("http://example.com")
	component := "test-component"
	version := "v1.0.0"
	expected := "http://example.com/component-descriptors/test-component:v1.0.0"
	result := resolver.ComponentVersionReference(component, version)
	assert.Equal(t, expected, result)
}

func TestURLPathResolver_StoreForReference(t *testing.T) {
	tests := []struct {
		name        string
		reference   string
		expectError bool
	}{
		{
			name:        "valid reference",
			reference:   "example.com/test-component:v1.0.0",
			expectError: false,
		},
		{
			name:        "invalid reference",
			reference:   "invalid:reference",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := NewURLPathResolver("http://example.com")
			store, err := resolver.StoreForReference(context.Background(), tt.reference)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, store)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, store)
		})
	}
}
