// Package maven contains helpers for Maven GAV coordinate resolution, sibling
// file naming, and authenticated artifact transport over HTTP(S).
package maven

import (
	"fmt"
	"net/url"
	"strings"

	"ocm.software/open-component-model/bindings/go/maven/spec/access/v2alpha1"
)

// FileRef identifies one resolvable Maven file: its absolute URL and its
// on-disk filename (used as the tar entry name).
type FileRef struct {
	URL      string
	Filename string
}

// IsSnapshot reports whether version is a Maven SNAPSHOT.
func IsSnapshot(version string) bool {
	return strings.HasSuffix(version, "-SNAPSHOT")
}

// mediaTypes maps the extensions a Maven repository entry can have to the
// Content-Type sent on upload. A fixed table keeps the header the same on
// every machine; mime.TypeByExtension reads the host's mime files and would
// not.
var mediaTypes = map[string]string{
	"jar":    "application/java-archive",
	"pom":    "application/xml",
	"xml":    "application/xml",
	"zip":    "application/zip",
	"asc":    "text/plain",
	"md5":    "text/plain",
	"sha1":   "text/plain",
	"sha256": "text/plain",
	"sha512": "text/plain",
}

// MediaTypeFor returns the Content-Type to send when uploading a file with the
// given extension, falling back to application/octet-stream.
func MediaTypeFor(extension string) string {
	if mt, ok := mediaTypes[extension]; ok {
		return mt
	}
	return "application/octet-stream"
}

// fileName builds "<artifactId>-<version>[-<classifier>].<ext>".
func fileName(artifactID, version string, a v2alpha1.Artifact) string {
	name := artifactID + "-" + version
	if a.Classifier != "" {
		name += "-" + a.Classifier
	}
	return name + "." + a.Extension
}

// repoBase parses RepoURL. Validate already requires a scheme and a host, so
// only a malformed URL can fail here.
func repoBase(m *v2alpha1.Maven) (*url.URL, error) {
	u, err := url.Parse(m.RepoURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing repoUrl %q: %w", m.RepoURL, err)
	}
	return u, nil
}

func groupPath(groupID string) string { return strings.ReplaceAll(groupID, ".", "/") }

// makeRef resolves one file: dirVersion is the directory segment (baseVersion),
// fileVersion is the version embedded in the filename (== dirVersion for
// releases; the timestamped value for snapshots).
func makeRef(m *v2alpha1.Maven, dirVersion, fileVersion string, a v2alpha1.Artifact) (FileRef, error) {
	name := fileName(m.ArtifactID, fileVersion, a)
	full, err := fileURL(m, dirVersion, name)
	if err != nil {
		return FileRef{}, err
	}
	return FileRef{URL: full, Filename: name}, nil
}

// fileURL is the absolute URL of name inside the version directory of m.
func fileURL(m *v2alpha1.Maven, dirVersion, name string) (string, error) {
	u, err := repoBase(m)
	if err != nil {
		return "", err
	}
	return u.JoinPath(groupPath(m.GroupID), m.ArtifactID, dirVersion, name).String(), nil
}

// versionMetadataURL is the version-level maven-metadata.xml (snapshot listing).
func versionMetadataURL(m *v2alpha1.Maven, baseVersion string) (string, error) {
	u, err := repoBase(m)
	if err != nil {
		return "", err
	}
	return u.JoinPath(groupPath(m.GroupID), m.ArtifactID, baseVersion, "maven-metadata.xml").String(), nil
}

// artifactMetadataURL is the artifact-level maven-metadata.xml (LATEST/RELEASE).
func artifactMetadataURL(m *v2alpha1.Maven) (string, error) {
	u, err := repoBase(m)
	if err != nil {
		return "", err
	}
	return u.JoinPath(groupPath(m.GroupID), m.ArtifactID, "maven-metadata.xml").String(), nil
}

// ReleaseFileName is the file name of artifact a at the literal spec version,
// the name a release download uses and an upload must match.
func ReleaseFileName(m *v2alpha1.Maven, a v2alpha1.Artifact) string {
	return fileName(m.ArtifactID, m.Version, a)
}

// FileURL is the release-path URL of a file called name, used for upload.
func FileURL(m *v2alpha1.Maven, name string) (string, error) {
	return fileURL(m, m.Version, name)
}
