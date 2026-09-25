package test

import (
	"context"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
)

// StaticPluginManager returns a plugin manager func.
func StaticPluginManager(pm *manager.PluginManager) func(context.Context, *genericv1.Config) (*manager.PluginManager, error) {
	return func(context.Context, *genericv1.Config) (*manager.PluginManager, error) {
		return pm, nil
	}
}
