package componentlister

import (
	"context"
	"fmt"
	"log/slog"

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
			return err
		}

		err = fn(response.List)
		if err != nil {
			return fmt.Errorf("failed to list components, callback func returned error for list '%v': %w", response.List, err)
		}

		// Exit if returned header indicates that there are no more items to be returned.
		if response.Header == nil || response.Header.Last == "" {
			break
		}

		// Exit if it looks like we are in an infinite loop due to incorrect header.
		if len(response.List) == 0 {
			slog.DebugContext(ctx, "component list page returned by external plug-in is empty, but response header indicates more items", "header", response.Header)
			break
		}
		if response.Header.Last == last {
			slog.DebugContext(ctx, "same component list page has already been processed", "last", last)
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
