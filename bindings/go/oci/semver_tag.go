package oci

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	slogcontext "github.com/veqryn/slog-context"
	"oras.land/oras-go/v2/errdef"
)

// plusSubstitute is used to substitute the plus character ('+') in OCI tags.
// An OCM version is allowed to contain the plus character, but OCI tags do not allow it.
// Because the OCI tag of an artifact representing an OCM component is derived from the respective component
// version, this replacement is required. See also:
// - https://github.com/open-component-model/ocm-spec/blob/main/doc/04-extensions/03-storage-backends/oci.md#version-mapping
const (
	plusSubstitute = ".build-"
	plus           = "+"
)

// ociTagRegexp is the OCI/Docker tag grammar. The distribution and OCI specs use
// the same expression.
//
// Reference: https://github.com/opencontainers/distribution-spec/blob/v1.1.0/spec.md#pulling-manifests
var ociTagRegexp = regexp.MustCompile(`^[\w][\w.-]{0,127}$`)

// VersionToOCITag converts an OCM component version into an OCI tag.
//
// It applies the OCM spec's version mapping (replacing the first '+' with
// ".build-", which OCI tags disallow) and then validates the result against the
// OCI tag grammar. When the version does not map to a valid OCI tag it logs a
// loud warning and returns errdef.ErrInvalidReference, so callers that can fail
// (e.g. publishing a component version) surface a clear error instead of a
// downstream registry rejection.
//
// The '+'→".build-" substitution is lossy and not reversed on read; choose a
// versioning scheme whose versions are valid OCI tags to avoid divergence.
func VersionToOCITag(ctx context.Context, version string) (string, error) {
	tag := strings.Replace(version, plus, plusSubstitute, 1)
	if tag != version {
		slogcontext.Warn(ctx, "component version contains discouraged character", "version", version, "character", plus)
	}

	if reason := invalidOCITagReason(tag); reason != "" {
		slogcontext.Warn(ctx,
			"component version is not a valid OCI tag; storing under a registry may fail or silently diverge from the version",
			"version", version, "tag", tag, "reason", reason)
		return tag, fmt.Errorf("%w: component version %q does not map to a valid OCI tag %q: %s", errdef.ErrInvalidReference, version, tag, reason)
	}

	return tag, nil
}

// invalidOCITagReason returns a human-readable reason when tag violates the OCI
// tag grammar, or an empty string when tag is valid.
func invalidOCITagReason(tag string) string {
	if ociTagRegexp.MatchString(tag) {
		return ""
	}
	switch {
	case tag == "":
		return "tag is empty"
	case len(tag) > 128:
		return fmt.Sprintf("tag length %d exceeds the 128 character limit", len(tag))
	default:
		first := rune(tag[0])
		if !isWordChar(first) {
			return fmt.Sprintf("tag must start with an alphanumeric or underscore, got %q", first)
		}
		return "tag contains characters outside [A-Za-z0-9_.-]"
	}
}

func isWordChar(r rune) bool {
	return r == '_' ||
		(r >= '0' && r <= '9') ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z')
}
