package v1alpha1

import (
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const HTTPStreamingType = "HTTPStreaming"

// HTTPStreaming is a fused transformation that streams a resource's content
// directly from its source access to a target HTTP endpoint (e.g. a PUT upload)
// without materializing the body in memory or on disk. It replaces the separate
// download + local-blob-upload pair for resources routed through an uploader
// configuration.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type HTTPStreaming struct {
	// +ocm:jsonschema-gen:enum=HTTPStreaming/v1alpha1
	Type   runtime.Type         `json:"type"`
	ID     string               `json:"id"`
	Spec   *HTTPStreamingSpec   `json:"spec"`
	Output *HTTPStreamingOutput `json:"output,omitempty"`
}

// HTTPStreamingSpec is the input specification for the HTTPStreaming transformation.
//
// Resource is the source resource descriptor with its original access. TargetResource
// is the fully-resolved target resource carrying a Wget access whose fields (URL, Verb,
// Header, Body, NoRedirect, MediaType) encode the entire HTTP request. Building the
// target resource at graph-build time keeps the transfer plan literal and deterministic.
//
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type HTTPStreamingSpec struct {
	// Resource is the source resource descriptor.
	Resource *v2.Resource `json:"resource"`
	// TargetResource is the target resource descriptor with the destination Wget access.
	TargetResource *v2.Resource `json:"targetResource"`
}

// HTTPStreamingOutput is the output specification for the HTTPStreaming transformation.
//
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type HTTPStreamingOutput struct {
	// Resource is the target resource descriptor with the digest filled in or verified.
	Resource *v2.Resource `json:"resource"`
}
