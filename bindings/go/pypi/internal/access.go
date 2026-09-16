package internal

import (
	"fmt"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	pypiaccess "ocm.software/open-component-model/bindings/go/pypi/spec/access"
	"ocm.software/open-component-model/bindings/go/pypi/spec/access/v1alpha1"
)

// ConvertAccess decodes and validates the pypi/v1alpha1 access of resource.
// The resource repository and the digest processor both start here, so a
// resource is rejected the same way whichever of them sees it first.
func ConvertAccess(resource *descriptor.Resource) (*v1alpha1.PyPI, error) {
	if resource == nil || resource.Access == nil {
		return nil, fmt.Errorf("resource access is required")
	}
	var p v1alpha1.PyPI
	if err := pypiaccess.Scheme.Convert(resource.Access, &p); err != nil {
		return nil, fmt.Errorf("error converting access to pypi spec: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("invalid pypi access: %w", err)
	}
	return &p, nil
}
