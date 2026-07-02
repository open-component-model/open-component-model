// Package maven contains helpers for Maven GAV coordinate resolution, checksum
// handling, and authenticated artifact transport over HTTP(S).
package maven

import (
	"fmt"
	"mime"
	"net/url"
	"strings"

	v1 "ocm.software/open-component-model/bindings/go/maven/spec/access/v1"
)

// DefaultExtension is the packaging assumed when a Maven access omits Extension.
const DefaultExtension = "jar"

// FileRef identifies one resolvable Maven file: its absolute URL, its on-disk
// filename (used as the tar entry name), and the media type of a single blob.
type FileRef struct {
	URL       string
	Filename  string
	MediaType string
}

// IsSnapshot reports whether version is a Maven SNAPSHOT.
func IsSnapshot(version string) bool {
	return strings.HasSuffix(version, "-SNAPSHOT")
}

// IsFile reports that exactly one dedicated file is addressed (classifier and
// extension both explicitly set).
func IsFile(m *v1.Maven) bool { return m.Classifier != nil && m.Extension != nil }

// IsPackage reports that neither classifier nor extension is set (bare GAV).
func IsPackage(m *v1.Maven) bool { return m.Classifier == nil && m.Extension == nil }

// defaultMediaType returns the media type implied by a file extension. "jar" is
// pinned to the Maven-standard type; others resolve via the stdlib mime table,
// falling back to application/octet-stream.
func defaultMediaType(extension string) string {
	if extension == "jar" {
		// Pin jar explicitly: mime.TypeByExtension reads platform mime files and is
		// therefore not guaranteed, so we always return the Maven-standard type.
		return "application/java-archive"
	}
	if mt := mime.TypeByExtension("." + extension); mt != "" {
		return mt
	}
	return "application/octet-stream"
}

// fileName builds "<artifactId>-<version>[-<classifier>].<ext>".
func fileName(artifactID, version, classifier, extension string) string {
	name := artifactID + "-" + version
	if classifier != "" {
		name += "-" + classifier
	}
	if extension == "" {
		extension = DefaultExtension
	}
	return name + "." + extension
}

func repoBase(m *v1.Maven) (*url.URL, error) {
	u, err := url.Parse(m.RepoURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing repoUrl %q: %w", m.RepoURL, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("repoUrl %q must be an absolute URL", m.RepoURL)
	}
	return u, nil
}

func groupPath(groupID string) string { return strings.ReplaceAll(groupID, ".", "/") }

// makeRef resolves one file: dirVersion is the directory segment (baseVersion),
// fileVersion is the version embedded in the filename (== dirVersion for
// releases; the timestamped value for snapshots).
func makeRef(m *v1.Maven, dirVersion, fileVersion, classifier, extension string) (FileRef, error) {
	u, err := repoBase(m)
	if err != nil {
		return FileRef{}, err
	}
	if extension == "" {
		extension = DefaultExtension
	}
	name := fileName(m.ArtifactID, fileVersion, classifier, extension)
	full := u.JoinPath(groupPath(m.GroupID), m.ArtifactID, dirVersion, name).String()
	return FileRef{URL: full, Filename: name, MediaType: defaultMediaType(extension)}, nil
}

// versionMetadataURL is the version-level maven-metadata.xml (snapshot listing).
func versionMetadataURL(m *v1.Maven, baseVersion string) (string, error) {
	u, err := repoBase(m)
	if err != nil {
		return "", err
	}
	return u.JoinPath(groupPath(m.GroupID), m.ArtifactID, baseVersion, "maven-metadata.xml").String(), nil
}

// artifactMetadataURL is the artifact-level maven-metadata.xml (LATEST/RELEASE).
func artifactMetadataURL(m *v1.Maven) (string, error) {
	u, err := repoBase(m)
	if err != nil {
		return "", err
	}
	return u.JoinPath(groupPath(m.GroupID), m.ArtifactID, "maven-metadata.xml").String(), nil
}

// ArtifactURL builds the single deterministic artifact URL for upload, applying
// defaults (classifier none, extension jar).
func ArtifactURL(m *v1.Maven) (string, error) {
	classifier := ""
	if m.Classifier != nil {
		classifier = *m.Classifier
	}
	extension := DefaultExtension
	if m.Extension != nil && *m.Extension != "" {
		extension = *m.Extension
	}
	ref, err := makeRef(m, m.Version, m.Version, classifier, extension)
	if err != nil {
		return "", err
	}
	return ref.URL, nil
}

// UploadMediaType returns the media type to stamp on an uploaded single artifact.
func UploadMediaType(m *v1.Maven) string {
	if m.MediaType != nil && *m.MediaType != "" {
		return *m.MediaType
	}
	extension := DefaultExtension
	if m.Extension != nil && *m.Extension != "" {
		extension = *m.Extension
	}
	return defaultMediaType(extension)
}
