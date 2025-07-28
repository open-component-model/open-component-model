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

	// TODO:
	// - Walk the dir
	// - Check if the content is really a helm chart?
	// - Tar the contents
	// - Respect the provenance file sitting next to the chart, if exists
	// - Pack the tar and the .prov file in an OCI layout as per https://github.com/helm/community/blob/main/hips/hip-0006.md#2-support-for-provenance-files
	//   - Config layer, chart layer, optionally provenance layer, tagged with helm chart version
	// - Return the result as a ReadOnlyBlob OR ReadOnlyBlob and an Access (if Repository field is set) --> the latter in a separate PR
	// - External plug-in (CLI) is a separate PR, helm version should be the suffix of the plug-in version

	// Helm SDK can be used, e.g. for chart validation

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
