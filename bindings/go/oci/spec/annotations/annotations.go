package annotations

import (
	"fmt"
	"strings"

	"ocm.software/open-component-model/bindings/go/oci/spec/repository/path"
)

// Annotations for OCI Image Manifests
const (
	// OCMComponentVersion is an annotation that indicates the component version.
	// It is an annotation that can be used during referrer resolution to identify the component version.
	// Do not modify this otuside of the OCM binding library
	OCMComponentVersion = "software.ocm.componentversion"

	// OCMCreator is an annotation that indicates the creator of the component version.
	// It is used historically by the OCM CLI to indicate the creator of the component version.
	// It is usually only a meta information, and has no semantic meaning beyond identifying a creating
	// process or user agent. as such it CAN be correlated to a user agent header in http.
	OCMCreator = "software.ocm.creator"

	OCMComponentVersionAnnotationSeparator = ":"
)

func NewComponentVersionAnnotation(component, version string) string {
	return fmt.Sprintf("%s:%s", component, version)
}

// ParseComponentVersionAnnotation parses the component version annotation and returns the component name and version.
// It can identify a possible prefix of the annotation, which is the default component descriptor path and exclude
// it from the component name. (this can be present in CTFs.)
func ParseComponentVersionAnnotation(annotation string) (string, string, error) {
	prefix := path.DefaultComponentDescriptorPath + "/"
	if withoutPrefix := strings.TrimPrefix(annotation, prefix); withoutPrefix != annotation {
		// Remove the prefix if it exists, to allow for both prefixed and non-prefixed annotations.
		annotation = withoutPrefix
	}

	split := strings.SplitN(annotation, OCMComponentVersionAnnotationSeparator, 3)
	if len(split) != 2 {
		return "", "", fmt.Errorf("%q is not considered a valid %q annotation, not exactly 2 parts: %q", annotation, OCMComponentVersion, split)
	}
	candidate := split[0]
	version := split[1]
	if len(version) == 0 {
		return "", "", fmt.Errorf("version parsed from %q in %q annotation is empty but should not be", annotation, OCMComponentVersion)
	}

	return candidate, version, nil
}
