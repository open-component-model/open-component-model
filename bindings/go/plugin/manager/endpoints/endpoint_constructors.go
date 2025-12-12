package endpoints

import (
	"encoding/json"
	"fmt"
	"net/http"

	pluginruntime "ocm.software/open-component-model/bindings/go/plugin/manager/types/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Handler contains the handling function and location wrapping function calls with.
// For example, a real function passed in from the plugin is wrapper into an HTTP HandlerFunc to be called
// later with the appropriate parameters extracted from the http.Request.
type Handler struct {
	Handler  http.HandlerFunc
	Location string
}

// EndpointBuilder constructs types for the plugin. Any Register functions that live in their respective package
// will take this builder as a parameter. The function then, will keep updating a tracker that collects all types and
// schemes declared by the plugin. Once all types have been declared, we call Marshal to return the registered types as
// JSON to the plugin manager.
type EndpointBuilder struct {
	PluginSpec pluginruntime.PluginSpec
	Handlers   []Handler
	Scheme     *runtime.Scheme
}

// NewEndpoints constructs a new builder for registering capabilities for the given plugin type.
func NewEndpoints(scheme *runtime.Scheme) *EndpointBuilder {
	return &EndpointBuilder{
		Scheme: scheme,
	}
}

// MarshalJSON returns the accumulated endpoints during Register* calls.
func (c *EndpointBuilder) MarshalJSON() ([]byte, error) {
	pluginSpec, err := pluginruntime.ConvertToSpec(&c.PluginSpec)
	if err != nil {
		return nil, fmt.Errorf("failed to convert to spec: %w", err)
	}
	content, err := json.Marshal(pluginSpec)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal capabilities: %w", err)
	}
	return content, nil
}

// GetHandlers returns all the Handlers that this plugin implemented during the registration of a capability.
func (c *EndpointBuilder) GetHandlers() []Handler {
	return c.Handlers
}

// AddConfigType adds a configuration type to the list of supported config types.
func (c *EndpointBuilder) AddConfigType(typ ...runtime.Type) {
	c.PluginSpec.SupportedConfigTypes = append(c.PluginSpec.SupportedConfigTypes, typ...)
}
