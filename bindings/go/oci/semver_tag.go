package oci

import (
	"strings"

	"ocm.software/open-component-model/bindings/go/oci/internal/log"
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

// versionsWithPlus cache is used to keep track of version numbers, a warning has already been logged for.
// The intention is to avoid logging the same warning multiple times for the same version number.
var versionsWithPlus = make(map[string]string)

// LooseSemverToOCITag converts an OCM version to a valid OCI tag by replacing 1 possible occurrence of the '+' character.
// If there is more than one occurrence of the '+' character, the expectation is that this is caught later by the
// OCI tag validation.
// See also:
// - https://github.com/open-component-model/ocm-spec/blob/main/doc/04-extensions/03-storage-backends/oci.md#version-mapping
// - https://semver.org/#spec-item-10
func LooseSemverToOCITag(version string) string {
	idx := strings.LastIndex(version, plus)
	if idx == -1 {
		return version
	}
	if _, cached := versionsWithPlus[version]; !cached {
		versionsWithPlus[version] = ""
		log.Base().Warn("component version contains discouraged character", "version", version, "character", plus)
	}

	return version[:idx] + plusSubstitute + version[idx+len(plus):]
}
