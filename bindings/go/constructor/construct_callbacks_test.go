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

// mockInputType implements runtime.Typed for testing
type mockInputType struct {
	Type runtime.Type
}

func (m *mockInputType) GetType() runtime.Type {
	return m.Type
}

func (m *mockInputType) SetType(typ runtime.Type) {
	m.Type = typ
}

func (m *mockInputType) DeepCopyTyped() runtime.Typed {
	return &mockInputType{
		Type: m.Type,
	}
}

// mockCallbackTracker helps track which callbacks were called and in what order
type mockCallbackTracker struct {
	startComponentCalled bool
	endComponentCalled   bool
	startResourceCalled  bool
	endResourceCalled    bool
	startSourceCalled    bool
	endSourceCalled      bool
	component            *constructorruntime.Component
	resource             *constructorruntime.Resource
	source               *constructorruntime.Source
	descriptor           *descriptor.Descriptor
	err                  error
}

func (m *mockCallbackTracker) reset() {
	m.startComponentCalled = false
	m.endComponentCalled = false
	m.startResourceCalled = false
	m.endResourceCalled = false
	m.startSourceCalled = false
	m.endSourceCalled = false
	m.component = nil
	m.resource = nil
	m.source = nil
	m.descriptor = nil
	m.err = nil
}

func TestConstructionCallbacks(t *testing.T) {
	tracker := &mockCallbackTracker{}
	mockRepo := newMockTargetRepository()

	// Create a simple component with one resource and one source
	component := &constructorruntime.Component{
		ComponentMeta: constructorruntime.ComponentMeta{
			ObjectMeta: constructorruntime.ObjectMeta{
				Name:    "test-component",
				Version: "v1.0.0",
			},
		},
		Resources: []constructorruntime.Resource{
			{
				ElementMeta: constructorruntime.ElementMeta{
					ObjectMeta: constructorruntime.ObjectMeta{
						Name:    "test-resource",
						Version: "v1.0.0",
					},
				},
				Type: "test",
				AccessOrInput: constructorruntime.AccessOrInput{
					Input: &mockInputType{
						Type: runtime.NewVersionedType("mock", "v1"),
					},
				},
			},
		},
		Sources: []constructorruntime.Source{
			{
				ElementMeta: constructorruntime.ElementMeta{
					ObjectMeta: constructorruntime.ObjectMeta{
						Name:    "test-source",
						Version: "v1.0.0",
					},
				},
				Type: "test",
				AccessOrInput: constructorruntime.AccessOrInput{
					Input: &mockInputType{
						Type: runtime.NewVersionedType("mock", "v1"),
					},
				},
			},
		},
	}

	constructor := &constructorruntime.ComponentConstructor{
		Components: []constructorruntime.Component{*component},
	}

	// Create mock input methods
	mockSourceInput := &mockSourceInputMethod{
		processedSource: &descriptor.Source{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-source",
					Version: "v1.0.0",
				},
			},
			Access: &descriptor.LocalBlob{
				MediaType: "application/octet-stream",
			},
		},
	}
	mockResourceInput := &mockInputMethod{
		processedResource: &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-resource",
					Version: "v1.0.0",
				},
			},
			Access: &descriptor.LocalBlob{
				MediaType: "application/octet-stream",
			},
		},
	}

	// Create the constructor with our callback tracker
	opts := Options{
		TargetRepositoryProvider: &mockTargetRepositoryProvider{repo: mockRepo},
		ResourceInputMethodProvider: &mockInputMethodProvider{
			methods: map[runtime.Type]ResourceInputMethod{
				runtime.NewVersionedType("mock", "v1"): mockResourceInput,
			},
		},
		SourceInputMethodProvider: &mockSourceInputMethodProvider{
			methods: map[runtime.Type]SourceInputMethod{
				runtime.NewVersionedType("mock", "v1"): mockSourceInput,
			},
		},
		ComponentConstructionCallbacks: ComponentConstructionCallbacks{
			OnStartComponentConstruct: func(ctx context.Context, component *constructorruntime.Component) error {
				tracker.startComponentCalled = true
				tracker.component = component
				return nil
			},
			OnEndComponentConstruct: func(ctx context.Context, desc *descriptor.Descriptor, err error) error {
				tracker.endComponentCalled = true
				tracker.descriptor = desc
				tracker.err = err
				return nil
			},
			OnStartResourceConstruct: func(ctx context.Context, resource *constructorruntime.Resource) error {
				tracker.startResourceCalled = true
				tracker.resource = resource
				return nil
			},
			OnEndResourceConstruct: func(ctx context.Context, resource *descriptor.Resource, err error) error {
				tracker.endResourceCalled = true
				return nil
			},
			OnStartSourceConstruct: func(ctx context.Context, source *constructorruntime.Source) error {
				tracker.startSourceCalled = true
				tracker.source = source
				return nil
			},
			OnEndSourceConstruct: func(ctx context.Context, source *descriptor.Source, err error) error {
				tracker.endSourceCalled = true
				return nil
			},
		},
	}

	constructorInstance := NewDefaultConstructor(constructor, opts)
	graph := constructorInstance.GetGraph()
	
	// Process the constructor
	err := constructorInstance.Construct(context.Background())
	require.NoError(t, err)
	descs := collectDescriptors(t, graph)
	require.Len(t, descs, 1)

	// Verify all callbacks were called
	assert.True(t, tracker.startComponentCalled, "OnStartComponentConstruct should have been called")
	assert.True(t, tracker.endComponentCalled, "OnEndComponentConstruct should have been called")
	assert.True(t, tracker.startResourceCalled, "OnStartResourceConstruct should have been called")
	assert.True(t, tracker.endResourceCalled, "OnEndResourceConstruct should have been called")
	assert.True(t, tracker.startSourceCalled, "OnStartSourceConstruct should have been called")
	assert.True(t, tracker.endSourceCalled, "OnEndSourceConstruct should have been called")

	// Verify the component passed to callbacks
	assert.Equal(t, component.Name, tracker.component.Name)
	assert.Equal(t, component.Version, tracker.component.Version)

	// Verify the resource passed to callbacks
	assert.Equal(t, component.Resources[0].Name, tracker.resource.Name)
	assert.Equal(t, component.Resources[0].Version, tracker.resource.Version)

	// Verify the source passed to callbacks
	assert.Equal(t, component.Sources[0].Name, tracker.source.Name)
	assert.Equal(t, component.Sources[0].Version, tracker.source.Version)

	// Verify the descriptor passed to end component callback
	assert.Equal(t, component.Name, tracker.descriptor.Component.Name)
	assert.Equal(t, component.Version, tracker.descriptor.Component.Version)
	assert.Nil(t, tracker.err, "No error should have been passed to OnEndComponentConstruct")
}
