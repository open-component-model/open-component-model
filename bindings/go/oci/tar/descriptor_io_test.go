package tar

import (
	"archive/tar"
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestSingleFileTARDecodeV2Descriptor(t *testing.T) {
	tests := []struct {
		name          string
		setupTAR      func() *bytes.Buffer
		expectedError string
	}{
		{
			name: "successful decode",
			setupTAR: func() *bytes.Buffer {
				// Create a valid component descriptor YAML
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
				yamlData, _ := yaml.Marshal(desc)
				// Create TAR archive with the descriptor
				tarBuf := &bytes.Buffer{}
				tarWriter := tar.NewWriter(tarBuf)
				tarWriter.WriteHeader(&tar.Header{
					Name: "component-descriptor.yaml",
					Mode: 0644,
					Size: int64(len(yamlData)),
				})
				tarWriter.Write(yamlData)
				tarWriter.Close()
				return tarBuf
			},
		},
		{
			name: "missing descriptor file",
			setupTAR: func() *bytes.Buffer {
				buf := &bytes.Buffer{}
				tarWriter := tar.NewWriter(buf)
				tarWriter.WriteHeader(&tar.Header{
					Name: "other-file.txt",
					Mode: 0644,
					Size: 0,
				})
				tarWriter.Close()
				return buf
			},
			expectedError: "component-descriptor.yaml not found in archive",
		},
		{
			name: "multiple descriptor files",
			setupTAR: func() *bytes.Buffer {
				buf := &bytes.Buffer{}
				tarWriter := tar.NewWriter(buf)
				// Write first descriptor
				tarWriter.WriteHeader(&tar.Header{
					Name: "component-descriptor.yaml",
					Mode: 0644,
					Size: 0,
				})
				// Write second descriptor
				tarWriter.WriteHeader(&tar.Header{
					Name: "component-descriptor.yaml",
					Mode: 0644,
					Size: 0,
				})
				tarWriter.Close()
				return buf
			},
			expectedError: "multiple component-descriptor.yaml files found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tarData := tt.setupTAR()
			desc, err := SingleFileTARDecodeV2Descriptor(tarData)

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
				assert.Nil(t, desc)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, desc)
			assert.Equal(t, "test-component", desc.Component.ComponentMeta.ObjectMeta.Name)
			assert.Equal(t, "1.0.0", desc.Component.ComponentMeta.ObjectMeta.Version)
		})
	}
}

func TestSingleFileTAREncodeV2Descriptor(t *testing.T) {
	tests := []struct {
		name          string
		desc          *descriptor.Descriptor
		expectedError string
	}{
		{
			name: "successful encode",
			desc: &descriptor.Descriptor{
				Component: descriptor.Component{
					Provider: descriptor.Provider{
						Name: "test-provider",
					},
					ComponentMeta: descriptor.ComponentMeta{
						ObjectMeta: descriptor.ObjectMeta{
							Name:    "test-component",
							Version: "1.0.0",
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			encoding, buf, err := SingleFileTAREncodeV2Descriptor(scheme, tt.desc)

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
				assert.Empty(t, encoding)
				assert.Nil(t, buf)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, buf)
			assert.Equal(t, "+yaml+tar", encoding)

			// Verify the TAR content
			tarReader := tar.NewReader(buf)
			header, err := tarReader.Next()
			require.NoError(t, err)
			assert.Equal(t, "component-descriptor.yaml", header.Name)
		})
	}
}
