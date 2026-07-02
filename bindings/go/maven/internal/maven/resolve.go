package maven

import (
	"context"
	"fmt"
	"net/http"

	v1 "ocm.software/open-component-model/bindings/go/maven/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// fetchMetadata GETs and parses a maven-metadata.xml. found is false on 404.
func (c *Client) fetchMetadata(ctx context.Context, url string, creds runtime.Typed) (md *metadata, found bool, err error) {
	body, status, err := c.Get(ctx, url, creds)
	if err != nil {
		return nil, false, fmt.Errorf("error fetching %q: %w", url, err)
	}
	if status == http.StatusNotFound {
		return nil, false, nil
	}
	if status != http.StatusOK {
		return nil, false, fmt.Errorf("error fetching %q: unexpected status %d", url, status)
	}
	md, err = parseMetadata(body)
	if err != nil {
		return nil, false, err
	}
	return md, true, nil
}

// Resolve returns the deterministic set of files to download for m.
func (c *Client) Resolve(ctx context.Context, m *v1.Maven, creds runtime.Typed) ([]FileRef, error) {
	base := m.Version
	if base == "LATEST" || base == "RELEASE" {
		u, err := artifactMetadataURL(m)
		if err != nil {
			return nil, err
		}
		md, found, err := c.fetchMetadata(ctx, u, creds)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("no maven-metadata.xml to resolve %q for %s:%s", m.Version, m.GroupID, m.ArtifactID)
		}
		if base == "LATEST" {
			base = md.Versioning.Latest
		} else {
			base = md.Versioning.Release
		}
		if base == "" {
			return nil, fmt.Errorf("maven-metadata.xml has no %q version for %s:%s", m.Version, m.GroupID, m.ArtifactID)
		}
	}

	if !IsSnapshot(base) {
		return resolveRelease(m, base)
	}
	return c.resolveSnapshot(ctx, m, base, creds)
}

// resolveRelease is pure: releases expose no listing.
func resolveRelease(m *v1.Maven, base string) ([]FileRef, error) {
	switch {
	case IsFile(m):
		ref, err := singleRef(m, base, base, *m.Classifier, *m.Extension)
		if err != nil {
			return nil, err
		}
		return []FileRef{ref}, nil
	case IsPackage(m):
		ref, err := singleRef(m, base, base, "", DefaultExtension)
		if err != nil {
			return nil, err
		}
		return []FileRef{ref}, nil
	default:
		return nil, fmt.Errorf("release %s:%s:%s cannot be enumerated deterministically; set both classifier and extension", m.GroupID, m.ArtifactID, base)
	}
}

func (c *Client) resolveSnapshot(ctx context.Context, m *v1.Maven, base string, creds runtime.Typed) ([]FileRef, error) {
	u, err := versionMetadataURL(m, base)
	if err != nil {
		return nil, err
	}
	md, found, err := c.fetchMetadata(ctx, u, creds)
	if err != nil {
		return nil, err
	}

	if IsFile(m) {
		fileVersion := base
		if found {
			fileVersion, err = snapshotFileVersion(md, base, *m.Classifier, *m.Extension)
			if err != nil {
				return nil, err
			}
		}
		ref, err := singleRef(m, base, fileVersion, *m.Classifier, *m.Extension)
		if err != nil {
			return nil, err
		}
		return []FileRef{ref}, nil
	}

	if !found {
		return nil, fmt.Errorf("snapshot %s:%s:%s has no maven-metadata.xml; cannot enumerate", m.GroupID, m.ArtifactID, base)
	}
	matches := filterSnapshotVersions(m, md.Versioning.SnapshotVersions)
	if len(matches) == 0 {
		return nil, fmt.Errorf("no snapshot artifacts match classifier=%v extension=%v for %s:%s:%s", stringOrAny(m.Classifier), stringOrAny(m.Extension), m.GroupID, m.ArtifactID, base)
	}
	refs := make([]FileRef, 0, len(matches))
	for _, sv := range matches {
		if err := validateSnapshotVersionValues(sv); err != nil {
			return nil, err
		}
		ref, err := makeRef(m, base, sv.Value, sv.Classifier, sv.Extension)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	// A pattern selector that happens to match exactly one file is still a
	// single-file result: apply the same MediaType override singleRef would.
	if len(refs) == 1 && m.MediaType != nil && *m.MediaType != "" {
		refs[0].MediaType = *m.MediaType
	}
	return refs, nil
}

// singleRef builds a ref and applies the MediaType override (single-file only).
func singleRef(m *v1.Maven, dirVersion, fileVersion, classifier, extension string) (FileRef, error) {
	ref, err := makeRef(m, dirVersion, fileVersion, classifier, extension)
	if err != nil {
		return FileRef{}, err
	}
	if m.MediaType != nil && *m.MediaType != "" {
		ref.MediaType = *m.MediaType
	}
	return ref, nil
}

// stringOrAny renders a selector pointer for error messages: nil matches anything.
func stringOrAny(s *string) string {
	if s == nil {
		return "<any>"
	}
	return *s
}
