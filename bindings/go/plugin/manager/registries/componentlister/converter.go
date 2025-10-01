package componentlister

import (
	"context"

	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/componentlister/v1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var _ repository.ComponentLister = (*componentListerPluginConverter)(nil)

type componentListerPluginConverter struct {
	externalPlugin          v1.ComponentListerPluginContract[runtime.Typed]
	repositorySpecification runtime.Typed
	credentials             map[string]string
	scheme                  *runtime.Scheme
}

func (r *componentListerPluginConverter) ListComponents(ctx context.Context, last string, fn func(names []string) error) error {
	request := v1.ListComponentsRequest[runtime.Typed]{
		Repository: r.repositorySpecification,
		Last:       last,
	}

	page, err := r.externalPlugin.ListComponents(ctx, request, r.credentials)
	if err != nil {
		return err
	}

	return fn(page)
}

func (r *ComponentListerRegistry) externalToComponentListerPluginConverter(plugin v1.ComponentListerPluginContract[runtime.Typed],
	scheme *runtime.Scheme,
	repositorySpecification runtime.Typed,
	credentials map[string]string,
) *componentListerPluginConverter {
	return &componentListerPluginConverter{
		externalPlugin:          plugin,
		repositorySpecification: repositorySpecification,
		credentials:             credentials,
		scheme:                  scheme,
	}
}
