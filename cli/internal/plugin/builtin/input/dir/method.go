package dir

import (
	"fmt"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/input/dir"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/input"
)

func Register(inputRegistry *input.RepositoryRegistry, filesystemConfig *filesystemv1alpha1.Config) error {
	if err := RegisterDirInputV1(inputRegistry, filesystemConfig); err != nil {
		return err
	}
	return nil
}

func RegisterDirInputV1(inputRegistry *input.RepositoryRegistry, filesystemConfig *filesystemv1alpha1.Config) error {
	method := &dir.InputMethod{
		WorkingDirectory: filesystemConfig.WorkingDirectory,
	}
	if err := inputRegistry.RegisterInternalResourceInputPlugin(method); err != nil {
		return fmt.Errorf("could not register dir resource input method: %w", err)
	}
	if err := inputRegistry.RegisterInternalSourceInputPlugin(method); err != nil {
		return fmt.Errorf("could not register dir source input method: %w", err)
	}
	return nil
}
