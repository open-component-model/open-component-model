package constructor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// MockResourceInputMethod is a mock implementation of ResourceInputMethod
type MockResourceInputMethod struct{}

func (m *MockResourceInputMethod) GetInputMethodScheme() *runtime.Scheme {
	return runtime.NewScheme()
}

func (m *MockResourceInputMethod) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *constructorruntime.Resource) (identity runtime.Identity, err error) {
	return runtime.Identity{}, nil
}

func (m *MockResourceInputMethod) ProcessResource(ctx context.Context, resource *constructorruntime.Resource, credentials map[string]string) (*ResourceInputMethodResult, error) {
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

func (m *MockSourceInputMethod) GetInputMethodScheme() *runtime.Scheme {
	return runtime.NewScheme()
}

func (m *MockSourceInputMethod) GetSourceCredentialConsumerIdentity(ctx context.Context, source *constructorruntime.Source) (identity runtime.Identity, err error) {
	return runtime.Identity{}, nil
}

func (m *MockSourceInputMethod) ProcessSource(ctx context.Context, source *constructorruntime.Source, credentials map[string]string) (*SourceInputMethodResult, error) {
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
	resource := &constructorruntime.Resource{
		ElementMeta: constructorruntime.ElementMeta{
			ObjectMeta: constructorruntime.ObjectMeta{
				Name:    "test-resource",
				Version: "v1.0.0",
			},
		},
		Type: "test-type",
		AccessOrInput: constructorruntime.AccessOrInput{
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
	emptyResource := &constructorruntime.Resource{}
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
	source := &constructorruntime.Source{
		ElementMeta: constructorruntime.ElementMeta{
			ObjectMeta: constructorruntime.ObjectMeta{
				Name:    "test-source",
				Version: "v1.0.0",
			},
		},
		Type: "test-type",
		AccessOrInput: constructorruntime.AccessOrInput{
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
	emptySource := &constructorruntime.Source{}
	_, err = registry.GetSourceInputMethod(context.Background(), emptySource)
	assert.Error(t, err)
}
