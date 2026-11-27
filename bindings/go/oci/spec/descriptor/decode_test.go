package descriptor

import (
	"archive/tar"
	"bytes"
	"encoding/json/v2"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func createV2DescriptorYAML() []byte {
	desc := &v2.Descriptor{
		Meta: v2.Meta{
			Version: "v2",
		},
		Component: v2.Component{
			ComponentMeta: v2.ComponentMeta{
				ObjectMeta: v2.ObjectMeta{
					Name:    "github.com/ocm/test-component",
					Version: "1.0.0",
				},
			},
		},
	}
	data, _ := yaml.Marshal(desc)
	return data
}

func createV2DescriptorJSON(tb testing.TB) []byte {
	tb.Helper()
	desc := &v2.Descriptor{
		Meta: v2.Meta{
			Version: "v2",
		},
		Component: v2.Component{
			ComponentMeta: v2.ComponentMeta{
				ObjectMeta: v2.ObjectMeta{
					Name:    "github.com/ocm/test-component",
					Version: "1.0.0",
				},
			},
		},
	}
	data, err := json.Marshal(desc, runtime.JSONOptions)
	require.NoError(tb, err)
	return data
}

func createTarWithFile(name string, content []byte) *bytes.Buffer {
	buf := &bytes.Buffer{}
	tw := tar.NewWriter(buf)
	_ = tw.WriteHeader(&tar.Header{
		Name: name,
		Mode: 0o644,
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
	validJSON := createV2DescriptorJSON(t)

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
			expectError: "unmarshaling component descriptor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desc, err := SingleFileDecodeDescriptor(tt.reader, tt.mediaType, func(mediaType string, bytes []byte, obj interface{}) error {
				return yaml.Unmarshal(bytes, obj)
			})
			if tt.expectError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectError)
				assert.Nil(t, desc)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, desc)
			assert.Equal(t, "github.com/ocm/test-component", desc.Component.Name)
			assert.Equal(t, "1.0.0", desc.Component.Version)
		})
	}
}

// Ensure descriptorFileFromTar skips unrelated files and reads correct file
func TestDescriptorFileFromTar(t *testing.T) {
	yamlData := createV2DescriptorYAML()
	buf := &bytes.Buffer{}
	tw := tar.NewWriter(buf)
	_ = tw.WriteHeader(&tar.Header{Name: "unrelated.txt", Mode: 0o644, Size: 0})
	_ = tw.WriteHeader(&tar.Header{
		Name: LegacyComponentDescriptorTarFileName,
		Mode: 0o644,
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
	assert.Equal(t, "github.com/ocm/test-component", d.Component.Name)
}

// Defensive test for empty TAR
func TestDescriptorFileFromTar_EmptyTar(t *testing.T) {
	buf := &bytes.Buffer{}
	tw := tar.NewWriter(buf)
	_ = tw.Close()

	r, err := descriptorFileFromTar(bytes.NewReader(buf.Bytes()))
	require.Error(t, err)
	assert.Nil(t, r)
	assert.Contains(t, err.Error(), "no component descriptor found")
}

// Defensive test for broken TAR stream
func TestDescriptorFileFromTar_BrokenTar(t *testing.T) {
	broken := bytes.NewBufferString("not-a-tar")
	r, err := descriptorFileFromTar(broken)
	require.Error(t, err)
	assert.Nil(t, r)
}

// ocmv1V3Alpha1Descriptor is written by OCM v1 0.43.0 with "add cv --scheme ocm.software/v3alpha1".
func ocmv1V3Alpha1Descriptor(t *testing.T) []byte {
	t.Helper()
	doc, err := os.ReadFile(filepath.Join("testdata", "ocmv1-v3alpha1-component-descriptor.yaml"))
	require.NoError(t, err)
	return doc
}

// A v3alpha1 descriptor does not fail v2 decoding; it decodes into an empty descriptor.
// So the schema cannot be detected from a decoding error.
func TestV3Alpha1DecodesWithoutErrorAsEmptyV2(t *testing.T) {
	doc := ocmv1V3Alpha1Descriptor(t)
	for _, mt := range []string{MediaTypeComponentDescriptorYAML, MediaTypeLegacyComponentDescriptorYAML} {
		t.Run(mt, func(t *testing.T) {
			var desc v2.Descriptor
			require.NoError(t, DefaultDescriptorUnmarshalFunc(mt, doc, &desc))
			assert.Empty(t, desc.Meta.Version)
			assert.Empty(t, desc.Component.Name)
		})
	}
}

func TestSingleFileDecodeDescriptor_RejectsV3Alpha1(t *testing.T) {
	ocmv1 := ocmv1V3Alpha1Descriptor(t)
	schemaVersionOnly := []byte("meta:\n  schemaVersion: ocm.software/v3alpha1\n")
	tests := []struct {
		name      string
		reader    io.Reader
		mediaType string
	}{
		{"OCM v1 descriptor in legacy tar", createTarWithFile(LegacyComponentDescriptorTarFileName, ocmv1), MediaTypeLegacyComponentDescriptorTar},
		{"OCM v1 descriptor as yaml", bytes.NewReader(ocmv1), MediaTypeComponentDescriptorYAML},
		{"OCM v1 descriptor as json media type", bytes.NewReader(ocmv1), MediaTypeComponentDescriptorJSON},
		{"meta.schemaVersion in legacy tar", createTarWithFile(LegacyComponentDescriptorTarFileName, schemaVersionOnly), MediaTypeLegacyComponentDescriptorTar},
		{"meta.schemaVersion as yaml", bytes.NewReader(schemaVersionOnly), MediaTypeComponentDescriptorYAML},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := SingleFileDecodeDescriptor(tt.reader, tt.mediaType, DefaultDescriptorUnmarshalFunc)
			require.ErrorIs(t, err, ErrUnsupportedSchemaV3Alpha1)
			assert.Contains(t, err.Error(), "ocm.software/v3alpha1")
		})
	}
}

// A v2 descriptor sets meta.schemaVersion, so the raw document is not parsed again.
func TestRejectV3Alpha1_SkipsParseForV2(t *testing.T) {
	decoded := &v2.Descriptor{Meta: v2.Meta{Version: "v2"}}
	assert.NoError(t, rejectV3Alpha1(decoded, ocmv1V3Alpha1Descriptor(t)))
}
