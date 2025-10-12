package componentlister

import (
	"context"
	"fmt"

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

// ListComponents retrieves component names from the plug-in.
func (r *componentListerPluginConverter) ListComponents(ctx context.Context, last string, fn func(names []string) error) error {
	for {
		request := &v1.ListComponentsRequest[runtime.Typed]{
			Repository: r.repositorySpecification,
			Last:       last,
		}

		response, err := r.externalPlugin.ListComponents(ctx, request, r.credentials)
		if err != nil {
			return fmt.Errorf("plug-in returned error: %w", err)
		}

		err = fn(response.List)
		if err != nil {
			return fmt.Errorf("callback func returned error: %w", err)
		}

		if response.Header == nil || response.Header.Last == "" || len(response.List) == 0 {
			break
		}

		last = response.Header.Last
	}

	return nil
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
