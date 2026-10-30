package s3

import (
	"testing"

	"github.com/stretchr/testify/require"

	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialtyperepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/digestprocessor"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/input"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/resource"
	s3input "ocm.software/open-component-model/bindings/go/s3/input"
	inputspec "ocm.software/open-component-model/bindings/go/s3/spec/input"
	v2 "ocm.software/open-component-model/bindings/go/s3/spec/input/v2"
)

// The registered input method must carry the configured HTTP settings; without them
// downloads silently fall back to the shared client's defaults.
func TestRegister_InputMethodUsesConfiguredHTTPConfig(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()
	httpConfig := &httpv1alpha1.Config{}

	inputRegistry := input.NewInputRepositoryRegistry(ctx)
	r.NoError(Register(
		inputRegistry,
		resource.NewResourceRegistry(ctx),
		digestprocessor.NewDigestProcessorRegistry(ctx),
		credentialtyperepository.NewCredentialTypeRegistry(ctx),
		httpConfig,
	))

	plugin, err := inputRegistry.GetResourceInputPlugin(ctx, &v2.S3{
		Type:       inputspec.V2VersionedType,
		BucketName: "my-bucket",
		ObjectKey:  "path/to/blob.txt",
	})
	r.NoError(err)

	method, ok := plugin.(*s3input.InputMethod)
	r.True(ok, "expected the built-in s3 input method, got %T", plugin)
	r.Same(httpConfig, method.HTTPConfig)
}
