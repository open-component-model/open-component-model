package componentlister

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/componentlister/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestComponentListerPluginConverter_ListComponents(t *testing.T) {
	t.Run("single page response", func(t *testing.T) {
		mockPlugin := &mockConverterPlugin{
			responses: []v1.ListComponentsResponse{
				{List: []string{"component1", "component2"}},
			},
		}

		converter := &componentListerPluginConverter{
			externalPlugin: mockPlugin,
		}

		var collectedNames []string
		err := converter.ListComponents(t.Context(), "", func(names []string) error {
			collectedNames = append(collectedNames, names...)
			return nil
		})

		assert.NoError(t, err)
		assert.Equal(t, []string{"component1", "component2"}, collectedNames)
		assert.Equal(t, 1, len(mockPlugin.requests))
		assert.Equal(t, "", mockPlugin.requests[0].Last)
	})

	t.Run("multiple page response with pagination", func(t *testing.T) {
		mockPlugin := &mockConverterPlugin{
			responses: []v1.ListComponentsResponse{
				{
					List:   []string{"component1", "component2"},
					Header: &v1.ListComponentsResponseHeader{Last: "component2"},
				},
				{
					List:   []string{"component3", "component4"},
					Header: &v1.ListComponentsResponseHeader{Last: "component4"},
				},
				{List: []string{"component5"}},
			},
		}

		converter := &componentListerPluginConverter{
			externalPlugin: mockPlugin,
		}

		var collectedNames []string
		err := converter.ListComponents(t.Context(), "", func(names []string) error {
			collectedNames = append(collectedNames, names...)
			return nil
		})

		assert.NoError(t, err)
		assert.Equal(t, []string{"component1", "component2", "component3", "component4", "component5"}, collectedNames)
		assert.Equal(t, 3, len(mockPlugin.requests))
		assert.Equal(t, "", mockPlugin.requests[0].Last)
		assert.Equal(t, "component2", mockPlugin.requests[1].Last)
		assert.Equal(t, "component4", mockPlugin.requests[2].Last)
	})

	t.Run("external plugin error", func(t *testing.T) {
		mockPlugin := &mockConverterPlugin{
			err: assert.AnError,
		}

		converter := &componentListerPluginConverter{
			externalPlugin: mockPlugin,
		}

		err := converter.ListComponents(t.Context(), "", func(names []string) error {
			return nil
		})

		assert.Error(t, err)
		expectedErr := assert.AnError.Error()
		assert.Truef(t, strings.Contains(err.Error(), expectedErr), "returned error '%s' does not contain expected '%s'", err.Error(), expectedErr)
	})

	t.Run("callback function error", func(t *testing.T) {
		mockPlugin := &mockConverterPlugin{
			responses: []v1.ListComponentsResponse{
				{List: []string{"component1", "component2"}},
			},
		}

		converter := &componentListerPluginConverter{
			externalPlugin: mockPlugin,
		}

		callbackErr := assert.AnError
		err := converter.ListComponents(t.Context(), "", func(names []string) error {
			return callbackErr
		})

		assert.Error(t, err)
		expectedErr := assert.AnError.Error()
		assert.Truef(t, strings.Contains(err.Error(), expectedErr), "returned error '%s' does not contain expected '%s'", err.Error(), expectedErr)
	})

	t.Run("empty response", func(t *testing.T) {
		mockPlugin := &mockConverterPlugin{
			responses: []v1.ListComponentsResponse{
				{List: []string{}},
			},
		}

		converter := &componentListerPluginConverter{
			externalPlugin: mockPlugin,
		}

		var callbackCalled bool
		err := converter.ListComponents(t.Context(), "", func(names []string) error {
			callbackCalled = true
			assert.Empty(t, names)
			return nil
		})

		assert.NoError(t, err)
		assert.True(t, callbackCalled)
	})
}

type mockConverterPlugin struct {
	responses []v1.ListComponentsResponse
	requests  []v1.ListComponentsRequest[runtime.Typed]
	err       error
	callCount int
}

var _ v1.ComponentListerPluginContract[runtime.Typed] = &mockConverterPlugin{}

func (m *mockConverterPlugin) ListComponents(ctx context.Context, request *v1.ListComponentsRequest[runtime.Typed], credentials map[string]string) (*v1.ListComponentsResponse, error) {
	m.requests = append(m.requests, *request)

	if m.err != nil {
		return nil, m.err
	}

	response := m.responses[m.callCount]
	m.callCount++
	return &response, nil
}

func (m *mockConverterPlugin) Ping(ctx context.Context) error {
	panic("not implemented")
}

func (m *mockConverterPlugin) GetIdentity(ctx context.Context, request *v1.GetIdentityRequest[runtime.Typed]) (*v1.GetIdentityResponse, error) {
	panic("not implemented")
}
