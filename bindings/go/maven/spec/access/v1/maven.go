package v1

import (
	"errors"
	"fmt"
	"net/url"

	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	Type              = "Maven"
	LegacyType        = "maven"
	LegacyTypeVersion = "v1"
)

// Maven describes the access for an artifact hosted in a Maven repository,
// addressed by its GAV coordinates. It is aligned with ocm v1
// https://github.com/open-component-model/ocm/blob/main/api/ocm/extensions/accessmethods/maven/method.go
//
// Credentials (username/password or an access token) are supplied through the
// credential resolver and keyed by the "MavenRepository" consumer identity.
//
// Note: this version resolves a single release artifact. SNAPSHOT version
// resolution (via maven-metadata.xml) and whole-GAV enumeration are not yet
// supported. See ocm-project follow-ups.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Maven struct {
	// +ocm:jsonschema-gen:enum=Maven/v1,maven/v1
	// +ocm:jsonschema-gen:enum:deprecated=Maven,maven
	Type runtime.Type `json:"type"`

	// RepoURL is the base URL of the Maven repository (e.g. https://repo1.maven.org/maven2).
	RepoURL string `json:"repoUrl"`
	// GroupID is the Maven group id (e.g. com.example).
	GroupID string `json:"groupId"`
	// ArtifactID is the Maven artifact id (e.g. lib).
	ArtifactID string `json:"artifactId"`
	// Version is the artifact version (e.g. 1.2.3). SNAPSHOT versions are not yet resolved.
	Version string `json:"version"`
	// Classifier is an optional Maven classifier (e.g. sources, javadoc).
	Classifier string `json:"classifier,omitempty"`
	// Extension is the artifact packaging/extension. Defaults to "jar" when empty.
	Extension string `json:"extension,omitempty"`
	// MediaType is the media type carried on the downloaded/uploaded blob.
	MediaType string `json:"mediaType,omitempty"`
}

// Validate verifies that the required coordinates are present and that RepoURL parses.
func (m *Maven) Validate() error {
	var errs []error
	if m.GroupID == "" {
		errs = append(errs, errors.New("groupId is required"))
	}
	if m.ArtifactID == "" {
		errs = append(errs, errors.New("artifactId is required"))
	}
	if m.Version == "" {
		errs = append(errs, errors.New("version is required"))
	}
	if m.RepoURL == "" {
		errs = append(errs, errors.New("repoUrl is required"))
	} else if _, err := url.ParseRequestURI(m.RepoURL); err != nil {
		errs = append(errs, fmt.Errorf("repoUrl is not a valid URL: %w", err))
	}
	return errors.Join(errs...)
}
