package utf8

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/input/utf8"
	utf8v1 "ocm.software/open-component-model/bindings/go/input/utf8/spec/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/input"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Register(inputRegistry *input.RepositoryRegistry, configs []*runtime.Raw) error {
	if err := RegisterUTF8InputV1(inputRegistry); err != nil {
		return err
	}

	return nil
}

func RegisterUTF8InputV1(inputRegistry *input.RepositoryRegistry) error {
	method := &utf8.InputMethod{}
	spec := &utf8v1.UTF8{}
	if err := input.RegisterInternalResourceInputPlugin(utf8.Scheme, inputRegistry, method, spec); err != nil {
		return fmt.Errorf("could not register UTF-8 file resource input method: %w", err)
	}
	if err := input.RegisterInternalSourcePlugin(utf8.Scheme, inputRegistry, method, spec); err != nil {
		return fmt.Errorf("could not register UTF-8 file source input method: %w", err)
	}
	return nil
}
