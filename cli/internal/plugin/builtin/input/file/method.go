package file

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/input/file"
	filev1 "ocm.software/open-component-model/bindings/go/input/file/spec/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/input"
)

func Register(inputRegistry *input.RepositoryRegistry) error {
	method := &file.InputMethod{}
	spec := &filev1.File{}
	if err := input.RegisterInternalResourceInputPlugin(file.Scheme, inputRegistry, method, spec); err != nil {
		return fmt.Errorf("could not register file resource input method: %w", err)
	}
	if err := input.RegisterInternalSourcePlugin(file.Scheme, inputRegistry, method, spec); err != nil {
		return fmt.Errorf("could not register file source input method: %w", err)
	}

	return nil
}
