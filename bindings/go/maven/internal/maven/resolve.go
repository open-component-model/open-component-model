package maven

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"ocm.software/open-component-model/bindings/go/maven/spec/access/v2alpha1"
	credsv1 "ocm.software/open-component-model/bindings/go/maven/spec/credentials/v1"
)

// fetchMetadata GETs and parses a maven-metadata.xml. found is false on 404.
func (c *Client) fetchMetadata(ctx context.Context, url string, creds *credsv1.MavenCredentials) (md *metadata, found bool, err error) {
	resp, err := c.Get(ctx, url, creds)
	if err != nil {
		return nil, false, fmt.Errorf("error fetching %q: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("error fetching %q: unexpected status %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("error reading %q: %w", url, err)
	}
	md, err = parseMetadata(body)
	if err != nil {
		return nil, false, err
	}
	return md, true, nil
}

// Resolve returns one FileRef per entry of m.Artifacts, in spec order.
// Releases need no lookup. SNAPSHOT file names come from the version-level
// maven-metadata.xml; LATEST and RELEASE come from the artifact-level one.
func (c *Client) Resolve(ctx context.Context, m *v2alpha1.Maven, creds *credsv1.MavenCredentials) ([]FileRef, error) {
	base, err := c.resolveBaseVersion(ctx, m, creds)
	if err != nil {
		return nil, err
	}
	if !IsSnapshot(base) {
		return resolveRelease(m, base)
	}
	return c.resolveSnapshot(ctx, m, base, creds)
}

// resolveBaseVersion turns the LATEST and RELEASE metaversions into a concrete
// version and returns any other version unchanged.
func (c *Client) resolveBaseVersion(ctx context.Context, m *v2alpha1.Maven, creds *credsv1.MavenCredentials) (string, error) {
	if m.Version != "LATEST" && m.Version != "RELEASE" {
		return m.Version, nil
	}
	u, err := artifactMetadataURL(m)
	if err != nil {
		return "", err
	}
	md, found, err := c.fetchMetadata(ctx, u, creds)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("no maven-metadata.xml to resolve %q for %s:%s", m.Version, m.GroupID, m.ArtifactID)
	}
	base := md.Versioning.Latest
	if m.Version == "RELEASE" {
		base = md.Versioning.Release
	}
	if base == "" {
		return "", fmt.Errorf("maven-metadata.xml has no %q version for %s:%s", m.Version, m.GroupID, m.ArtifactID)
	}
	return base, nil
}

// resolveRelease is pure: a release file lives at its literal coordinates.
func resolveRelease(m *v2alpha1.Maven, base string) ([]FileRef, error) {
	refs := make([]FileRef, 0, len(m.Artifacts))
	for _, a := range m.Artifacts {
		ref, err := makeRef(m, base, base, a)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

// resolveSnapshot maps every artifact entry to its timestamped file name. A
// repository without version-level metadata (a plain file server) keeps the
// literal "-SNAPSHOT" name.
func (c *Client) resolveSnapshot(ctx context.Context, m *v2alpha1.Maven, base string, creds *credsv1.MavenCredentials) ([]FileRef, error) {
	u, err := versionMetadataURL(m, base)
	if err != nil {
		return nil, err
	}
	md, found, err := c.fetchMetadata(ctx, u, creds)
	if err != nil {
		return nil, err
	}
	refs := make([]FileRef, 0, len(m.Artifacts))
	for _, a := range m.Artifacts {
		fileVersion := base
		if found {
			fileVersion, err = snapshotFileVersion(md, base, a.Classifier, a.Extension)
			if err != nil {
				return nil, err
			}
		}
		ref, err := makeRef(m, base, fileVersion, a)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}
