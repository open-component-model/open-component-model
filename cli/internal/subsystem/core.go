package subsystem

import (
	"errors"

	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func RegisterPluginManager(pm *manager.PluginManager) error {
	return errors.Join(
		inputSystem.Scheme.RegisterScheme(pm.InputRegistry.InputRepositoryScheme()),
		ocmRepositorySystem.Scheme.RegisterScheme(pm.ComponentVersionRepositoryRegistry.GetComponentVersionRepositoryScheme()),
	)
}

var (
	inputSystem = &Subsystem{
		Name:        "input",
		Title:       "Resource/Source Input Methods",
		Description: "Input methods define how content is sourced and ingested into an OCM component version.",
		Scheme:      runtime.NewScheme(),
	}
	ocmRepositorySystem = &Subsystem{
		Name:        "ocm-repository",
		Title:       "OCM Component Version Repositories",
		Description: "Repositories for storing and managing OCM component versions.",
		Scheme:      runtime.NewScheme(),
	}
)

func init() {
	Register(inputSystem)
	Register(ocmRepositorySystem)
}
