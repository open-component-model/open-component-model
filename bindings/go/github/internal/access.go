package internal

import (
	"fmt"

	accessspec "ocm.software/open-component-model/bindings/go/github/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/github/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// AccessFrom converts a resource's or source's access into a typed *v1.GitHub.
func AccessFrom(access runtime.Typed) (*v1.GitHub, error) {
	if access == nil {
		return nil, fmt.Errorf("access is required")
	}
	var gitHub v1.GitHub
	if err := accessspec.Scheme.Convert(access, &gitHub); err != nil {
		return nil, fmt.Errorf("error converting access to github spec: %w", err)
	}
	return &gitHub, nil
}
