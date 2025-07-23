package dir

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/input/dir"
	dirv1 "ocm.software/open-component-model/bindings/go/input/dir/spec/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/input"
)

func Register(inputRegistry *input.RepositoryRegistry) error {
	if err := RegisterDirInputV1(inputRegistry); err != nil {
		return err
	}
	return nil
}

func RegisterDirInputV1(inputRegistry *input.RepositoryRegistry) error {
	method := &dir.InputMethod{}
	spec := &dirv1.Dir{}
	if err := input.RegisterInternalResourceInputPlugin(dir.Scheme, inputRegistry, method, spec); err != nil {
		return fmt.Errorf("could not register dir resource input method: %w", err)
	}
	if err := input.RegisterInternalSourcePlugin(dir.Scheme, inputRegistry, method, spec); err != nil {
		return fmt.Errorf("could not register dir source input method: %w", err)
	}
	return nil
}
