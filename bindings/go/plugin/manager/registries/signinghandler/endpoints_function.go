package signinghandler

import (
	"encoding/json"
	"fmt"
	"net/http"

	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/signing/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// GetSignerIdentity provides the identity for a signer
	GetSignerIdentity = "/sign/identity"
	// GetVerifierIdentity provides the identity for a signer
	GetVerifierIdentity = "/verify/identity"
	// Sign defines the endpoint to sign content
	Sign = "/sign"
	// Verify defines the endpoint to verify content
	Verify = "/verify"
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

// handleGetSignerIdentity handles the GetSignerIdentity endpoint
func handleGetSignerIdentity[T runtime.Typed](plugin v1.SignerPluginContract[T]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request v1.GetSignerIdentityRequest[T]
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			handleError(w, err, http.StatusBadRequest, "failed to unmarshal request")
			return
		}

		response, err := plugin.GetSignerIdentity(r.Context(), &request)
		if err != nil {
			handleError(w, err, http.StatusInternalServerError, "failed to get identity")
			return
		}

		handleJSONResponse(w, response)
	}
}

// handleGetVerifierIdentity handles the GetVerifierIdentity endpoint
func handleGetVerifierIdentity[T runtime.Typed](plugin v1.VerifierPluginContract[T]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request v1.GetVerifierIdentityRequest[T]
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			handleError(w, err, http.StatusBadRequest, "failed to unmarshal request")
			return
		}

		response, err := plugin.GetVerifierIdentity(r.Context(), &request)
		if err != nil {
			handleError(w, err, http.StatusInternalServerError, "failed to get identity")
			return
		}

		handleJSONResponse(w, response)
	}
}

// handleSign handles the GetGlobalResource endpoint
func handleSign[T runtime.Typed](plugin v1.SignerPluginContract[T]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request v1.SignRequest[T]
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			handleError(w, err, http.StatusBadRequest, "failed to unmarshal request")
			return
		}

		response, err := plugin.Sign(r.Context(), &request, make(map[string]string))
		if err != nil {
			handleError(w, err, http.StatusInternalServerError, "failed to get global resource")
			return
		}

		handleJSONResponse(w, response)
	}
}

// handleVerify handles the Verify endpoint
func handleVerify[T runtime.Typed](plugin v1.VerifierPluginContract[T]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request v1.VerifyRequest[T]
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			handleError(w, err, http.StatusBadRequest, "failed to unmarshal request")
			return
		}

		response, err := plugin.Verify(r.Context(), &request, make(map[string]string))
		if err != nil {
			handleError(w, err, http.StatusInternalServerError, "failed to add global resource")
			return
		}

		handleJSONResponse(w, response)
	}
}

// RegisterPlugin registers the resource plugin endpoints with the endpoint builder.
// It sets up HTTP handlers for identity, sign and verify operations.
func RegisterPlugin[T runtime.Typed](
	proto T,
	plugin v1.SignatureHandlerContract[T],
	c *endpoints.EndpointBuilder,
) error {
	if c.CurrentTypes.Types == nil {
		c.CurrentTypes.Types = make(map[types.PluginType][]types.Type)
	}

	typ, err := c.Scheme.TypeForPrototype(proto)
	if err != nil {
		return fmt.Errorf("failed to get type for prototype %T: %w", proto, err)
	}

	// Register endpoints
	c.Handlers = append(c.Handlers,
		endpoints.Handler{
			Location: GetSignerIdentity,
			Handler:  handleGetSignerIdentity(plugin),
		},
		endpoints.Handler{
			Location: Sign,
			Handler:  handleSign(plugin),
		},
		endpoints.Handler{
			Location: GetVerifierIdentity,
			Handler:  handleGetVerifierIdentity(plugin),
		},
		endpoints.Handler{
			Location: Verify,
			Handler:  handleVerify(plugin),
		},
	)

	schema, err := runtime.GenerateJSONSchemaForType(proto)
	if err != nil {
		return fmt.Errorf("failed to generate jsonschema for prototype %T: %w", proto, err)
	}

	// Add resource type to the plugin's types
	c.CurrentTypes.Types[types.ResourceRepositoryPluginType] = append(c.CurrentTypes.Types[types.ResourceRepositoryPluginType], types.Type{
		Type:       typ,
		JSONSchema: schema,
	})

	return nil
}
