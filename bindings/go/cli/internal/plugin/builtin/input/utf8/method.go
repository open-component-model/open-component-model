package utf8

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/input/utf8"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/input"
)

func Register(inputRegistry *input.RepositoryRegistry) error {
	if err := RegisterUTF8InputV1(inputRegistry); err != nil {
		return err
	}

	return nil
}

func RegisterUTF8InputV1(inputRegistry *input.RepositoryRegistry) error {
	method := &utf8.InputMethod{}
	if err := inputRegistry.RegisterInternalResourceInputPlugin(method); err != nil {
		return fmt.Errorf("could not register UTF-8 file resource input method: %w", err)
	}
	if err := inputRegistry.RegisterInternalSourceInputPlugin(method); err != nil {
		return fmt.Errorf("could not register UTF-8 file source input method: %w", err)
	}
	return nil
}
