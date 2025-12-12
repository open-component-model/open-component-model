package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Helm describes an input sourced by a file system directory having a typical helm chart structure:
// Chart.yaml, values.yaml, charts/, templates/, ...

// Helm implements the input type for helm charts.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Helm struct {
	// +ocm:jsonschema-gen:enum=helm/v1,Helm/v1
	// +ocm:jsonschema-gen:enum:deprecated=helm,Helm
	Type runtime.Type `json:"type"`

	// Path is the path to the directory or tgz file containing the chart.
	Path string `json:"path,omitempty"`

	// Repository is an OCI reference specifying the upload location of the fetched chart.
	// The reference MUST contain a version tag, and it needs to equal the version of the chart.
	Repository string `json:"repository,omitempty"`

	//nolint:gocritic // Deprecated field is okay
	// Version is used in the following ways:
	// - in the case of HTTP/S based helm repositories, the version is ignored (e.g.: https://example.com/charts/mychart-1.2.3.tgz)
	// - in case of oci based helm repositories:
	//   - if not set, the version is taken from the oci reference tag (e.g.: oci://example.com/charts/mychart:1.2.3)
	//   - if set, and the oci reference tag is not set, the version is set to the value of this field.
	//   - if both are set they MUST equal otherwise helm download will fail with a mismatch error.
	// Deprecated: This field should NOT be used, either rely on the full http/s url or an oci reference with a tag.
	Version string `json:"version,omitempty"`

	// HelmRepository specifies the download location of the helm chart. It can either be a URL or an OCI reference.
	HelmRepository string `json:"helmRepository,omitempty"`

	//nolint:gocritic // Deprecated field is okay
	// CACert is used in combination with HelmRepository to specify a TLS root certificate to access the source helm repository.
	// Deprecated: This field is deprecated in favor of using certificates through the credentials.
	CACert string `json:"caCert,omitempty"`

	//nolint:gocritic // Deprecated field is okay
	// CACertFile is used in combination with HelmRepository to specify a relative filename
	// for TLS root certificate to access the source helm repository.
	// Deprecated: This field is deprecated in favor of using certificates through the credentials.
	CACertFile string `json:"caCertFile,omitempty"`
}

func (t *Helm) String() string {
	return t.Path
}

const (
	Version = "v1"
	Type    = "helm"
)
