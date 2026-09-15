package internal

import (
	"fmt"
	"strings"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// componentLabel renders a component as "<shortName>@<version>" for display purposes.
func componentLabel(c *descriptor.Component) string {
	shortName := c.Name
	if i := strings.LastIndex(c.Name, "/"); i >= 0 {
		shortName = c.Name[i+1:]
	}
	return fmt.Sprintf("%s@%s", shortName, c.Version)
}

// uploadLabel renders the label for an AddComponentVersion transformation. When a
// component is transferred to more than one target, the target index (0-based)
// disambiguates the labels, mirroring the T<idx> suffix on the transformation ID.
func uploadLabel(c *descriptor.Component, targetIdx, numTargets int) string {
	if numTargets > 1 {
		return fmt.Sprintf("%s [Upload → target %d]", componentLabel(c), targetIdx+1)
	}
	return fmt.Sprintf("%s [Upload]", componentLabel(c))
}

// resourceLabel renders the label for a resource-level transformation, e.g.
// "my-app@1.0.0 [Get icons]", op is the operation verb (Get, Add, Convert, Transfer)
func resourceLabel(c *descriptor.Component, op, resourceName string) string {
	return fmt.Sprintf("%s [%s %s]", componentLabel(c), op, resourceName)
}

// cleanupLabel is the label of the file-buffer cleanup transformation. There is
// exactly one cleanup transformation per graph, so no component context is needed.
const cleanupLabel = "Cleanup temp files"
