// Package maven contains pure helpers for Maven GAV coordinate resolution.
package maven

import (
	"fmt"
	"net/url"
	"strings"

	v1 "ocm.software/open-component-model/bindings/go/maven/spec/access/v1"
)

// DefaultExtension is the packaging assumed when a Maven access omits Extension.
const DefaultExtension = "jar"

// extension returns the access extension or the default.
func extension(m *v1.Maven) string {
	if m.Extension != "" {
		return m.Extension
	}
	return DefaultExtension
}

// ArtifactPath returns the repository-relative path of the artifact, e.g.
// "com/example/lib/1.2.3/lib-1.2.3-sources.jar".
func ArtifactPath(m *v1.Maven) string {
	group := strings.ReplaceAll(m.GroupID, ".", "/")
	filename := m.ArtifactID + "-" + m.Version
	if m.Classifier != "" {
		filename += "-" + m.Classifier
	}
	filename += "." + extension(m)
	return strings.Join([]string{group, m.ArtifactID, m.Version, filename}, "/")
}

// ArtifactURL joins RepoURL and ArtifactPath, normalizing slashes.
func ArtifactURL(m *v1.Maven) (string, error) {
	u, err := url.Parse(m.RepoURL)
	if err != nil {
		return "", fmt.Errorf("error parsing repoUrl %q: %w", m.RepoURL, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("repoUrl %q must be an absolute URL", m.RepoURL)
	}
	return u.JoinPath(ArtifactPath(m)).String(), nil
}

// DefaultMediaType returns the media type implied by the artifact extension.
func DefaultMediaType(m *v1.Maven) string {
	if extension(m) == "jar" {
		return "application/java-archive"
	}
	return "application/octet-stream"
}
