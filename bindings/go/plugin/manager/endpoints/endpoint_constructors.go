package endpoints

import (
	"encoding/json"
	"net/http"

	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Handler contains the handling function, the location and the Schema for wrapping function calls with.
// For example, a real function passed in from the plugin is wrapper into an HTTP HandlerFunc to be called
// later. This handler will use the Schema in order to verify any access spec information that is passed in.
type Handler struct {
	Handler  http.HandlerFunc
	Location string
}

// EndpointBuilder constructs a capability for the plugin. Register*Endpoint will keep updating
// an internal tracker. Once all capabilities have been declared, we call Marshal to
// return the registered capabilities to the plugin manager.
type EndpointBuilder struct {
	CurrentTypes types.Types
	Handlers     []Handler
	Scheme       *runtime.Scheme
}

// NewEndpoints constructs a new builder for registering capabilities for the given plugin type.
func NewEndpoints(scheme *runtime.Scheme) *EndpointBuilder {
	return &EndpointBuilder{
		Scheme: scheme,
	}
}

// MarshalJSON returns the accumulated endpoints during Register* calls.
func (c *EndpointBuilder) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.CurrentTypes)
}

// GetHandlers returns all the Handlers that this plugin implemented during the registration of a capability.
func (c *EndpointBuilder) GetHandlers() []Handler {
	return c.Handlers
}
