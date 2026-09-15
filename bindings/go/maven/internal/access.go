package internal

import (
	"fmt"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	mavenaccess "ocm.software/open-component-model/bindings/go/maven/spec/access"
	"ocm.software/open-component-model/bindings/go/maven/spec/access/v2alpha1"
)

// ConvertAccess decodes and validates the maven/v2alpha1 access of resource.
// The resource repository and the digest processor both start here, so a
// resource is rejected the same way whichever of them sees it first.
func ConvertAccess(resource *descriptor.Resource) (*v2alpha1.Maven, error) {
	if resource == nil || resource.Access == nil {
		return nil, fmt.Errorf("resource access is required")
	}
	var m v2alpha1.Maven
	if err := mavenaccess.Scheme.Convert(resource.Access, &m); err != nil {
		return nil, fmt.Errorf("error converting access to maven spec: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("invalid maven access: %w", err)
	}
	return &m, nil
}
