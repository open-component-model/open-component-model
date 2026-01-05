package subsystem

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestRegistry(t *testing.T) {
	scheme := runtime.NewScheme()
	s := &Subsystem{
		Name:        "test-subsystem",
		Title:       "Test Subsystem",
		Description: "A subsystem for testing",
		Scheme:      scheme,
		Guides: []Guide{
			{
				Title:   "Test Guide",
				Summary: "A guide for testing",
				Sections: []Section{
					{
						Title:   "Test Section",
						Content: "This is a test section",
						Example: &Example{
							Caption:  "Test Example",
							Language: "yaml",
							Content:  "test: example",
						},
					},
				},
			},
		},
	}

	registry := NewRegistry()
	registry.Register(s)

	assert.Equal(t, s, registry.Get("test-subsystem"))
	assert.Nil(t, registry.Get("non-existent"))
	assert.ElementsMatch(t, []*Subsystem{s}, registry.List())
}

func TestGlobalRegistry(t *testing.T) {
	s := &Subsystem{Name: "global-test"}
	Register(s)
	assert.Equal(t, s, Get("global-test"))
	assert.Contains(t, List(), s)
}
