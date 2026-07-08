package internal

import (
	"fmt"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	accessspec "ocm.software/open-component-model/bindings/go/github/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/github/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// AccessFromResource converts the resource's access into a typed *v1.GitHub.
func AccessFromResource(resource *descriptor.Resource) (*v1.GitHub, error) {
	if resource == nil || resource.Access == nil {
		return nil, fmt.Errorf("resource access is required")
	}
	return convertAccess(resource.Access)
}

// AccessFromSource converts the source's access into a typed *v1.GitHub.
func AccessFromSource(source *descriptor.Source) (*v1.GitHub, error) {
	if source == nil || source.Access == nil {
		return nil, fmt.Errorf("source access is required")
	}
	return convertAccess(source.Access)
}

func convertAccess(access runtime.Typed) (*v1.GitHub, error) {
	var gitHub v1.GitHub
	if err := accessspec.Scheme.Convert(access, &gitHub); err != nil {
		return nil, fmt.Errorf("error converting access to github spec: %w", err)
	}
	return &gitHub, nil
}
