package file

import (
	"fmt"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/input/file"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/input"
)

func Register(inputRegistry *input.RepositoryRegistry, filesystemConfig *filesystemv1alpha1.Config) error {
	if err := RegisterFileInputV1(inputRegistry, filesystemConfig); err != nil {
		return err
	}
	return nil
}

func RegisterFileInputV1(inputRegistry *input.RepositoryRegistry, filesystemConfig *filesystemv1alpha1.Config) error {
	method := &file.InputMethod{
		WorkingDirectory: filesystemConfig.WorkingDirectory,
	}

	if err := inputRegistry.RegisterInternalResourceInputPlugin(method); err != nil {
		return fmt.Errorf("could not register file resource input method: %w", err)
	}
	if err := inputRegistry.RegisterInternalSourceInputPlugin(method); err != nil {
		return fmt.Errorf("could not register file source input method: %w", err)
	}
	return nil
}
