package oci_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content/memory"
	oci2 "oras.land/oras-go/v2/content/oci"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocmoci "ocm.software/open-component-model/bindings/go/oci/access"
	v1 "ocm.software/open-component-model/bindings/go/oci/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var testScheme = runtime.NewScheme()

func init() {
	ocmoci.MustAddToScheme(testScheme)
	v2.MustAddToScheme(testScheme)
}

func Repository(t *testing.T, options ...oci.RepositoryOption) *oci.Repository {
	repo, err := oci.NewRepository(options...)
	require.NoError(t, err, "Failed to create repository")
	return repo
}

// MockResolver implements the Resolver interface for testing
type MockResolver struct {
	store *memory.Store
}

func (m *MockResolver) StoreForReference(ctx context.Context, reference string) (oci.Store, error) {
	return m.store, nil
}

func (m *MockResolver) ComponentVersionReference(component, version string) string {
	return fmt.Sprintf("%s:%s", component, version)
}

func (m *MockResolver) TargetResourceReference(srcReference string) (string, error) {
	return fmt.Sprintf("test-registry.example.com/test-repo/%s", srcReference), nil
}

func TestRepository_AddComponentVersion(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	mockStore := memory.New()

	mockResolver := &MockResolver{store: mockStore}
	repo := Repository(t, oci.WithResolver(mockResolver))

	// Create a test component descriptor
	desc := &descriptor.Descriptor{
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-component",
					Version: "1.0.0",
				},
			},
		},
	}

	// Test adding component version
	err := repo.AddComponentVersion(ctx, desc)
	r.NoError(err, "Failed to add component version when it should succeed")

	resolved, err := mockStore.Resolve(ctx, mockResolver.ComponentVersionReference(desc.Component.Name, desc.Component.Version))
	r.NoError(err, "Failed to resolve component version after adding it")
	r.NotNil(resolved, "Resolved component version should not be nil")

	desc2, err := repo.GetComponentVersion(ctx, desc.Component.Name, desc.Component.Version)
	r.NoError(err, "Failed to get component version after adding it")

	r.NotNil(desc2, "Component version should not be nil after adding it")
	r.Equal(desc.Component.Name, desc2.Component.Name, "Component name should match")
}

func TestRepository_GetComponentVersion(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()
	mockStore := memory.New()
	mockResolver := &MockResolver{store: mockStore}
	repo := Repository(t, oci.WithResolver(mockResolver))

	// Test getting non-existent component version
	desc, err := repo.GetComponentVersion(ctx, "test-component", "1.0.0")
	r.Error(err, "Expected error when getting non-existent component version")
	r.Nil(desc, "Expected nil descriptor when getting non-existent component version")

	// Create a test component descriptor
	desc = &descriptor.Descriptor{
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-component",
					Version: "1.0.0",
				},
			},
		},
	}
	// Test adding component version
	err = repo.AddComponentVersion(ctx, desc)
	r.NoError(err, "Failed to add component version when it should succeed")

	desc, err = repo.GetComponentVersion(ctx, "test-component", "1.0.0")
	r.NoError(err, "Expected error when getting non-existent component version")
	r.NotNil(desc, "Expected nil descriptor when getting non-existent component version")

}

func TestRepository_GetLocalResource(t *testing.T) {
	// Create test resources with different configurations
	testCases := []struct {
		name           string
		resource       *descriptor.Resource
		content        []byte
		identity       map[string]string
		expectError    bool
		errorContains  string
		setupComponent bool
	}{
		{
			name: "non-existent component",
			resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-resource",
						Version: "1.0.0",
					},
				},
				Type: "test-type",
				Access: &runtime.Raw{
					Type: runtime.Type{
						Name: "localBlob",
					},
					Data: []byte(`{"localReference":"sha256:1234567890","mediaType":"application/octet-stream"}`),
				},
			},
			identity: map[string]string{
				"name":    "test-resource",
				"version": "1.0.0",
			},
			expectError:    true,
			setupComponent: false,
		},
		{
			name: "non-existent resource in existing component",
			resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-resource",
						Version: "1.0.0",
					},
				},
				Type: "test-type",
				Access: &runtime.Raw{
					Type: runtime.Type{
						Name: "localBlob",
					},
					Data: []byte(`{"localReference":"sha256:1234567890","mediaType":"application/octet-stream"}`),
				},
			},
			identity: map[string]string{
				"name":    "test-resource",
				"version": "1.0.0",
			},
			expectError:    true,
			setupComponent: true,
		},
		{
			name: "resource with platform-specific identity",
			resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "platform-resource",
						Version: "1.0.0",
					},
					ExtraIdentity: map[string]string{
						"architecture": "amd64",
						"os":           "linux",
					},
				},
				Type: "test-type",
				Access: &runtime.Raw{
					Type: runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
					Data: []byte(fmt.Sprintf(
						`{"type":"%s","localReference":"%s","mediaType":"application/octet-stream"}`,
						runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
						digest.FromString("platform specific content").String(),
					)),
				},
			},
			content: []byte("platform specific content"),
			identity: map[string]string{
				"name":         "platform-resource",
				"version":      "1.0.0",
				"architecture": "amd64",
				"os":           "linux",
			},
			expectError:    false,
			setupComponent: true,
		},
		{
			name: "resource with invalid identity",
			resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-resource",
						Version: "1.0.0",
					},
				},
				Type: "test-type",
				Access: &runtime.Raw{
					Type: runtime.Type{
						Name: "localBlob",
					},
					Data: []byte(`{"localReference":"sha256:1234567890","mediaType":"application/octet-stream"}`),
				},
			},
			identity: map[string]string{
				"invalid": "key",
			},
			expectError:    true,
			setupComponent: true,
		},
		{
			name: "resource with invalid identity",
			resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "platform-resource",
						Version: "1.0.0",
					},
					ExtraIdentity: map[string]string{
						"architecture": "amd64",
						"os":           "linux",
					},
				},
				Type: "test-type",
				Access: &runtime.Raw{
					Type: runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
					Data: []byte(fmt.Sprintf(
						`{"type":"%s","localReference":"%s","mediaType":"application/octet-stream"}`,
						runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
						digest.FromString("platform specific content").String(),
					)),
				},
			},
			identity: map[string]string{
				"name":    "test-resource",
				"version": "1.0.0",
			},
			expectError:    true,
			errorContains:  "found 0 candidates while looking for resource map[name:test-resource version:1.0.0], but expected exactly one",
			setupComponent: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			ctx := t.Context()
			mockStore := memory.New()
			mockResolver := &MockResolver{store: mockStore}
			repo := Repository(t, oci.WithResolver(mockResolver))

			// Create a test component descriptor
			desc := &descriptor.Descriptor{
				Component: descriptor.Component{
					ComponentMeta: descriptor.ComponentMeta{
						ObjectMeta: descriptor.ObjectMeta{
							Name:    "test-component",
							Version: "1.0.0",
						},
					},
				},
			}
			desc.Component.Resources = append(desc.Component.Resources, *tc.resource)

			// Add resource if content is provided
			if tc.content != nil {
				b := blob.NewDirectReadOnlyBlob(bytes.NewReader(tc.content))
				newRes, err := repo.AddLocalResource(ctx, desc.Component.Name, desc.Component.Version, tc.resource, b)
				r.NoError(err, "Failed to add test resource")
				r.NotNil(newRes, "Resource should not be nil after adding")
			}

			// Setup component if needed
			if tc.setupComponent {
				err := repo.AddComponentVersion(ctx, desc)
				r.NoError(err, "Failed to setup test component")
			}

			// Test getting the resource
			blob, err := repo.GetLocalResource(ctx, desc.Component.Name, desc.Component.Version, tc.identity)

			if tc.expectError {
				r.Error(err, "Expected error but got none")
				if tc.errorContains != "" {
					r.Contains(err.Error(), tc.errorContains, "Error message should contain expected text")
				}
				r.Nil(blob, "Blob should be nil when error occurs")
			} else {
				r.NoError(err, "Unexpected error when getting resource")
				r.NotNil(blob, "Blob should not be nil for successful retrieval")

				// Verify blob content if it was provided
				if tc.content != nil {
					reader, err := blob.ReadCloser()
					r.NoError(err, "Failed to get blob reader")
					defer reader.Close()

					content, err := io.ReadAll(reader)
					r.NoError(err, "Failed to read blob content")
					r.Equal(tc.content, content, "Blob content should match expected content")
				}
			}
		})
	}
}

func TestRepository_DownloadUploadResource(t *testing.T) {
	tests := []struct {
		name           string
		resource       *descriptor.Resource
		content        []byte
		wantErr        bool
		useLocalUpload bool
	}{
		{
			name: "resource with valid OCI image access",
			resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-resource",
						Version: "1.0.0",
					},
				},
				Type: "ociImage",
				Access: &v1.OCIImage{
					ImageReference: "test-image:latest",
				},
			},
			content:        []byte("test content"),
			wantErr:        false,
			useLocalUpload: false,
		},
		{
			name: "resource with valid OCI image layer access",
			resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test-layer-resource",
						Version: "1.0.0",
					},
				},
				Type: "ociImageLayer",
				Access: &runtime.Raw{
					Type: runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
					Data: []byte(fmt.Sprintf(
						`{"type":"%s","localReference":"%s","mediaType":"%s"}`,
						runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
						digest.FromString("test layer content").String(),
						ociImageSpecV1.MediaTypeImageLayer,
					)),
				},
			},
			content:        []byte("test layer content"),
			wantErr:        false,
			useLocalUpload: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			ctx := context.Background()

			// Create a mock resolver with a memory store
			mockStore := memory.New()
			mockResolver := &MockResolver{store: mockStore}

			// Create a repository with the mock resolver
			repo := Repository(t, oci.WithResolver(mockResolver))

			// Create a test component descriptor
			desc := &descriptor.Descriptor{
				Component: descriptor.Component{
					ComponentMeta: descriptor.ComponentMeta{
						ObjectMeta: descriptor.ObjectMeta{
							Name:    "test-component",
							Version: "1.0.0",
						},
					},
				},
			}

			// Add the resource to the component descriptor
			desc.Component.Resources = append(desc.Component.Resources, *tc.resource)

			// Create a blob with the test content
			b := blob.NewDirectReadOnlyBlob(bytes.NewReader(tc.content))

			if tc.useLocalUpload {
				// Use AddLocalResource for local uploads
				newRes, err := repo.AddLocalResource(ctx, desc.Component.Name, desc.Component.Version, tc.resource, b)
				r.NoError(err, "Failed to add local resource")
				r.NotNil(newRes, "Resource should not be nil after adding")

				// Add the component version
				err = repo.AddComponentVersion(ctx, desc)
				r.NoError(err, "Failed to add component version")

				// Try to get the resource back using GetResource with the global access
				downloadedRes, err := repo.DownloadResource(ctx, newRes)
				r.NoError(err, "Failed to get resource")
				r.NotNil(downloadedRes, "Downloaded resource should not be nil")

				// Verify the content
				reader, err := downloadedRes.ReadCloser()
				r.NoError(err, "Failed to get blob reader")
				defer reader.Close()

				downloadedContent, err := io.ReadAll(reader)
				r.NoError(err, "Failed to read blob content")
				r.Equal(tc.content, downloadedContent, "Downloaded content should match original content")
			} else {
				// Use UploadResource for global uploads
				// Create a temporary OCI store
				buf := bytes.NewBuffer(nil)
				store := tar.NewOCILayoutWriter(buf)

				// Create and write the OCI layout file
				layout := ociImageSpecV1.ImageLayout{
					Version: ociImageSpecV1.ImageLayoutVersion,
				}
				layoutBytes, err := json.Marshal(layout)
				r.NoError(err, "Failed to marshal layout")
				err = store.Push(ctx, ociImageSpecV1.Descriptor{
					MediaType: "application/vnd.oci.image.layout.v1+json",
					Size:      int64(len(layoutBytes)),
					Digest:    digest.FromBytes(layoutBytes),
				}, bytes.NewReader(layoutBytes))
				r.NoError(err, "Failed to push layout")

				// Create and write the index file
				index := ociImageSpecV1.Index{
					Versioned: specs.Versioned{
						SchemaVersion: 2,
					},
					Manifests: []ociImageSpecV1.Descriptor{},
				}
				indexBytes, err := json.Marshal(index)
				r.NoError(err, "Failed to marshal index")
				err = store.Push(ctx, ociImageSpecV1.Descriptor{
					MediaType: ociImageSpecV1.MediaTypeImageIndex,
					Size:      int64(len(indexBytes)),
					Digest:    digest.FromBytes(indexBytes),
				}, bytes.NewReader(indexBytes))
				r.NoError(err, "Failed to push index")

				// Create the manifest
				manifest := ociImageSpecV1.Manifest{
					Versioned: specs.Versioned{
						SchemaVersion: 2,
					},
					MediaType: ociImageSpecV1.MediaTypeImageManifest,
					Config: ociImageSpecV1.Descriptor{
						MediaType: "application/vnd.oci.image.config.v1+json",
						Size:      2,
						Digest:    digest.FromString("{}"),
					},
					Layers: []ociImageSpecV1.Descriptor{
						{
							MediaType: "application/vnd.oci.image.layer.v1.tar",
							Size:      int64(len(tc.content)),
							Digest:    digest.FromBytes(tc.content),
						},
					},
				}
				manifestBytes, err := json.Marshal(manifest)
				r.NoError(err, "Failed to marshal manifest")
				manifestDesc := ociImageSpecV1.Descriptor{
					MediaType: ociImageSpecV1.MediaTypeImageManifest,
					Digest:    digest.FromBytes(manifestBytes),
					Size:      int64(len(manifestBytes)),
				}

				// Push the manifest
				err = store.Push(ctx, manifestDesc, bytes.NewReader(manifestBytes))
				r.NoError(err, "Failed to push manifest")

				// Push the config
				configDesc := manifest.Config
				err = store.Push(ctx, configDesc, bytes.NewReader([]byte("{}")))
				r.NoError(err, "Failed to push config")

				// Push the content layer
				layerDesc := manifest.Layers[0]
				err = store.Push(ctx, layerDesc, bytes.NewReader(tc.content))
				r.NoError(err, "Failed to push layer")

				// Tag the manifest
				err = store.Tag(ctx, manifestDesc, "test-image:latest")
				r.NoError(err, "Failed to tag manifest")

				r.NoError(store.Close())

				// Store all necessary content in the mock store for lookup
				// Store the manifest
				err = mockStore.Push(ctx, manifestDesc, bytes.NewReader(manifestBytes))
				r.NoError(err, "Failed to push manifest to mock store")
				err = mockStore.Tag(ctx, manifestDesc, "test-image:latest")
				r.NoError(err, "Failed to tag manifest in mock store")

				// Store the config
				err = mockStore.Push(ctx, configDesc, bytes.NewReader([]byte("{}")))
				r.NoError(err, "Failed to push config to mock store")

				// Store the layer
				err = mockStore.Push(ctx, layerDesc, bytes.NewReader(tc.content))
				r.NoError(err, "Failed to push layer to mock store")

				// Upload the resource with the store content
				b := blob.NewDirectReadOnlyBlob(buf)
				r.NoError(repo.UploadResource(ctx, tc.resource.Access, tc.resource, b), "Failed to upload test resource")
				r.NotNil(tc.resource.Access, "Resource should not be nil after uploading")

				// Download the resource
				downloadedRes, err := repo.DownloadResource(ctx, tc.resource)
				if tc.wantErr {
					r.Error(err, "Expected error but got none")
					return
				}
				r.NoError(err, "Failed to download resource")
				r.NotNil(downloadedRes, "Downloaded resource should not be nil")

				// Verify the downloaded content
				reader, err := downloadedRes.ReadCloser()
				r.NoError(err, "Failed to get blob reader")
				defer reader.Close()

				// create tmp file
				out := filepath.Join(t.TempDir(), "test.tar")
				f, err := os.Create(out)
				r.NoError(err, "Failed to create temp file")
				t.Cleanup(func() {
					r.NoError(f.Close(), "Failed to close temp file")
				})
				unzipped, err := gzip.NewReader(reader)
				r.NoError(err, "Failed to create gzip reader")
				t.Cleanup(func() {
					r.NoError(unzipped.Close(), "Failed to close gzip reader")
				})
				// write the content to the file
				_, err = io.Copy(f, unzipped)
				r.NoError(err, "Failed to write downloaded content to file")

				strg, err := oci2.NewFromTar(ctx, out)
				r.NoError(err, "Failed to create new store from tar")
				// Verify the content
				_, err = strg.Resolve(ctx, "test-image:latest")
				r.NoError(err, "Failed to resolve test-image:latest")
			}
		})
	}
}

func TestRepository_AddLocalResourceOCILayout(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	// Create a mock resolver with a memory store
	mockStore := memory.New()
	mockResolver := &MockResolver{store: mockStore}

	// Create a repository with the mock resolver
	repo := Repository(t, oci.WithResolver(mockResolver))

	// Create a test component descriptor
	desc := &descriptor.Descriptor{
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-component",
					Version: "1.0.0",
				},
			},
		},
	}

	data, _ := createSingleLayerOCIImage(t, []byte("test content"), "test-image:latest")

	// Create a resource with OCI layout media type
	resource := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "test-resource",
				Version: "1.0.0",
			},
		},
		Type: "test-type",
		Access: &runtime.Raw{
			Type: runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
			Data: []byte(fmt.Sprintf(
				`{"type":"%s","localReference":"%s","mediaType":"%s"}`,
				runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
				digest.FromBytes(data).String(),
				oci.MediaTypeOCIImageLayoutV1+"+tar",
			)),
		},
	}

	// Add the resource to the component descriptor
	desc.Component.Resources = append(desc.Component.Resources, *resource)

	// Add the OCI layout as a local resource
	b := blob.NewDirectReadOnlyBlob(bytes.NewReader(data))
	newRes, err := repo.AddLocalResource(ctx, desc.Component.Name, desc.Component.Version, resource, b)
	r.NoError(err, "Failed to add OCI layout resource")
	r.NotNil(newRes, "Resource should not be nil after adding")

	// Add the component version
	err = repo.AddComponentVersion(ctx, desc)
	r.NoError(err, "Failed to add component version")

	// Try to get the resource back
	blob, err := repo.GetLocalResource(ctx, desc.Component.Name, desc.Component.Version, map[string]string{
		"name":    "test-resource",
		"version": "1.0.0",
	})
	r.NoError(err, "Failed to get OCI layout resource")
	r.NotNil(blob, "Blob should not be nil")

	layout, err := tar.ReadOCILayout(ctx, blob)
	r.NoError(err, "Failed to read OCI layout")
	t.Cleanup(func() {
		r.NoError(layout.Close(), "Failed to close OCI layout")
	})
	r.Len(layout.Index.Manifests, 1)
}

func TestRepository_AddLocalResourceOCIImageLayer(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	// Create a mock resolver with a memory store
	mockStore := memory.New()
	mockResolver := &MockResolver{store: mockStore}

	// Create a repository with the mock resolver
	repo := Repository(t, oci.WithResolver(mockResolver))

	// Create a test component descriptor
	desc := &descriptor.Descriptor{
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-component",
					Version: "1.0.0",
				},
			},
		},
	}

	content := []byte("test layer content")
	contentDigest := digest.FromBytes(content)

	// Create a resource with OCI image layer media type
	resource := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "test-layer-resource",
				Version: "1.0.0",
			},
		},
		Type: "ociImageLayer",
		Access: &runtime.Raw{
			Type: runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
			Data: []byte(fmt.Sprintf(
				`{"type":"%s","localReference":"%s","mediaType":"%s"}`,
				runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
				contentDigest.String(),
				ociImageSpecV1.MediaTypeImageLayer,
			)),
		},
	}

	// Add the resource to the component descriptor
	desc.Component.Resources = append(desc.Component.Resources, *resource)

	// Add the OCI image layer as a local resource
	b := blob.NewDirectReadOnlyBlob(bytes.NewReader(content))
	newRes, err := repo.AddLocalResource(ctx, desc.Component.Name, desc.Component.Version, resource, b)
	r.NoError(err, "Failed to add OCI image layer resource")
	r.NotNil(newRes, "Resource should not be nil after adding")

	// Add the component version
	err = repo.AddComponentVersion(ctx, desc)
	r.NoError(err, "Failed to add component version")

	// Try to get the resource back
	blob, err := repo.GetLocalResource(ctx, desc.Component.Name, desc.Component.Version, map[string]string{
		"name":    "test-layer-resource",
		"version": "1.0.0",
	})
	r.NoError(err, "Failed to get OCI image layer resource")
	r.NotNil(blob, "Blob should not be nil")

	// Verify the content
	reader, err := blob.ReadCloser()
	r.NoError(err, "Failed to get blob reader")
	defer reader.Close()

	downloadedContent, err := io.ReadAll(reader)
	r.NoError(err, "Failed to read blob content")
	r.Equal(content, downloadedContent, "Downloaded content should match original content")
}

func createSingleLayerOCIImage(t *testing.T, data []byte, ref string) ([]byte, *v1.OCIImage) {
	r := require.New(t)
	var buf bytes.Buffer
	w := tar.NewOCILayoutWriter(&buf)

	desc := ociImageSpecV1.Descriptor{}
	desc.Digest = digest.FromBytes(data)
	desc.Size = int64(len(data))
	desc.MediaType = ociImageSpecV1.MediaTypeImageLayer

	r.NoError(w.Push(t.Context(), desc, bytes.NewReader(data)))

	configRaw, err := json.Marshal(map[string]string{})
	r.NoError(err)
	configDesc := ociImageSpecV1.Descriptor{
		Digest:    digest.FromBytes(configRaw),
		Size:      int64(len(configRaw)),
		MediaType: "application/json",
	}
	r.NoError(w.Push(t.Context(), configDesc, bytes.NewReader(configRaw)))

	manifest := ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config:    configDesc,
		Layers: []ociImageSpecV1.Descriptor{
			desc,
		},
	}
	manifestRaw, err := json.Marshal(manifest)
	r.NoError(err)

	manifestDesc := ociImageSpecV1.Descriptor{
		Digest:    digest.FromBytes(manifestRaw),
		Size:      int64(len(manifestRaw)),
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
	}
	r.NoError(w.Push(t.Context(), manifestDesc, bytes.NewReader(manifestRaw)))

	r.NoError(w.Tag(t.Context(), manifestDesc, ref))

	r.NoError(w.Close())

	return buf.Bytes(), &v1.OCIImage{
		ImageReference: ref,
	}
}
