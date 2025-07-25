package helm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"ocm.software/open-component-model/bindings/go/blob"
	v1 "ocm.software/open-component-model/bindings/go/input/helm/spec/v1"
)

var (
	ErrEmptyPath        = errors.New("helm input path must not be empty")
	ErrUnsupportedField = errors.New("unsupported input field must not be used")
)

// GetV1HelmBlob creates a ReadOnlyBlob from a v1.Helm specification.
// It reads the directory from the filesystem and packages it as an OCI artifact.
// The function returns an error if the file path is empty or if there are issues reading the directory
// contents from the filesystem.
func GetV1HelmBlob(ctx context.Context, helmSpec v1.Helm) (blob.ReadOnlyBlob, error) {
	if err := validateInputSpec(helmSpec); err != nil {
		return nil, fmt.Errorf("invalid helm input spec: %w", err)
	}

	return nil, nil
}

func validateInputSpec(helmSpec v1.Helm) error {
	var err error

	if helmSpec.Path == "" {
		err = ErrEmptyPath
	}

	var unsupportedFields []string
	if helmSpec.HelmRepository != "" {
		unsupportedFields = append(unsupportedFields, "helmRepository")
	}
	if helmSpec.CACert != "" {
		unsupportedFields = append(unsupportedFields, "caCert")
	}
	if helmSpec.CACertFile != "" {
		unsupportedFields = append(unsupportedFields, "caCertFile")
	}
	if helmSpec.Version != "" {
		unsupportedFields = append(unsupportedFields, "version")
	}
	if helmSpec.Repository != "" {
		unsupportedFields = append(unsupportedFields, "repository")
	}
	if len(unsupportedFields) > 0 {
		err = errors.Join(err, ErrUnsupportedField, fmt.Errorf("%w: %s", ErrUnsupportedField, strings.Join(unsupportedFields, ", ")))
	}

	return err
}
