package internal

import (
	"fmt"
	"reflect"

	"ocm.software/open-component-model/bindings/go/git/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func AccessFrom(spec runtime.Typed) (*v1.Git, error) {
	if spec == nil || (reflect.ValueOf(spec).Kind() == reflect.Pointer && reflect.ValueOf(spec).IsNil()) {
		return nil, fmt.Errorf("git access is required")
	}

	if _, err := access.Scheme.NewObject(spec.GetType()); err != nil {
		return nil, fmt.Errorf("unsupported git access type: %w", err)
	}

	var result v1.Git
	if err := access.Scheme.Convert(spec, &result); err != nil {
		return nil, fmt.Errorf("cannot decode git access: %w", err)
	}

	if err := result.Validate(); err != nil {
		return nil, err
	}

	return &result, nil
}
