package transform

import (
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

type PluginOrchestrator struct {
	pm *manager.PluginManager
}

func NewPluginTransformationOrchestrator(pm *manager.PluginManager) *PluginOrchestrator {
	return &PluginOrchestrator{pm: pm}
}

type Orchestrator interface {
	Orchestrate(config *v1alpha1.TransformationGraphDefinition) error
}

//
//func (p *PluginOrchestrator) Orchestrate(config *v1alpha1.TransformationGraphDefinition) error {
//
//}
