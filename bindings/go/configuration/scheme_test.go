package configuration_test

import (
	"testing"

	"ocm.software/open-component-model/bindings/go/configuration"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestRegister(t *testing.T) {
	s := runtime.NewScheme()
	if err := configuration.Register(s); err != nil {
		t.Fatal(err)
	}
}
