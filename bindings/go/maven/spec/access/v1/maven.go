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
// Selection: with both classifier and extension set, exactly one file is
// addressed. For a SNAPSHOT version, leaving either unset selects all matching
// files from maven-metadata.xml; they are packaged as a tgz when more than one
// file matches, and a single match is returned as-is. For a release, a bare
// GAV resolves to the main artifact; setting exactly one of
// classifier/extension is an error (releases cannot be enumerated).
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
	// Version is the artifact version (e.g. 1.2.3). SNAPSHOT versions (e.g.
	// 1.2.3-SNAPSHOT) are resolved via maven-metadata.xml.
	Version string `json:"version"`
	// Classifier is an optional Maven classifier (e.g. sources, javadoc).
	// Absent means unspecified; an explicitly empty string selects the main
	// artifact (no classifier). This distinction drives multi-file selection.
	Classifier *string `json:"classifier,omitempty"`
	// Extension is the artifact packaging/extension. nil defaults to "jar".
	Extension *string `json:"extension,omitempty"`
	// MediaType overrides the blob media type for a single downloaded/uploaded
	// file. Ignored for tgz output.
	MediaType *string `json:"mediaType,omitempty"`
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
