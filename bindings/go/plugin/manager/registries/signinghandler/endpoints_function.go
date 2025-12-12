package signinghandler

import (
	"encoding/json"
	"fmt"
	"net/http"

	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/signing/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/plugins"
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

// credentialsFromHeader extracts credentials from the Authorization header if present
// and unmarshals them into a map. If the header is absent, it returns an empty map.
// If the header is present but cannot be unmarshaled, it writes an error response and returns ok as false.
func credentialsFromHeader(w http.ResponseWriter, h http.Header) (credentials map[string]string, ok bool) {
	authHeader := h.Get("Authorization")
	if authHeader == "" {
		return nil, true
	}
	if err := json.Unmarshal([]byte(authHeader), &credentials); err != nil {
		plugins.NewError(fmt.Errorf("failed to marshal credentials: %w", err), http.StatusUnauthorized).Write(w)
		return nil, false
	}
	return credentials, true
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

// handleSign handles the Sign endpoint
func handleSign[T runtime.Typed](plugin v1.SignerPluginContract[T]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		credentials, ok := credentialsFromHeader(w, r.Header)
		if !ok {
			return
		}

		var request v1.SignRequest[T]
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			handleError(w, err, http.StatusBadRequest, "failed to unmarshal request")
			return
		}

		response, err := plugin.Sign(r.Context(), &request, credentials)
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
		credentials, ok := credentialsFromHeader(w, r.Header)
		if !ok {
			return
		}

		var request v1.VerifyRequest[T]
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			handleError(w, err, http.StatusBadRequest, "failed to unmarshal request")
			return
		}

		response, err := plugin.Verify(r.Context(), &request, credentials)
		if err != nil {
			handleError(w, err, http.StatusInternalServerError, "failed to add global resource")
			return
		}

		handleJSONResponse(w, response)
	}
}

// RegisterPlugin registers the resource plugin endpoints with the endpoint builder.
// It sets up HTTP handlers for identity, sign and verify operations.
func RegisterPlugin[CONTRACT v1.SignatureHandlerContract[T], T runtime.Typed](
	proto T,
	plugin CONTRACT,
	c *endpoints.EndpointBuilder,
) error {
	typ, err := c.Scheme.TypeForPrototype(proto)
	if err != nil {
		return fmt.Errorf("failed to get type for prototype %T: %w", proto, err)
	}

	// Register endpoints
	c.Handlers = append(c.Handlers,
		endpoints.Handler{
			Location: GetSignerIdentity,
			Handler:  handleGetSignerIdentity[T](plugin),
		},
		endpoints.Handler{
			Location: Sign,
			Handler:  handleSign[T](plugin),
		},
		endpoints.Handler{
			Location: GetVerifierIdentity,
			Handler:  handleGetVerifierIdentity[T](plugin),
		},
		endpoints.Handler{
			Location: Verify,
			Handler:  handleVerify[T](plugin),
		},
	)

	schema, err := plugins.GenerateJSONSchemaForType(proto)
	if err != nil {
		return fmt.Errorf("failed to generate jsonschema for prototype %T: %w", proto, err)
	}

	// Add resource type to the plugin's types
	c.PluginSpec.CapabilitySpecs = append(c.PluginSpec.CapabilitySpecs, &v1.CapabilitySpec{
		Type: runtime.NewUnversionedType(string(v1.SigningHandlerPluginType)),
		SupportedSigningSpecTypes: []types.Type{
			{
				Type:       typ,
				Aliases:    nil,
				JSONSchema: schema,
			},
		},
	})

	return nil
}
