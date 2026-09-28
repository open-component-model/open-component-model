package spec

import (
	"fmt"
	"net/url"
	"strings"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// ArtifactoryUploaderConfigType routes matching resources to a repository of a JFrog
	// Artifactory server.
	ArtifactoryUploaderConfigType = "artifactory.uploader.transfer.config.ocm.software"
	// NexusUploaderConfigType routes matching resources to a hosted repository of a Sonatype
	// Nexus Repository 3 server.
	NexusUploaderConfigType = "nexus.uploader.transfer.config.ocm.software"
)

func init() {
	Scheme.MustRegisterWithAlias(&ArtifactoryUploaderConfig{},
		runtime.NewVersionedType(ArtifactoryUploaderConfigType, Version),
		runtime.NewUnversionedType(ArtifactoryUploaderConfigType),
	)
	Scheme.MustRegisterWithAlias(&NexusUploaderConfig{},
		runtime.NewVersionedType(NexusUploaderConfigType, Version),
		runtime.NewUnversionedType(NexusUploaderConfigType),
	)
}

// ArtifactoryUploaderConfig uploads matching resources into a local repository of a JFrog
// Artifactory server. What is uploaded and how the resource is re-described depends on the
// package type of the repository, which is read from the Artifactory repository configuration
// (GET <url>/artifactory/api/repositories/<repository>):
//
//   - helm: the packaged Helm chart located in the resource content (a packaged chart, a tar
//     containing one as the helm downloader produces, or a helm chart OCI artifact) is deployed and
//     the resource is published with a Helm/v1 access (helmRepository
//     <url>/artifactory/api/helm/<repository>, helmChart <name>:<version>). The chart is not
//     parsed: name and version are the chart metadata Artifactory records, and content it does
//     not recognize as a chart is deleted again and fails the transfer. The repository must not
//     enforce chart name and version in file names (Helm Enforce Layout), because the file name
//     is not derived from the chart.
//   - generic, maven: the resource content is deployed as is (OCI artifacts as an OCI layout tar)
//     and the resource is published with a Wget/v1 access on the stored file. The file is not
//     packaged as a Maven artifact: it is downloadable, but Maven only resolves it if the content
//     and path already follow the Maven layout. Where Artifactory stores a file under another
//     path than requested (Maven -SNAPSHOT versions get a unique timestamped version), the access
//     points at the stored file.
//   - npm: the resource content, an npm package tarball, is deployed as is and published with a
//     Wget/v1 access on the stored file. Artifactory reads its package.json and serves it through
//     its npm API; content it does not recognize as a package is deleted again and fails the
//     transfer. Artifactory moves the latest dist-tag to the most recently deployed version.
//
// The content is deployed to <url>/artifactory/<repository>/<path>. Path defaults to
// <component>/<component version>/<resource>-<resource version>, plus .tgz for helm and npm. The
// deployed file carries the properties ocm.component.name, ocm.component.version, ocm.resource.name,
// ocm.resource.version and, for resources with one, ocm.resource.extraIdentity. A file already
// stored at the path is only replaced when these properties name the same resource of the same
// component version or it has the same content.
//
// Upload credentials are resolved for the HelmChartRepository consumer identity of
// <url>/artifactory/api/helm/<repository>, falling back to the Wget consumer identity of
// <url>/artifactory/<repository>. They are also used to read the repository configuration.
//
//	type: generic.config.ocm.software/v1
//	configurations:
//	  - type: artifactory.uploader.transfer.config.ocm.software/v1alpha1
//	    match:
//	      accessType: Helm/v1
//	    url: https://common.repositories.cloud.sap
//	    repository: open-component-model-helm-test
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type ArtifactoryUploaderConfig struct {
	// +ocm:jsonschema-gen:enum=artifactory.uploader.transfer.config.ocm.software/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=artifactory.uploader.transfer.config.ocm.software
	Type runtime.Type `json:"type"`
	// MatchSpec selects the resources this uploader applies to (exposed as `match`).
	MatchSpec UploaderMatch `json:"match"`
	// URL is the base URL of the server (scheme, host, optional port and context path) without
	// the /artifactory segment, e.g. https://common.repositories.cloud.sap.
	URL string `json:"url"`
	// Repository is the key of the local repository to upload into.
	Repository string `json:"repository"`
	// Path overrides where the content is stored, relative to the repository root, e.g.
	// ${component.name + "/" + component.version + "/" + resource.name + "-" + resource.version + ".tgz"}.
	// It is a literal or a CEL expression wrapped in ${...} over the source resource (resource)
	// and its component (component: name, version, provider, ...). The result must consist of
	// non-empty segments without . or .. and, for helm and npm repositories, end in .tgz. Defaults to
	// <component>/<component version>/<resource>-<resource version>, plus .tgz for helm and npm.
	Path string `json:"path,omitempty"`
}

// NexusUploaderConfig uploads matching resources into a hosted repository of a Sonatype Nexus
// Repository 3 server. What is uploaded and how the resource is re-described depends on the
// format of the repository, which is read from the Nexus repository settings
// (GET <url>/service/rest/v1/repositories/<repository>):
//
//   - helm: the packaged Helm chart located in the resource content is uploaded to
//     <url>/repository/<repository>/<resource>-<resource version>.tgz. Nexus stores it under the
//     path it derives from the chart (<name>-<version>.tgz) and the resource is published with a
//     Helm/v1 access (helmRepository <url>/repository/<repository>). Path is not supported.
//   - raw: the resource content is uploaded as is (OCI artifacts as an OCI layout tar) to
//     <url>/repository/<repository>/<path> and the resource is published with a Wget/v1 access
//     on it. Path defaults to <component>/<component version>/<resource>-<resource version>.
//     Nexus records no owner of a file, so a file already stored at the path is never
//     overwritten: it is reused when it has the same content, otherwise the transfer fails.
//   - maven2: the resource content is uploaded as one file of a Maven component to Path, which
//     must follow the Maven repository layout
//     <group path>/<artifactId>/<version>/<artifactId>-<version>[-<classifier>].<extension>. The
//     coordinates are taken from the path. Release versions go through the components API,
//     which keeps maven-metadata.xml up to date; snapshot versions, which the components API
//     refuses, are uploaded with a plain PUT and are not added to maven-metadata.xml. Upload the
//     POM as a resource of its own so Maven can resolve the component. The resource is
//     published with a Wget/v1 access on the stored file, and like raw files, a stored file is
//     never overwritten.
//   - npm: the resource content, an npm package tarball, is uploaded through the components API.
//     Nexus reads name and version from its package.json and stores it under
//     <name>/-/<name>-<version>.tgz; the resource is published with a Wget/v1 access on the
//     stored tarball. A tarball the repository already stores is reused. Path is not supported.
//
// Upload credentials are resolved for the HelmChartRepository consumer identity of
// <url>/repository/<repository>, falling back to its Wget consumer identity. They are also used
// to read the repository settings.
//
//	type: generic.config.ocm.software/v1
//	configurations:
//	  - type: nexus.uploader.transfer.config.ocm.software/v1alpha1
//	    match:
//	      accessType: Helm/v1
//	    url: https://nexus.example.com
//	    repository: helm-hosted
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type NexusUploaderConfig struct {
	// +ocm:jsonschema-gen:enum=nexus.uploader.transfer.config.ocm.software/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=nexus.uploader.transfer.config.ocm.software
	Type runtime.Type `json:"type"`
	// MatchSpec selects the resources this uploader applies to (exposed as `match`).
	MatchSpec UploaderMatch `json:"match"`
	// URL is the base URL of the server (scheme, host, optional port and context path) without
	// the /repository segment, e.g. https://nexus.example.com.
	URL string `json:"url"`
	// Repository is the name of the hosted repository to upload into.
	Repository string `json:"repository"`
	// Path overrides where the content is stored in a raw repository and is required for a
	// maven2 repository, relative to the repository root. It is a literal or a CEL expression
	// wrapped in ${...} over the source resource (resource) and its component (component: name,
	// version, provider, ...). The result must consist of non-empty segments other than "." and
	// "..", and for maven2 follow the Maven repository layout. It is not supported for helm
	// repositories: Nexus stores charts under a path derived from the chart. Defaults to
	// <component>/<component version>/<resource>-<resource version> for raw repositories.
	Path string `json:"path,omitempty"`
}

// Match reports whether this uploader applies to resource. It implements [UploaderConfig].
func (u *ArtifactoryUploaderConfig) Match(resource descriptorv2.Resource, types TypeResolver) bool {
	if u == nil {
		return false
	}
	return u.MatchSpec.Matches(resource, types)
}

// Match reports whether this uploader applies to resource. It implements [UploaderConfig].
func (u *NexusUploaderConfig) Match(resource descriptorv2.Resource, types TypeResolver) bool {
	if u == nil {
		return false
	}
	return u.MatchSpec.Matches(resource, types)
}

// Validate rejects a non-matching Type, an empty match access type, a URL that is not an
// absolute http(s) URL without query or fragment and a repository that is not a single key. An
// empty Type is allowed for programmatically constructed configs.
func (u *ArtifactoryUploaderConfig) Validate() error {
	if u == nil {
		return nil
	}
	return validateRepositoryUploader(u.Type, ArtifactoryUploaderConfigType, u.MatchSpec, u.URL, u.Repository)
}

// Validate rejects what [ArtifactoryUploaderConfig.Validate] rejects.
func (u *NexusUploaderConfig) Validate() error {
	if u == nil {
		return nil
	}
	if err := validateRepositoryUploader(u.Type, NexusUploaderConfigType, u.MatchSpec, u.URL, u.Repository); err != nil {
		return err
	}
	return nil
}

func validateRepositoryUploader(typ runtime.Type, name string, match UploaderMatch, rawURL, repository string) error {
	if !typ.IsEmpty() {
		if typ.Name != name || (typ.Version != "" && typ.Version != Version) {
			return fmt.Errorf("invalid type %q (must be %q or %q)", typ, name, runtime.NewVersionedType(name, Version))
		}
	}
	if match.AccessType.IsEmpty() {
		return fmt.Errorf("match.accessType is required")
	}
	if rawURL == "" {
		return fmt.Errorf("url is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url %q: %w", rawURL, err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("url must be an absolute http or https URL, got %q", rawURL)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("url must not carry a query or fragment, got %q", rawURL)
	}
	if repository == "" {
		return fmt.Errorf("repository is required")
	}
	if strings.ContainsAny(repository, "/?#") {
		return fmt.Errorf("repository must be a single repository key, got %q", repository)
	}
	return nil
}
