package descriptor

import (
	"archive/tar"
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
	"sigs.k8s.io/yaml"
)

// createMinimalDescriptor returns a valid runtime.Descriptor.
func createMinimalDescriptor() *descriptor.Descriptor {
	return &descriptor.Descriptor{
		Meta: descriptor.Meta{
			Version: "v2",
		},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "encode-test",
					Version: "1.0.0",
				},
			},
			Provider: descriptor.Provider{
				Name: "test-provider",
			},
		},
	}
}

func decodeTar(t *testing.T, tarBuf *bytes.Buffer) *v2.Descriptor {
	tr := tar.NewReader(bytes.NewReader(tarBuf.Bytes()))
	header, err := tr.Next()
	require.NoError(t, err)
	assert.Equal(t, LegacyComponentDescriptorTarFileName, header.Name)
	data, err := io.ReadAll(tr)
	require.NoError(t, err)
	var v2desc v2.Descriptor
	require.NoError(t, yaml.Unmarshal(data, &v2desc))
	return &v2desc
}

func TestSingleFileEncodeDescriptor_AllFormats(t *testing.T) {
	scheme := runtime.NewScheme()
	desc := createMinimalDescriptor()

	tests := []struct {
		name          string
		mediaType     string
		validate      func(t *testing.T, buf *bytes.Buffer)
		expectedError string
	}{
		{
			name:      "YAML encoding success",
			mediaType: MediaTypeComponentDescriptorYAML,
			validate: func(t *testing.T, buf *bytes.Buffer) {
				var out v2.Descriptor
				require.NoError(t, yaml.Unmarshal(buf.Bytes(), &out))
				assert.Equal(t, "encode-test", out.Component.ComponentMeta.ObjectMeta.Name)
			},
		},
		{
			name:      "JSON encoding success",
			mediaType: MediaTypeComponentDescriptorJSON,
			validate: func(t *testing.T, buf *bytes.Buffer) {
				assert.Contains(t, buf.String(), "encode-test")
				assert.Contains(t, buf.String(), "\"name\"")
			},
		},
		{
			name:      "TAR encoding success",
			mediaType: MediaTypeLegacyComponentDescriptorTar,
			validate: func(t *testing.T, buf *bytes.Buffer) {
				v2desc := decodeTar(t, buf)
				assert.Equal(t, "encode-test", v2desc.Component.ComponentMeta.ObjectMeta.Name)
			},
		},
		{
			name:          "unsupported media type",
			mediaType:     "application/unsupported",
			expectedError: "unsupported descriptor media type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf, err := SingleFileEncodeDescriptor(scheme, desc, tt.mediaType)
			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
				assert.Nil(t, buf)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, buf)
			if tt.validate != nil {
				tt.validate(t, buf)
			}
		})
	}
}

func TestSingleFileEncodeDescriptor_ErrorPaths(t *testing.T) {
	scheme := runtime.NewScheme()

	// force ConvertToV2 to fail
	badDesc := &descriptor.Descriptor{}
	buf, err := SingleFileEncodeDescriptor(scheme, badDesc, MediaTypeComponentDescriptorYAML)
	assert.Error(t, err)
	assert.Nil(t, buf)
	assert.Contains(t, err.Error(), "convert component descriptor")

	// TAR encoding failure simulation by invalid header (impossible with real tar.Writer)
	// covered indirectly by conversion and marshalling errors
}
