package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Wget describes the access for an HTTP-downloadable resource.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Wget struct {
	// +ocm:jsonschema-gen:enum=wget/v1
	// +ocm:jsonschema-gen:enum:deprecated=wget
	Type runtime.Type `json:"type"`

	// URL is the HTTP endpoint to download the resource from.
	URL string `json:"url"`

	// MediaType is the media type of the resource with optional format qualifiers.
	MediaType string `json:"mediaType,omitempty"`

	// Header contains HTTP headers to be sent with the request.
	Header map[string][]string `json:"header,omitempty"`

	// Verb is the HTTP method to use (GET, POST, etc.). Defaults to GET.
	Verb string `json:"verb,omitempty"`

	// Body is the HTTP body to send with the request.
	Body []byte `json:"body,omitempty"`

	// NoRedirect indicates whether to follow HTTP redirects.
	NoRedirect bool `json:"noRedirect,omitempty"`
}
