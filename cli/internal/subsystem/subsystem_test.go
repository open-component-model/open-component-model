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
		Description: "A subsystem for testing",
		Scheme:      scheme,
	}

	registry := NewRegistry()
	registry.Register(s)

	assert.Equal(t, s, registry.Get("test-subsystem"))
	assert.Nil(t, registry.Get("non-existent"))
	assert.ElementsMatch(t, []*Subsystem{s}, registry.List())
}

func TestRegistryListSorted(t *testing.T) {
	registry := NewRegistry()

	// Register subsystems in reverse alphabetical order
	registry.Register(NewSubsystem("zebra", "Z subsystem"))
	registry.Register(NewSubsystem("apple", "A subsystem"))
	registry.Register(NewSubsystem("middle", "M subsystem"))

	// List should return them sorted by name
	list := registry.List()
	assert.Len(t, list, 3)
	assert.Equal(t, "apple", list[0].Name)
	assert.Equal(t, "middle", list[1].Name)
	assert.Equal(t, "zebra", list[2].Name)
}

func TestNewSubsystem(t *testing.T) {
	s := NewSubsystem("test-name", "test description")

	assert.Equal(t, "test-name", s.Name)
	assert.Equal(t, "test description", s.Description)
	assert.NotNil(t, s.Scheme)
}
