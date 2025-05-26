package constructor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// MockResourceInputMethod is a mock implementation of ResourceInputMethod
type MockResourceInputMethod struct{}

func (m *MockResourceInputMethod) GetCredentialConsumerIdentity(ctx context.Context, resource *constructorv1.Resource) (runtime.Identity, error) {
	return runtime.Identity{}, nil
}

func (m *MockResourceInputMethod) ProcessResource(ctx context.Context, resource *constructorv1.Resource, credentials map[string]string) (*ResourceInputMethodResult, error) {
	return &ResourceInputMethodResult{
		ProcessedResource: &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    resource.Name,
					Version: resource.Version,
				},
			},
			Type: resource.Type,
		},
	}, nil
}

// MockSourceInputMethod is a mock implementation of SourceInputMethod
type MockSourceInputMethod struct{}

func (m *MockSourceInputMethod) GetCredentialConsumerIdentity(ctx context.Context, source *constructorv1.Source) (runtime.Identity, error) {
	return runtime.Identity{}, nil
}

func (m *MockSourceInputMethod) ProcessSource(ctx context.Context, source *constructorv1.Source, credentials map[string]string) (*SourceInputMethodResult, error) {
	return &SourceInputMethodResult{
		ProcessedSource: &descriptor.Source{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    source.Name,
					Version: source.Version,
				},
			},
			Type: source.Type,
		},
	}, nil
}

// MockTyped is a mock implementation of runtime.Typed
type MockTyped struct {
	Type runtime.Type `json:"type"`
}

func (m *MockTyped) GetType() runtime.Type {
	return m.Type
}

func (m *MockTyped) SetType(t runtime.Type) {
	m.Type = t
}

func (m *MockTyped) DeepCopyTyped() runtime.Typed {
	return &MockTyped{
		Type: m.Type,
	}
}

func TestNewInputMethodRegistry(t *testing.T) {
	scheme := runtime.NewScheme()
	registry := New(scheme)

	assert.NotNil(t, registry)
	assert.NotNil(t, registry.resourceMethods)
	assert.NotNil(t, registry.scheme)
}

func TestRegisterAndGetResourceInputMethod(t *testing.T) {
	scheme := runtime.NewScheme()
	scheme.MustRegisterWithAlias(&MockTyped{}, runtime.Type{Name: "test-type", Version: "v1"})

	registry := New(scheme)

	mockMethod := &MockResourceInputMethod{}
	mockTyped := &MockTyped{
		Type: runtime.Type{Name: "test-type", Version: "v1"},
	}

	// Register the method
	registry.MustRegisterResourceInputMethod(mockTyped, mockMethod)

	// Create a resource with input
	resource := &constructorv1.Resource{
		ElementMeta: constructorv1.ElementMeta{
			ObjectMeta: constructorv1.ObjectMeta{
				Name:    "test-resource",
				Version: "v1.0.0",
			},
		},
		Type: "test-type",
		AccessOrInput: constructorv1.AccessOrInput{
			Input: &runtime.Raw{
				Type: runtime.Type{Name: "test-type", Version: "v1"},
			},
		},
	}

	// Get the method
	method, err := registry.GetResourceInputMethod(context.Background(), resource)
	require.NoError(t, err)
	assert.NotNil(t, method)

	// Test error case - nil resource
	_, err = registry.GetResourceInputMethod(context.Background(), nil)
	assert.Error(t, err)

	// Test error case - resource without input
	emptyResource := &constructorv1.Resource{}
	_, err = registry.GetResourceInputMethod(context.Background(), emptyResource)
	assert.Error(t, err)
}

func TestRegisterAndGetSourceInputMethod(t *testing.T) {
	scheme := runtime.NewScheme()
	scheme.MustRegisterWithAlias(&MockTyped{}, runtime.Type{Name: "test-type", Version: "v1"})
	registry := New(scheme)

	mockMethod := &MockSourceInputMethod{}
	mockTyped := &MockTyped{
		Type: runtime.Type{Name: "test-type", Version: "v1"},
	}

	// Register the method
	registry.MustRegisterSourceInputMethod(mockTyped, mockMethod)

	// Create a source with input
	source := &constructorv1.Source{
		ElementMeta: constructorv1.ElementMeta{
			ObjectMeta: constructorv1.ObjectMeta{
				Name:    "test-source",
				Version: "v1.0.0",
			},
		},
		Type: "test-type",
		AccessOrInput: constructorv1.AccessOrInput{
			Input: &runtime.Raw{
				Type: runtime.Type{Name: "test-type", Version: "v1"},
			},
		},
	}

	// Get the method
	method, err := registry.GetSourceInputMethod(context.Background(), source)
	require.NoError(t, err)
	assert.NotNil(t, method)

	// Test error case - nil source
	_, err = registry.GetSourceInputMethod(context.Background(), nil)
	assert.Error(t, err)

	// Test error case - source without input
	emptySource := &constructorv1.Source{}
	_, err = registry.GetSourceInputMethod(context.Background(), emptySource)
	assert.Error(t, err)
}
