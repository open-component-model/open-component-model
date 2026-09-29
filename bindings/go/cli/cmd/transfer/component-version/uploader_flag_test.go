package component_version

import (
	"testing"

	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

func TestUploaderEntry(t *testing.T) {
	decode := func(t *testing.T, raw *runtime.Raw) transferv1alpha1.UploaderConfig {
		t.Helper()
		uploaders, err := transferv1alpha1.LookupUploaderConfigs(&genericv1.Config{Configurations: []*runtime.Raw{raw}})
		require.NoError(t, err)
		require.Len(t, uploaders, 1)
		return uploaders[0]
	}

	t.Run("name only", func(t *testing.T) {
		r := require.New(t)
		raw, err := uploaderEntry("oci")
		r.NoError(err)
		r.Equal(runtime.NewVersionedType(transferv1alpha1.OCIUploaderConfigType, transferv1alpha1.Version), raw.Type)
		r.JSONEq(`{"type":"oci.uploader.transfer.config.ocm.software/v1alpha1"}`, string(raw.Data))
	})

	t.Run("name with match", func(t *testing.T) {
		r := require.New(t)
		raw, err := uploaderEntry(`reference=resource.name == "x"`)
		r.NoError(err)
		u, ok := decode(t, raw).(*transferv1alpha1.ReferenceUploaderConfig)
		r.True(ok)
		r.Equal(`resource.name == "x"`, u.Match)
	})

	t.Run("full type with match keeps the type", func(t *testing.T) {
		r := require.New(t)
		raw, err := uploaderEntry(`reference.uploader.transfer.config.ocm.software=resource.name == "x"`)
		r.NoError(err)
		r.Equal(runtime.NewUnversionedType(transferv1alpha1.ReferenceUploaderConfigType), raw.Type)
		r.IsType(&transferv1alpha1.ReferenceUploaderConfig{}, decode(t, raw))
	})

	t.Run("mapping", func(t *testing.T) {
		r := require.New(t)
		raw, err := uploaderEntry(`{type: http, match: 'resource.access.isType("Wget")', targetURL: 'https://t/x'}`)
		r.NoError(err)
		u, ok := decode(t, raw).(*transferv1alpha1.HTTPUploaderConfig)
		r.True(ok)
		r.Equal(`resource.access.isType("Wget")`, u.Match)
		r.Equal("https://t/x", u.TargetURL)
	})

	for _, tc := range []struct {
		value   string
		wantErr string
	}{
		{"reference=", "empty match"},
		{"", "empty value"},
		{"{match: x}", `missing "type"`},
		{"nope", `unknown uploader "nope"`},
	} {
		t.Run("error "+tc.wantErr, func(t *testing.T) {
			_, err := uploaderEntry(tc.value)
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}
