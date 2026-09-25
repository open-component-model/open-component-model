package v1alpha1

import (
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
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
// Resource is the source resource descriptor with its original access. Request is the
// HTTP upload request (URL, method, headers, body, redirect handling) and is the single
// source of truth for the outbound call. TargetResource is the resource as it will be
// published after a successful upload: its access is the read (download) access at the
// resolved target URL and MUST NOT carry the upload-only request fields (write verb,
// body, request headers), so a later download does not re-issue the write request.
// Building both at graph-build time keeps the transfer plan literal and deterministic.
//
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type HTTPStreamingSpec struct {
	// Resource is the source resource descriptor.
	Resource *v2.Resource `json:"resource"`
	// Request is the resolved HTTP upload request (a Wget access carrying URL, verb,
	// headers, body and redirect handling).
	Request *wgetaccessv1.Wget `json:"request"`
	// TargetResource is the resource to publish after upload; its access is the read
	// access at the resolved target URL, without the upload-only request fields.
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
