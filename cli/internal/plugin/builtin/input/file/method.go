package file

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/input/file"
	filev1 "ocm.software/open-component-model/bindings/go/input/file/spec/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/input"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Register(inputRegistry *input.RepositoryRegistry, configs []*runtime.Raw) error {
	if err := RegisterFileInputV1(inputRegistry, configs); err != nil {
		return err
	}
	return nil
}

func RegisterFileInputV1(inputRegistry *input.RepositoryRegistry, _ []*runtime.Raw) error {
	method := &file.InputMethod{} // config is added in this
	spec := &filev1.File{}        // config is added in this
	if err := input.RegisterInternalResourceInputPlugin(file.Scheme, inputRegistry, method, spec); err != nil {
		return fmt.Errorf("could not register file resource input method: %w", err)
	}
	if err := input.RegisterInternalSourcePlugin(file.Scheme, inputRegistry, method, spec); err != nil {
		return fmt.Errorf("could not register file source input method: %w", err)
	}
	return nil
}
