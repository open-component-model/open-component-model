package resource

import (
	"encoding/json"
	"fmt"
	"net/http"

	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/resource/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// GetIdentity provides the identity of a type supported by the plugin.
	GetIdentity = "/identity"
	// GetGlobalResource defines the endpoint to get a global resource.
	GetGlobalResource = "/resource/get"
	// AddGlobalResource defines the endpoint to add a global resource.
	AddGlobalResource = "/resource/add"
)

// handleError writes an error response to the http.ResponseWriter
func handleError(w http.ResponseWriter, err error, status int, message string) {
	http.Error(w, fmt.Sprintf("%s: %v", message, err), status)
}

// handleJSONResponse encodes the response as JSON and writes it to the http.ResponseWriter
func handleJSONResponse(w http.ResponseWriter, response interface{}) {
	if err := json.NewEncoder(w).Encode(response); err != nil {
		handleError(w, err, http.StatusInternalServerError, "failed to encode response")
		return
	}
}

// handleGetIdentity handles the GetIdentity endpoint
func handleGetIdentity(plugin v1.ReadWriteResourcePluginContract) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request v1.GetIdentityRequest[runtime.Typed]
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			handleError(w, err, http.StatusBadRequest, "failed to unmarshal request")
			return
		}

		response, err := plugin.GetIdentity(r.Context(), &request)
		if err != nil {
			handleError(w, err, http.StatusInternalServerError, "failed to get identity")
			return
		}

		handleJSONResponse(w, response)
	}
}

// handleGetGlobalResource handles the GetGlobalResource endpoint
func handleGetGlobalResource(plugin v1.ReadWriteResourcePluginContract) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request v1.GetGlobalResourceRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			handleError(w, err, http.StatusBadRequest, "failed to unmarshal request")
			return
		}

		response, err := plugin.GetGlobalResource(r.Context(), &request, make(map[string]string))
		if err != nil {
			handleError(w, err, http.StatusInternalServerError, "failed to get global resource")
			return
		}

		handleJSONResponse(w, response)
	}
}

// handleAddGlobalResource handles the AddGlobalResource endpoint
func handleAddGlobalResource(plugin v1.ReadWriteResourcePluginContract) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request v1.AddGlobalResourceRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			handleError(w, err, http.StatusBadRequest, "failed to unmarshal request")
			return
		}

		response, err := plugin.AddGlobalResource(r.Context(), &request, make(map[string]string))
		if err != nil {
			handleError(w, err, http.StatusInternalServerError, "failed to add global resource")
			return
		}

		handleJSONResponse(w, response)
	}
}

// RegisterResourcePlugin registers the resource plugin endpoints with the endpoint builder.
// It sets up HTTP handlers for identity, get resource, and add resource operations.
func RegisterResourcePlugin[T runtime.Typed](
	proto T,
	plugin v1.ReadWriteResourcePluginContract,
	c *endpoints.EndpointBuilder,
) error {
	typ, err := c.Scheme.TypeForPrototype(proto)
	if err != nil {
		return fmt.Errorf("failed to get type for prototype %T: %w", proto, err)
	}

	// Register endpoints
	c.Handlers = append(c.Handlers,
		endpoints.Handler{
			Location: GetIdentity,
			Handler:  handleGetIdentity(plugin),
		},
		endpoints.Handler{
			Location: GetGlobalResource,
			Handler:  handleGetGlobalResource(plugin),
		},
		endpoints.Handler{
			Location: AddGlobalResource,
			Handler:  handleAddGlobalResource(plugin),
		},
	)

	schema, err := plugins.GenerateJSONSchemaForType(proto)
	if err != nil {
		return fmt.Errorf("failed to generate jsonschema for prototype %T: %w", proto, err)
	}

	// Add resource type to the plugin's types
	c.PluginSpec.CapabilitySpecs = append(c.PluginSpec.CapabilitySpecs, &v1.CapabilitySpec{
		Type: runtime.NewUnversionedType(string(v1.ResourceRepositoryPluginType)),
		SupportedAccessTypes: []types.Type{
			{
				Type:       typ,
				Aliases:    nil,
				JSONSchema: schema,
			},
		},
	})

	return nil
}
