package builtin_test

import (
	"context"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/internal/plugin/builtin"
)

// TestBuiltinSpecificationsAreValidatable guards the validation the constructor performs before it
// builds a component version: it can only report a faulty access or input specification of a
// built-in type if that type checks itself. A new built-in type therefore has to implement
// runtime.Validatable, or "ocm add cv" silently accepts anything for it.
func TestBuiltinSpecificationsAreValidatable(t *testing.T) {
	pm := manager.NewPluginManager(context.Background())
	require.NoError(t, builtin.Register(pm, &filesystemv1alpha1.Config{}, &httpv1alpha1.Config{}, slog.Default()))

	for _, tt := range []struct {
		name   string
		scheme *runtime.Scheme
	}{
		{"access", pm.ResourcePluginRegistry.ResourceScheme()},
		{"input", pm.InputRegistry.InputRepositoryScheme()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			types := slices.SortedFunc(maps.Keys(tt.scheme.GetTypes()), func(a, b runtime.Type) int {
				return strings.Compare(a.String(), b.String())
			})
			require.NotEmpty(t, types, "expected built-in %s types to be registered", tt.name)

			for _, typ := range types {
				t.Run(typ.String(), func(t *testing.T) {
					obj, err := tt.scheme.NewObject(typ)
					require.NoError(t, err)
					require.Implementsf(t, (*runtime.Validatable)(nil), obj,
						"%s type %q must implement runtime.Validatable so that the constructor can validate it", tt.name, typ)
				})
			}
		})
	}
}
