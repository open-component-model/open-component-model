package v1alpha1

import (
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	ArtifactoryUploadType = "ArtifactoryUpload"
	NexusUploadType       = "NexusUpload"
)

// ArtifactoryUpload uploads a resource into a local repository of a JFrog Artifactory server
// and publishes it with an access on that repository. The package type of the repository,
// read from the server, decides what is uploaded: a Helm chart published with a Helm/v1 access
// (helm), or the resource content as is, published with a Wget/v1 access (generic, maven, npm).
// The transfer creates it for resources matched by an ArtifactoryUploaderConfig.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type ArtifactoryUpload struct {
	// +ocm:jsonschema-gen:enum=ArtifactoryUpload/v1alpha1
	Type   runtime.Type            `json:"type"`
	ID     string                  `json:"id"`
	Spec   *RepositoryUploadSpec   `json:"spec"`
	Output *RepositoryUploadOutput `json:"output,omitempty"`
}

// NexusUpload uploads a resource into a hosted repository of a Sonatype Nexus Repository 3
// server and publishes it with an access on that repository. The format of the repository,
// read from the server, decides what is uploaded: a Helm chart published with a Helm/v1 access
// (helm), or the resource content as is, published with a Wget/v1 access (raw, maven2, npm).
// The transfer creates it for resources matched by a NexusUploaderConfig.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type NexusUpload struct {
	// +ocm:jsonschema-gen:enum=NexusUpload/v1alpha1
	Type   runtime.Type            `json:"type"`
	ID     string                  `json:"id"`
	Spec   *RepositoryUploadSpec   `json:"spec"`
	Output *RepositoryUploadOutput `json:"output,omitempty"`
}

// RepositoryUploadSpec is the input specification of the ArtifactoryUpload and NexusUpload
// transformations.
//
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type RepositoryUploadSpec struct {
	// Resource is the source resource to upload.
	Resource *v2.Resource `json:"resource"`
	// ComponentVersion is the component version holding the resource. It determines the default
	// upload location and, for local blob resources, where the resource is read from.
	ComponentVersion *RepositoryUploadComponentVersion `json:"componentVersion"`
	// URL is the base URL of the server.
	URL string `json:"url"`
	// Repository is the name of the target repository.
	Repository string `json:"repository"`
	// Path is where the content is stored, relative to the repository root. It must consist of
	// non-empty segments without . or .. and, for helm repositories, end in .tgz. Empty stores the
	// content under <component>/<component version>/<resource>-<resource version>.
	Path string `json:"path,omitempty"`
}

// RepositoryUploadComponentVersion identifies the component version holding the resource.
//
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type RepositoryUploadComponentVersion struct {
	// Repository is the specification of the repository holding the component version. It is
	// set for local blob resources only, which are read from it.
	Repository *runtime.Raw `json:"repository,omitempty"`
	// Component is the component name.
	Component string `json:"component"`
	// Version is the component version.
	Version string `json:"version"`
}

// RepositoryUploadOutput is the output of the ArtifactoryUpload and NexusUpload transformations.
//
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type RepositoryUploadOutput struct {
	// Resource is the uploaded resource with its access on the target repository.
	Resource *v2.Resource `json:"resource"`
}
