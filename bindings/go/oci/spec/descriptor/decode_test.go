package descriptor

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"sigs.k8s.io/yaml"
)

func createV2DescriptorYAML() []byte {
	desc := &v2.Descriptor{
		Meta: v2.Meta{
			Version: "v2",
		},
		Component: v2.Component{
			ComponentMeta: v2.ComponentMeta{
				ObjectMeta: v2.ObjectMeta{
					Name:    "test-component",
					Version: "1.0.0",
				},
			},
		},
	}
	data, _ := yaml.Marshal(desc)
	return data
}

func createV2DescriptorJSON() []byte {
	desc := &v2.Descriptor{
		Meta: v2.Meta{
			Version: "v2",
		},
		Component: v2.Component{
			ComponentMeta: v2.ComponentMeta{
				ObjectMeta: v2.ObjectMeta{
					Name:    "test-component",
					Version: "1.0.0",
				},
			},
		},
	}
	data, _ := json.Marshal(desc)
	return data
}

func createTarWithFile(name string, content []byte) *bytes.Buffer {
	buf := &bytes.Buffer{}
	tw := tar.NewWriter(buf)
	_ = tw.WriteHeader(&tar.Header{
		Name: name,
		Mode: 0644,
		Size: int64(len(content)),
	})
	if len(content) > 0 {
		_, _ = tw.Write(content)
	}
	_ = tw.Close()
	return buf
}

func TestSingleFileDecodeDescriptor_AllFormats(t *testing.T) {
	validYAML := createV2DescriptorYAML()
	validJSON := createV2DescriptorJSON()

	tests := []struct {
		name          string
		reader        *bytes.Buffer
		mediaType     string
		expectSuccess bool
		expectError   string
	}{
		// --- YAML-based ---
		{
			name:          "YAML decode success",
			reader:        bytes.NewBuffer(validYAML),
			mediaType:     MediaTypeComponentDescriptorYAML,
			expectSuccess: true,
		},
		{
			name:        "YAML decode failure invalid data",
			reader:      bytes.NewBufferString("invalid: yaml: : :"),
			mediaType:   MediaTypeComponentDescriptorYAML,
			expectError: "unmarshaling component descriptor",
		},

		// --- JSON-based ---
		{
			name:          "JSON decode success",
			reader:        bytes.NewBuffer(validJSON),
			mediaType:     MediaTypeComponentDescriptorJSON,
			expectSuccess: true,
		},
		{
			name:        "JSON decode failure invalid data",
			reader:      bytes.NewBufferString("{{"),
			mediaType:   MediaTypeComponentDescriptorJSON,
			expectError: "unmarshaling component descriptor",
		},

		// --- TAR-based ---
		{
			name:          "TAR decode success (v2 descriptor inside tar)",
			reader:        createTarWithFile(LegacyComponentDescriptorTarFileName, validYAML),
			mediaType:     MediaTypeLegacyComponentDescriptorTar,
			expectSuccess: true,
		},
		{
			name:        "TAR decode failure - missing descriptor file",
			reader:      createTarWithFile("other.txt", validYAML),
			mediaType:   MediaTypeLegacyComponentDescriptorTar,
			expectError: "no component descriptor found",
		},
		{
			name: "TAR decode failure - malformed descriptor content",
			reader: createTarWithFile(
				LegacyComponentDescriptorTarFileName,
				[]byte("not-a-valid-yaml: : :"),
			),
			mediaType:   MediaTypeLegacyComponentDescriptorTar,
			expectError: "unmarshaling component descriptor",
		},

		// --- Unsupported format ---
		{
			name:        "unsupported media type",
			reader:      bytes.NewBufferString("anything"),
			mediaType:   "application/unknown",
			expectError: "unsupported descriptor media type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desc, err := SingleFileDecodeDescriptor(tt.reader, tt.mediaType)
			if tt.expectError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectError)
				assert.Nil(t, desc)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, desc)
			assert.Equal(t, "test-component", desc.Component.ComponentMeta.ObjectMeta.Name)
			assert.Equal(t, "1.0.0", desc.Component.ComponentMeta.ObjectMeta.Version)
		})
	}
}

// Ensure descriptorFileFromTar skips unrelated files and reads correct file
func TestDescriptorFileFromTar(t *testing.T) {
	yamlData := createV2DescriptorYAML()
	buf := &bytes.Buffer{}
	tw := tar.NewWriter(buf)
	_ = tw.WriteHeader(&tar.Header{Name: "unrelated.txt", Mode: 0644, Size: 0})
	_ = tw.WriteHeader(&tar.Header{
		Name: LegacyComponentDescriptorTarFileName,
		Mode: 0644,
		Size: int64(len(yamlData)),
	})
	_, _ = tw.Write(yamlData)
	_ = tw.Close()

	r, err := descriptorFileFromTar(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	require.NotNil(t, r)

	data, err := io.ReadAll(r)
	require.NoError(t, err)
	var d v2.Descriptor
	require.NoError(t, yaml.Unmarshal(data, &d))
	assert.Equal(t, "test-component", d.Component.ComponentMeta.ObjectMeta.Name)
}

// Defensive test for empty TAR
func TestDescriptorFileFromTar_EmptyTar(t *testing.T) {
	buf := &bytes.Buffer{}
	tw := tar.NewWriter(buf)
	_ = tw.Close()

	r, err := descriptorFileFromTar(bytes.NewReader(buf.Bytes()))
	assert.Error(t, err)
	assert.Nil(t, r)
	assert.Contains(t, err.Error(), "no component descriptor found")
}

// Defensive test for broken TAR stream
func TestDescriptorFileFromTar_BrokenTar(t *testing.T) {
	broken := bytes.NewBufferString("not-a-tar")
	r, err := descriptorFileFromTar(broken)
	assert.Error(t, err)
	assert.Nil(t, r)
}
