package maven

import (
	"encoding/xml"
	"fmt"
	"strings"
)

type metadata struct {
	XMLName    xml.Name   `xml:"metadata"`
	Versioning versioning `xml:"versioning"`
}

type versioning struct {
	Latest           string            `xml:"latest"`
	Release          string            `xml:"release"`
	Snapshot         snapshot          `xml:"snapshot"`
	SnapshotVersions []snapshotVersion `xml:"snapshotVersions>snapshotVersion"`
}

type snapshot struct {
	Timestamp   string `xml:"timestamp"`
	BuildNumber string `xml:"buildNumber"`
}

type snapshotVersion struct {
	Classifier string `xml:"classifier"`
	Extension  string `xml:"extension"`
	Value      string `xml:"value"`
}

func parseMetadata(body []byte) (*metadata, error) {
	var md metadata
	if err := xml.Unmarshal(body, &md); err != nil {
		return nil, fmt.Errorf("error parsing maven-metadata.xml: %w", err)
	}
	return &md, nil
}

// snapshotFileVersion resolves the filename version for one file of a snapshot.
// Preference: exact snapshotVersion (classifier+extension), then
// timestamp+buildNumber, then the literal baseVersion. All values sourced
// from the server-supplied maven-metadata.xml are validated against path
// traversal before being returned, since a malicious or compromised
// repository could otherwise inject it into the fetch URL and filename.
func snapshotFileVersion(md *metadata, baseVersion, classifier, extension string) (string, error) {
	for _, sv := range md.Versioning.SnapshotVersions {
		if sv.Classifier == classifier && sv.Extension == extension {
			if err := validateSnapshotVersionValues(sv); err != nil {
				return "", err
			}
			return sv.Value, nil
		}
	}
	if md.Versioning.Snapshot.Timestamp != "" && md.Versioning.Snapshot.BuildNumber != "" {
		for _, v := range []string{md.Versioning.Snapshot.Timestamp, md.Versioning.Snapshot.BuildNumber} {
			if unsafePathValue(v) {
				return "", fmt.Errorf("maven-metadata.xml snapshot timestamp/buildNumber contains an unsafe value %q", v)
			}
		}
		return strings.TrimSuffix(baseVersion, "SNAPSHOT") + md.Versioning.Snapshot.Timestamp + "-" + md.Versioning.Snapshot.BuildNumber, nil
	}
	return baseVersion, nil
}

// unsafePathValue reports whether v could escape its intended use as a tar
// entry name or URL path segment (e.g. via path traversal).
func unsafePathValue(v string) bool {
	return strings.ContainsAny(v, `/\`) || strings.Contains(v, "..")
}

// validateSnapshotVersionValues rejects a snapshotVersion whose Value,
// Classifier, or Extension could escape their intended use as a tar entry
// name or URL path segment. maven-metadata.xml is server-supplied, so a
// malicious or compromised repository could otherwise inject path traversal
// via these fields.
func validateSnapshotVersionValues(sv snapshotVersion) error {
	for _, v := range []string{sv.Value, sv.Classifier, sv.Extension} {
		if unsafePathValue(v) {
			return fmt.Errorf("maven-metadata.xml snapshotVersion contains an unsafe value %q", v)
		}
	}
	return nil
}
