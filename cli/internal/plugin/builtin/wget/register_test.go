package wget

import (
	"testing"

	"github.com/stretchr/testify/require"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/digestprocessor"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/input"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/resource"
	"ocm.software/open-component-model/bindings/go/runtime"
	wgetinput "ocm.software/open-component-model/bindings/go/wget/input"
	wgetv1 "ocm.software/open-component-model/bindings/go/wget/spec/input/v1"
)

// The registered input method must carry the configured temp folder. Without it the
// download silently lands in the OS temp directory, ignoring tempFolder of the
// filesystem configuration.
func TestRegister_InputMethodUsesConfiguredTempFolder(t *testing.T) {
	ctx := t.Context()
	tempFolder := t.TempDir()

	inputRegistry := input.NewInputRepositoryRegistry(ctx)
	require.NoError(t, Register(
		inputRegistry,
		resource.NewResourceRegistry(ctx),
		digestprocessor.NewDigestProcessorRegistry(ctx),
		credentialrepository.NewCredentialRepositoryRegistry(ctx),
		&httpv1alpha1.Config{},
		&filesystemv1alpha1.Config{TempFolder: tempFolder},
	))

	plugin, err := inputRegistry.GetResourceInputPlugin(ctx, &wgetv1.Wget{
		Type: runtime.NewVersionedType(wgetv1.Type, "v1"),
		URL:  "https://downloads.example.com/app.tar.gz",
	})
	require.NoError(t, err)

	method, ok := plugin.(*wgetinput.InputMethod)
	require.True(t, ok, "expected the built-in wget input method, got %T", plugin)
	require.Equal(t, tempFolder, method.TempFolder)
}
