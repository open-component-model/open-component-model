package internal

import (
	"fmt"
	"strings"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// componentLabel renders a component as "<shortName>@<version>" for display purposes.
func componentLabel(c *descriptor.Component) string {
	shortName := c.Name
	if i := strings.LastIndex(c.Name, "/"); i >= 0 {
		shortName = c.Name[i+1:]
	}
	return fmt.Sprintf("%s@%s", shortName, c.Version)
}

// targetKind names the target repository type for display
func targetKind(toSpec runtime.Typed) string {
	if repo, err := convertToConcreteRepo(toSpec); err == nil {
		switch repo.(type) {
		case *ctfv1.Repository:
			return ctfv1.ShortType
		case *oci.Repository:
			return oci.ShortType
		}
	}
	return toSpec.GetType().GetName()
}

// uploadLabel renders the label for an AddComponentVersion transformation. When a
// component is transferred to more than one target, the target index (0-based)
// disambiguates the labels, mirroring the T<idx> suffix on the transformation ID.
func uploadLabel(c *descriptor.Component, toSpec runtime.Typed, targetIdx, numTargets int) string {
	if numTargets > 1 {
		return fmt.Sprintf("%s [Upload to %s \u2192 target %d]", componentLabel(c), targetKind(toSpec), targetIdx+1)
	}
	return fmt.Sprintf("%s [Upload to %s]", componentLabel(c), targetKind(toSpec))
}

// getLabel renders the label for a resource Get transformation,
// e.g. "my-app@1.0.0 [Get icons]".
func getLabel(c *descriptor.Component, resourceName string) string {
	return fmt.Sprintf("%s [Get %s]", componentLabel(c), resourceName)
}

// addLabel renders the label for a resource Add transformation,
// e.g. "my-app@1.0.0 [Add icons as LocalBlob to CTF]".
func addLabel(c *descriptor.Component, resourceName, kind string, toSpec runtime.Typed) string {
	return fmt.Sprintf("%s [Add %s as %s to %s]", componentLabel(c), resourceName, kind, targetKind(toSpec))
}

// convertLabel renders the label for the Helm chart conversion, which always produces
// an OCI artifact, e.g. "my-app@1.0.0 [Convert chart to OCIArtifact]".
func convertLabel(c *descriptor.Component, resourceName string) string {
	return fmt.Sprintf("%s [Convert %s to OCIArtifact]", componentLabel(c), resourceName)
}

// transferLabel renders the label for the OCI streaming path
// e.g. "my-app@1.0.0 [Transfer icons as OCIArtifact to OCI]".
func transferLabel(c *descriptor.Component, resourceName string, toSpec runtime.Typed) string {
	return fmt.Sprintf("%s [Transfer %s to %s]", componentLabel(c), resourceName, targetKind(toSpec))
}

// cleanupLabel is the label of the file-buffer cleanup transformation. There is
// exactly one cleanup transformation per graph, so no component context is needed.
const cleanupLabel = "Cleanup temp files"
