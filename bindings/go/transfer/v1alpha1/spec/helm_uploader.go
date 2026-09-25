package spec

import (
	"fmt"
	"net/url"
	"strings"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// HelmUploaderConfigType routes matching resources to a Helm repository of a JFrog Artifactory or
// Sonatype Nexus server.
const HelmUploaderConfigType = "helm.uploader.transfer.config.ocm.software"

func init() {
	Scheme.MustRegisterWithAlias(&HelmUploaderConfig{},
		runtime.NewVersionedType(HelmUploaderConfigType, Version),
		runtime.NewUnversionedType(HelmUploaderConfigType),
	)
}

// HelmRepositoryServer selects the API of the Helm repository server.
// +ocm:jsonschema-gen:enum=Artifactory,Nexus
type HelmRepositoryServer string

const (
	// HelmRepositoryServerArtifactory deploys into a JFrog Artifactory Helm repository.
	HelmRepositoryServerArtifactory HelmRepositoryServer = "Artifactory"
	// HelmRepositoryServerNexus uploads into a Sonatype Nexus Repository 3 Helm hosted repository.
	HelmRepositoryServerNexus HelmRepositoryServer = "Nexus"
)

// HelmUploaderConfig uploads matching Helm chart resources into a Helm repository of a JFrog
// Artifactory or Sonatype Nexus Repository 3 server and re-describes them with a Helm/v1 access
// (helmChart <name>:<version>). The chart is not parsed: name and version are the chart metadata
// the server records for the uploaded chart, and content it does not recognize as a chart fails
// the transfer. The chart is located in the resource content: a packaged chart, a tar containing
// one (as the helm downloader produces), or a helm chart OCI artifact (OCIImage and oci:// Helm
// sources, LocalBlob sources stored by the helm input).
//
// Artifactory: the chart is deployed to
// <url>/artifactory/<repository>/<component>/<component version>/<resource>-<resource version>.tgz
// and published with helmRepository <url>/artifactory/api/helm/<repository>. Content Artifactory
// does not recognize as a chart is deleted again. The target repository must not enforce chart
// name and version in file names (Artifactory's Helm Enforce Layout), because the file name is
// derived from the resource.
//
// Nexus: the chart is uploaded to <url>/repository/<repository>/<resource>-<resource version>.tgz;
// Nexus stores it under the path it derives from the chart (<name>-<version>.tgz) and the chart
// is published with helmRepository <url>/repository/<repository>.
//
// Upload credentials are resolved for the HelmChartRepository consumer identity of the published
// helmRepository URL, falling back to the Wget consumer identity of the upload URL.
//
//	type: generic.config.ocm.software/v1
//	configurations:
//	  - type: helm.uploader.transfer.config.ocm.software/v1alpha1
//	    match:
//	      accessType: Helm/v1
//	    server: Artifactory
//	    url: https://common.repositories.cloud.sap
//	    repository: open-component-model-helm-test
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type HelmUploaderConfig struct {
	// +ocm:jsonschema-gen:enum=helm.uploader.transfer.config.ocm.software/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=helm.uploader.transfer.config.ocm.software
	Type runtime.Type `json:"type"`
	// MatchSpec selects the resources this uploader applies to (exposed as `match`).
	MatchSpec UploaderMatch `json:"match"`
	// Server selects the API of the Helm repository server: Artifactory or Nexus.
	Server HelmRepositoryServer `json:"server"`
	// URL is the base URL of the server (scheme, host, optional port and context path) without
	// the /artifactory (Artifactory) or /repository (Nexus) segment,
	// e.g. https://common.repositories.cloud.sap.
	URL string `json:"url"`
	// Repository is the name of the Helm (hosted) repository to upload into.
	Repository string `json:"repository"`
	// Reindex requests Artifactory's Helm index recalculation for the uploaded chart only
	// (POST <url>/artifactory/api/helm/<repository>/<chart path>/reindex, Artifactory 7.105.2 or
	// later) after each upload. Artifactory only; Nexus maintains its index on its own. Artifactory
	// also indexes deployed charts on its own, so a failing request is only logged. Defaults to
	// true.
	Reindex *bool `json:"reindex,omitempty"`
}

// ReindexEnabled reports whether a Helm index recalculation follows each upload: always false for
// Nexus, default true for Artifactory.
func (u *HelmUploaderConfig) ReindexEnabled() bool {
	return u.Server == HelmRepositoryServerArtifactory && (u.Reindex == nil || *u.Reindex)
}

// Match reports whether this uploader applies to resource, delegating to the
// configured [UploaderMatch]. It implements [UploaderConfig].
func (u *HelmUploaderConfig) Match(resource descriptorv2.Resource) bool {
	if u == nil {
		return false
	}
	return u.MatchSpec.Matches(resource)
}

// Validate rejects a non-matching Type, an unknown server, reindex for Nexus, an empty match
// access type, a URL that is not an absolute http(s) URL without query or fragment and a
// repository that is not a single key. An empty Type is allowed for programmatically
// constructed configs.
func (u *HelmUploaderConfig) Validate() error {
	if u == nil {
		return nil
	}
	if !u.Type.IsEmpty() {
		if u.Type.Name != HelmUploaderConfigType || (u.Type.Version != "" && u.Type.Version != Version) {
			return fmt.Errorf("invalid type %q (must be %q or %q)",
				u.Type, HelmUploaderConfigType, runtime.NewVersionedType(HelmUploaderConfigType, Version))
		}
	}
	switch u.Server {
	case "":
		return fmt.Errorf("server is required (Artifactory or Nexus)")
	case HelmRepositoryServerArtifactory, HelmRepositoryServerNexus:
	default:
		return fmt.Errorf("server must be %q or %q, got %q", HelmRepositoryServerArtifactory, HelmRepositoryServerNexus, u.Server)
	}
	if u.Server == HelmRepositoryServerNexus && u.Reindex != nil {
		return fmt.Errorf("reindex is only supported for server %q", HelmRepositoryServerArtifactory)
	}
	if u.MatchSpec.AccessType.IsEmpty() {
		return fmt.Errorf("match.accessType is required")
	}
	if u.URL == "" {
		return fmt.Errorf("url is required")
	}
	parsed, err := url.Parse(u.URL)
	if err != nil {
		return fmt.Errorf("invalid url %q: %w", u.URL, err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("url must be an absolute http or https URL, got %q", u.URL)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("url must not carry a query or fragment, got %q", u.URL)
	}
	if u.Repository == "" {
		return fmt.Errorf("repository is required")
	}
	if strings.ContainsAny(u.Repository, "/?#") {
		return fmt.Errorf("repository must be a single repository key, got %q", u.Repository)
	}
	return nil
}
