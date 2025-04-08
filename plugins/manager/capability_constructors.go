package manager

import (
	"encoding/json"
	"net/http"

	"github.com/invopop/jsonschema"

	"ocm.software/open-component-model/bindings/go/runtime"
)

type Handler struct {
	Handler  http.HandlerFunc `json:"-"` // ignore the handler when marshalling
	Location string           `json:"location"`
	Schema   []byte           `json:"schema"`
}

type OCMComponentVersionRepositoryHandlers struct {
	UploadComponentVersion   Handler // maybe contain the type here?
	DownloadComponentVersion Handler
	UploadResource           Handler
	DownloadResource         Handler
}

type OCMComponentVersionRepositoryHandlersOpts struct {
	UploadComponentVersion   http.HandlerFunc
	DownloadComponentVersion http.HandlerFunc
	UploadResource           http.HandlerFunc
	DownloadResource         http.HandlerFunc
}

func (o *OCMComponentVersionRepositoryHandlers) GetHandlers() []Handler {
	return []Handler{
		o.UploadComponentVersion,
		o.DownloadComponentVersion,
		o.UploadResource,
		o.DownloadResource,
	}
}

var _ CapabilityHandlerProvider = &OCMComponentVersionRepositoryHandlers{}

// CapabilityHandlerProvider can be used to list handlers that the plugin SDK needs to register for a plugin.
// This is used by the SDK as a convenience so users don't have to care about it.
type CapabilityHandlerProvider interface {
	GetHandlers() []Handler
}

type OCMComponentVersionRepositoryOptions struct {
	Handlers OCMComponentVersionRepositoryHandlers `json:"handlers"`
}

// OCIRegistry assume this type lives in binding/go or some other place in OCM.
type OCIRegistry struct {
	runtime.Type `json:"type"`
	BaseUrl      string `json:"baseUrl"`
	SubPath      string `json:"subPath"`
}

var Type = "OCIArtifact/v1"

// This should return something that the plugin then can use to register handlers.
// Provides the handlers and also provides what to pass back to the plugin registration?
// This has an opinionated parameter list for the handlers it needs. It will construct the
// handlers including the location, send back a serialized format, and send back a struct
// that might implement some kind of interface or something for the plugin SDK to start the plugin.
func NewOCMComponentVersionRepository(typ string, pluginType PluginType, handlers OCMComponentVersionRepositoryHandlersOpts) (CapabilityHandlerProvider, []byte, error) {
	ociRegistry := &OCIRegistry{}
	schemaOCIRegistry, err := jsonschema.Reflect(ociRegistry).MarshalJSON()
	if err != nil {
		panic(err)
	}

	result := &OCMComponentVersionRepositoryHandlers{
		UploadComponentVersion: Handler{
			Handler:  handlers.UploadComponentVersion,
			Location: "/cv/upload",
		},
		DownloadComponentVersion: Handler{
			Handler:  handlers.DownloadComponentVersion,
			Location: "/cv/download",
		},
		UploadResource: Handler{
			Handler:  handlers.UploadResource,
			Location: "/cv/upload/resource",
			Schema:   schemaOCIRegistry,
		},
		DownloadResource: Handler{
			Handler:  handlers.DownloadResource,
			Location: "/cv/download/resource",
			Schema:   schemaOCIRegistry,
		},
	}

	capability := Capabilities{
		PluginType: pluginType,
		Capabilities: []Capability{
			{
				// TODO: Need the endpoints here? Should the endpoints contain the type?
				Capability: "OCMComponentVersionRepository",
				Type:       typ, // which would be OCIRegistry for example. Could we infer this?
			},
		},
	}

	content, err := json.Marshal(capability)
	if err != nil {
		return nil, nil, err
	}

	//str := &OCMComponentVersionRepositoryHandlers{
	//
	//}

	// how do I turn that now into the right thing?
	// maybe the other thing does that?
	// what if we... provide the same struct for the capability constructor?

	// what now...

	// ask for the endpoints, but that's a list...
	// ... construct the plugin with the given handlers and the schema the handlers need.
	// we can be opinionated on the schema
	// We know that information.

	return result, content, nil
}
