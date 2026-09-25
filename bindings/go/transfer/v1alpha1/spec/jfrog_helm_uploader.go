package spec

import (
	"fmt"
	"net/url"
	"strings"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// JFrogHelmUploaderConfigType routes matching resources to a JFrog Artifactory Helm repository.
const JFrogHelmUploaderConfigType = "jfrog.helm.uploader.transfer.config.ocm.software"

func init() {
	Scheme.MustRegisterWithAlias(&JFrogHelmUploaderConfig{},
		runtime.NewVersionedType(JFrogHelmUploaderConfigType, Version),
		runtime.NewUnversionedType(JFrogHelmUploaderConfigType),
	)
}

// JFrogHelmUploaderConfig deploys matching Helm chart resources into a JFrog Artifactory Helm
// repository via Artifactory's deploy REST API (PUT <url>/artifactory/<repository>/<name>-<version>.tgz,
// name and version from the chart's Chart.yaml) and re-describes them with a Helm/v1 access
// (helmRepository <url>/artifactory/api/helm/<repository>, helmChart <name>:<version>). The chart
// is detected from the resource content: a packaged chart, a tar containing one (as the helm
// downloader produces), or a helm chart OCI artifact (OCIImage and oci:// Helm sources, LocalBlob
// sources stored by the helm input). Upload credentials are resolved for the HelmChartRepository
// consumer identity of <url>/artifactory/api/helm/<repository>, falling back to the Wget consumer
// identity of the upload URL. By default the Helm index is recalculated after each upload.
//
//	type: generic.config.ocm.software/v1
//	configurations:
//	  - type: jfrog.helm.uploader.transfer.config.ocm.software/v1alpha1
//	    match:
//	      accessType: Helm/v1
//	    url: https://common.repositories.cloud.sap
//	    repository: open-component-model-helm-test
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type JFrogHelmUploaderConfig struct {
	// +ocm:jsonschema-gen:enum=jfrog.helm.uploader.transfer.config.ocm.software/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=jfrog.helm.uploader.transfer.config.ocm.software
	Type runtime.Type `json:"type"`
	// MatchSpec selects the resources this uploader applies to (exposed as `match`).
	MatchSpec UploaderMatch `json:"match"`
	// URL is the base URL of the Artifactory instance (scheme, host, optional port and
	// context path) without the /artifactory segment, e.g. https://common.repositories.cloud.sap.
	URL string `json:"url"`
	// Repository is the key of the Artifactory Helm repository to deploy into.
	Repository string `json:"repository"`
	// Reindex triggers Artifactory's Helm index recalculation
	// (POST <url>/artifactory/api/helm/<repository>/reindex) after each upload, so the chart
	// becomes pullable right away. Defaults to true; set to false on large repositories that
	// are reindexed by other means.
	Reindex *bool `json:"reindex,omitempty"`
}

// ReindexEnabled reports whether a Helm index recalculation follows each upload (default true).
func (u *JFrogHelmUploaderConfig) ReindexEnabled() bool {
	return u.Reindex == nil || *u.Reindex
}

// Match reports whether this uploader applies to resource, delegating to the
// configured [UploaderMatch]. It implements [UploaderConfig].
func (u *JFrogHelmUploaderConfig) Match(resource descriptorv2.Resource) bool {
	if u == nil {
		return false
	}
	return u.MatchSpec.Matches(resource)
}

// Validate rejects a non-matching Type, an empty match access type, a URL that is not an
// absolute http(s) URL without query or fragment and a repository that is not a single key.
// An empty Type is allowed for programmatically constructed configs.
func (u *JFrogHelmUploaderConfig) Validate() error {
	if u == nil {
		return nil
	}
	if !u.Type.IsEmpty() {
		if u.Type.Name != JFrogHelmUploaderConfigType || (u.Type.Version != "" && u.Type.Version != Version) {
			return fmt.Errorf("invalid type %q (must be %q or %q)",
				u.Type, JFrogHelmUploaderConfigType, runtime.NewVersionedType(JFrogHelmUploaderConfigType, Version))
		}
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
