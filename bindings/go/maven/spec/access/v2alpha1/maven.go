package v2alpha1

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// Type is the name of the Maven access type. The canonical wire form is
// "maven/v2alpha1".
const Type = "maven"

// Maven describes access to one or more files of a Maven artifact. The artifact
// is addressed by its GAV coordinates; the files are listed explicitly in
// Artifacts by extension and optional classifier.
//
// Maven's own protocol has no listing for release versions, so the spec has to
// name the files it wants. This keeps resolution deterministic and avoids
// scraping directory pages. Sibling files (the ".asc" signature and the
// checksum files) are never listed; they are fetched automatically for every
// file the repository has them for and stored as-is. The result is always one
// application/x-tgz archive, even for a single file.
//
// SNAPSHOT versions resolve their timestamped file names through the
// version-level maven-metadata.xml. When that file is absent, the literal
// "-SNAPSHOT" file name is used. LATEST and RELEASE resolve through the
// artifact-level maven-metadata.xml.
//
// Credentials (username/password or a bearer token in identityToken) are
// supplied through the credential resolver and keyed by the "MavenRepository"
// consumer identity.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Maven struct {
	// +ocm:jsonschema-gen:enum=maven/v2alpha1,Maven/v2alpha1
	Type runtime.Type `json:"type"`

	// RepoURL is the base URL of the Maven repository (e.g. https://repo1.maven.org/maven2).
	RepoURL string `json:"repoUrl"`
	// GroupID is the Maven group id (e.g. com.example).
	GroupID string `json:"groupId"`
	// ArtifactID is the Maven artifact id (e.g. lib).
	ArtifactID string `json:"artifactId"`
	// Version is the artifact version (e.g. 1.2.3, 1.2.3-SNAPSHOT, LATEST, RELEASE).
	Version string `json:"version"`
	// Artifacts lists the files to access. At least one entry is required.
	Artifacts []Artifact `json:"artifacts"`
}

// Artifact selects one file of a Maven artifact, named
// "<artifactId>-<version>[-<classifier>].<extension>" in the repository.
type Artifact struct {
	// Extension is the file extension (e.g. jar, pom, zip). Required.
	Extension string `json:"extension"`
	// Classifier is the optional Maven classifier (e.g. sources, javadoc).
	// Empty selects the main file.
	Classifier string `json:"classifier,omitempty"`
}

// Validate checks that the coordinates are present and safe to place in a URL
// path and a tar entry name, that RepoURL is an absolute URL, and that
// Artifacts holds at least one well-formed, unique entry.
func (m *Maven) Validate() error {
	var errs []error
	for _, c := range []struct{ name, value string }{
		{"groupId", m.GroupID}, {"artifactId", m.ArtifactID}, {"version", m.Version},
	} {
		if c.value == "" {
			errs = append(errs, fmt.Errorf("%s is required", c.name))
		} else if escapesPathSegment(c.value) {
			errs = append(errs, fmt.Errorf("%s %q must not contain path separators or \"..\"", c.name, c.value))
		}
	}
	if m.RepoURL == "" {
		errs = append(errs, errors.New("repoUrl is required"))
	} else if u, err := url.Parse(m.RepoURL); err != nil {
		errs = append(errs, fmt.Errorf("repoUrl is not a valid URL: %w", err))
	} else if u.Scheme == "" || u.Host == "" {
		errs = append(errs, fmt.Errorf("repoUrl %q must be an absolute URL with a scheme and a host", m.RepoURL))
	}
	errs = append(errs, validateArtifacts(m.Artifacts)...)
	return errors.Join(errs...)
}

// IsPinnedVersion reports whether Version names one fixed set of files.
// LATEST and RELEASE are looked up in maven-metadata.xml and a SNAPSHOT is
// redeployed in place, so what either resolves to changes over time. A digest
// or a by-value transfer must not be taken over such a version.
func (m *Maven) IsPinnedVersion() bool {
	return m.Version != "LATEST" && m.Version != "RELEASE" && !strings.HasSuffix(m.Version, "-SNAPSHOT")
}

func validateArtifacts(artifacts []Artifact) []error {
	if len(artifacts) == 0 {
		return []error{errors.New("artifacts must list at least one file")}
	}
	var errs []error
	seen := make(map[Artifact]struct{}, len(artifacts))
	for i, a := range artifacts {
		if a.Extension == "" {
			errs = append(errs, fmt.Errorf("artifacts[%d]: extension is required", i))
		}
		for _, v := range []string{a.Extension, a.Classifier} {
			if escapesPathSegment(v) {
				errs = append(errs, fmt.Errorf("artifacts[%d]: %q must not contain path separators or \"..\"", i, v))
			}
		}
		if _, dup := seen[a]; dup {
			errs = append(errs, fmt.Errorf("artifacts[%d]: duplicate entry (extension %q, classifier %q)", i, a.Extension, a.Classifier))
		}
		seen[a] = struct{}{}
	}
	return errs
}

// escapesPathSegment reports whether v could leave the URL path segment or the
// tar entry name it is placed in. Every coordinate and every artifact field
// ends up in both, and url.JoinPath resolves "..", so a value holding one would
// fetch from outside the artifact directory or write outside the archive.
func escapesPathSegment(v string) bool {
	return strings.ContainsAny(v, `/\`) || strings.Contains(v, "..")
}
