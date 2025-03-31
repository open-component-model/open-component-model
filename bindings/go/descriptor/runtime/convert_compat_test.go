package runtime_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptorRuntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// CompatibilityTestAccessType implements runtime.Typed for testing
type CompatibilityTestAccessType struct {
	Type runtime.Type `json:"type"`
	Data string       `json:"data"`
}

func (t *CompatibilityTestAccessType) GetType() runtime.Type {
	return t.Type
}

func (t *CompatibilityTestAccessType) SetType(typ runtime.Type) {
	t.Type = typ
}

func (t *CompatibilityTestAccessType) DeepCopyTyped() runtime.Typed {
	return &CompatibilityTestAccessType{
		Type: t.Type,
		Data: t.Data,
	}
}

var _ runtime.Typed = &CompatibilityTestAccessType{}

// TestCompatibilityConvert tests the behavior of the CompatibilityConvert method which handles legacy type conversions
// in component descriptors. The method:
//
// 1. Takes a scheme that defines the set of known types
// 2. Takes a descriptor containing resources and sources with access types
// 3. Takes conversion options that map legacy types to their new equivalents
//
// The conversion process:
// - For each resource and source in the descriptor:
//   - If the access type is not registered in the scheme, look up its legacy type mapping
//   - If a mapping exists, convert the type to its new equivalent
//   - If no mapping exists, return an error
//
// - Collect all conversion errors and return them as a combined error
//
// The test cases verify:
// - Successful conversion of resource access types
// - Successful conversion of source access types
// - Successful conversion of multiple types
// - Error handling for unknown legacy types
func TestCompatibilityConvert(t *testing.T) {
	scheme := runtime.NewScheme()
	legacyType := runtime.Type{
		Name:    "legacyType",
		Version: "v1",
	}
	newType := runtime.Type{
		Name:    "newType",
		Version: "v1",
	}

	// Register the new type in the scheme
	scheme.MustRegisterWithAlias(&CompatibilityTestAccessType{}, newType)

	opts := &descriptorRuntime.CompatibilityConversionOptions{
		KnownLegacyTypes: map[runtime.Type]runtime.Type{
			legacyType: newType,
		},
	}

	tests := []struct {
		name        string
		descriptor  *descriptorRuntime.Descriptor
		expectError bool
		errorMsg    string
	}{
		{
			name: "successful conversion of resource access type",
			descriptor: &descriptorRuntime.Descriptor{
				Component: descriptorRuntime.Component{
					Resources: []descriptorRuntime.Resource{
						{
							Access: &CompatibilityTestAccessType{
								Type: legacyType,
								Data: "test-data",
							},
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "successful conversion of source access type",
			descriptor: &descriptorRuntime.Descriptor{
				Component: descriptorRuntime.Component{
					Sources: []descriptorRuntime.Source{
						{
							Access: &CompatibilityTestAccessType{
								Type: legacyType,
								Data: "test-data",
							},
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "successful conversion of multiple types",
			descriptor: &descriptorRuntime.Descriptor{
				Component: descriptorRuntime.Component{
					Resources: []descriptorRuntime.Resource{
						{
							Access: &CompatibilityTestAccessType{
								Type: legacyType,
								Data: "test-data",
							},
						},
					},
					Sources: []descriptorRuntime.Source{
						{
							Access: &CompatibilityTestAccessType{
								Type: legacyType,
								Data: "test-data",
							},
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "unknown legacy type",
			descriptor: &descriptorRuntime.Descriptor{
				Component: descriptorRuntime.Component{
					Resources: []descriptorRuntime.Resource{
						{
							Access: &CompatibilityTestAccessType{
								Type: runtime.Type{
									Name:    "unknownType",
									Version: "v1",
								},
								Data: "test-data",
							},
						},
					},
				},
			},
			expectError: true,
			errorMsg:    "type \"unknownType/v1\" is not registered and no known legacy type found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := descriptorRuntime.CompatibilityConvert(scheme, tt.descriptor, opts)
			if tt.expectError {
				assert.Error(t, err)
				assert.ErrorContains(t, err, tt.errorMsg)
				return
			}
			require.NoError(t, err)

			// Verify conversions
			for _, resource := range tt.descriptor.Component.Resources {
				if access, ok := resource.Access.(*CompatibilityTestAccessType); ok {
					assert.Equal(t, newType, access.Type)
					assert.Equal(t, "test-data", access.Data)
				}
			}
			for _, source := range tt.descriptor.Component.Sources {
				if access, ok := source.Access.(*CompatibilityTestAccessType); ok {
					assert.Equal(t, newType, access.Type)
					assert.Equal(t, "test-data", access.Data)
				}
			}
		})
	}
}

func TestCompatibilityConvertWithMultipleErrors(t *testing.T) {
	scheme := runtime.NewScheme()
	opts := &descriptorRuntime.CompatibilityConversionOptions{
		KnownLegacyTypes: map[runtime.Type]runtime.Type{},
	}

	descriptor := &descriptorRuntime.Descriptor{
		Component: descriptorRuntime.Component{
			Resources: []descriptorRuntime.Resource{
				{
					Access: &CompatibilityTestAccessType{
						Type: runtime.Type{
							Name:    "unknownType1",
							Version: "v1",
						},
						Data: "test-data",
					},
				},
				{
					Access: &CompatibilityTestAccessType{
						Type: runtime.Type{
							Name:    "unknownType2",
							Version: "v1",
						},
						Data: "test-data",
					},
				},
			},
		},
	}

	err := descriptorRuntime.CompatibilityConvert(scheme, descriptor, opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "resource access type at resource index 0 could not be converted")
	assert.Contains(t, err.Error(), "resource access type at resource index 1 could not be converted")
}

// TestCompatibilityConvertE2E tests the end-to-end conversion flow with type tracking.
// It verifies that:
// 1. A v2 descriptor can be converted to a runtime descriptor
// 2. Legacy types in the runtime descriptor can be converted to new types
// 3. The runtime descriptor can be converted back to v2
// 4. The conversion process tracks which types were changed
func TestCompatibilityConvertE2E(t *testing.T) {
	scheme := runtime.NewScheme()

	// Define legacy and new types
	legacyType := runtime.Type{
		Name: "localBlob",
	}
	newType := runtime.Type{
		Name:    "LocalBlob",
		Version: "v1",
		Group:   "software.ocm.accessType",
	}

	// Register the new type in the scheme
	scheme.MustRegisterWithAlias(&descriptorRuntime.LocalBlob{}, newType)

	// Create a v2 descriptor with a legacy access type
	v2Desc := &descriptorv2.Descriptor{
		Meta: descriptorv2.Meta{
			Version: "v2",
		},
		Component: descriptorv2.Component{
			ComponentMeta: descriptorv2.ComponentMeta{
				ObjectMeta: descriptorv2.ObjectMeta{
					Name:    "test-component",
					Version: "1.0.0",
				},
			},
			Provider: "test-provider",
			Resources: []descriptorv2.Resource{
				{
					ElementMeta: descriptorv2.ElementMeta{
						ObjectMeta: descriptorv2.ObjectMeta{
							Name:    "test-resource",
							Version: "1.0.0",
						},
					},
					Type:     "test-type",
					Relation: descriptorv2.LocalRelation,
					Access: &runtime.Raw{
						Type: legacyType,
						Data: []byte(`{"type":"localBlob","localReference":"sha256:1234","mediaType":"application/octet-stream"}`),
					},
				},
			},
		},
	}

	// Convert v2 to runtime descriptor
	runtimeDesc, err := descriptorRuntime.ConvertFromV2(v2Desc)
	require.NoError(t, err)

	// Convert legacy types
	opts := &descriptorRuntime.CompatibilityConversionOptions{
		KnownLegacyTypes: map[runtime.Type]runtime.Type{
			legacyType: newType,
		},
	}
	err = descriptorRuntime.CompatibilityConvert(scheme, runtimeDesc, opts)
	require.NoError(t, err)

	// Verify the conversion in the runtime descriptor
	require.Len(t, runtimeDesc.Component.Resources, 1)
	resource := runtimeDesc.Component.Resources[0]
	require.NotNil(t, resource.Access)
	access, ok := resource.Access.(*descriptorRuntime.LocalBlob)
	require.True(t, ok)
	assert.Equal(t, newType, access.Type)
	assert.Equal(t, "sha256:1234", access.LocalReference)
	assert.Equal(t, "application/octet-stream", access.MediaType)

	// Convert back to v2
	v2DescNew, err := descriptorRuntime.ConvertToV2(scheme, runtimeDesc)
	require.NoError(t, err)

	// Verify the conversion back to v2
	require.Len(t, v2DescNew.Component.Resources, 1)
	v2Resource := v2DescNew.Component.Resources[0]
	require.NotNil(t, v2Resource.Access)
	assert.Equal(t, newType, v2Resource.Access.Type)
	rawData := v2Resource.Access.Data
	assert.Contains(t, string(rawData), `"localReference":"sha256:1234"`)
	assert.Contains(t, string(rawData), `"mediaType":"application/octet-stream"`)
}
