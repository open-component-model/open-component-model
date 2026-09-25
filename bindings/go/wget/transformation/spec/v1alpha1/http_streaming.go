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
	// Opener names a source opener registered on the transformer that produces the bytes
	// to upload instead of the plain source download (e.g. the Helm chart archive of a Helm
	// or OCI resource). Empty uploads the downloaded source bytes unchanged.
	Opener string `json:"opener,omitempty"`
	// AfterUpload is an optional body-less request (POST unless a verb is set) sent after a
	// successful upload, e.g. to refresh a repository index. Credentials are resolved for its
	// URL like for Request; a non-2xx response fails the transformation.
	AfterUpload *wgetaccessv1.Wget `json:"afterUpload,omitempty"`
	// ComponentVersion is set when Resource is a local resource: it names the source component
	// version (and its repository) the local blob is streamed from, instead of downloading it
	// through its access.
	ComponentVersion *SourceComponentVersion `json:"componentVersion,omitempty"`
}

// SourceComponentVersion identifies the component version holding a local resource.
//
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type SourceComponentVersion struct {
	// Repository is the specification of the repository holding the component version.
	Repository *runtime.Raw `json:"repository"`
	// Component is the component name.
	Component string `json:"component"`
	// Version is the component version.
	Version string `json:"version"`
}

// HTTPStreamingOutput is the output specification for the HTTPStreaming transformation.
//
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type HTTPStreamingOutput struct {
	// Resource is the target resource descriptor with the digest filled in or verified.
	Resource *v2.Resource `json:"resource"`
}
