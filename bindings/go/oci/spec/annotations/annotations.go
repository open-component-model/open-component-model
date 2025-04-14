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
)

func NewComponentVersionAnnotation(component, version string) string {
	return fmt.Sprintf("%s/%s:%s", path.DefaultComponentDescriptorPath, component, version)
}

func ParseComponentVersionAnnotation(annotation string) (string, string, error) {
	prefix := path.DefaultComponentDescriptorPath + "/"
	if !strings.HasPrefix(annotation, prefix) {
		return "", "", fmt.Errorf("%q is not considered a valid %q annotation because of a bad prefix, expected %q", annotation, OCMComponentVersion, prefix)
	}
	postTrim := strings.TrimPrefix(annotation, prefix)
	split := strings.Split(postTrim, ":")
	if len(split) != 2 {
		return "", "", fmt.Errorf("%q is not considered a valid %q annotation", annotation, OCMComponentVersion)
	}
	candidate := split[0]
	version := split[1]
	if len(version) == 0 {
		return "", "", fmt.Errorf("version parsed from %q in %q annotation is empty but should not be", annotation, OCMComponentVersion)
	}

	return candidate, version, nil
}
