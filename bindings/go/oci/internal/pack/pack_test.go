package pack_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/file"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	resourceblob "ocm.software/open-component-model/bindings/go/oci/blob"
	. "ocm.software/open-component-model/bindings/go/oci/internal/pack"
	oci "ocm.software/open-component-model/bindings/go/oci/spec/access"
	accessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type testBlob struct {
	content   []byte
	mediaType string
	digest    digest.Digest
}

func (b *testBlob) ReadCloser() (io.ReadCloser, error) {
	if b.content == nil {
		return nil, errors.New("blob not found")
	}
	return io.NopCloser(bytes.NewReader(b.content)), nil
}

func (b *testBlob) Size() int64 {
	if b.content == nil {
		return blob.SizeUnknown
	}
	return int64(len(b.content))
}

func (b *testBlob) MediaType() (string, bool) {
	return b.mediaType, b.mediaType != ""
}

func (b *testBlob) Digest() (string, bool) {
	return b.digest.String(), b.digest != ""
}

func TestNewResourceBlobOCILayer(t *testing.T) {
	tests := []struct {
		name          string
		blob          *testBlob
		res           *descriptor.Resource
		opts          ResourceBlobOCILayerOptions
		expectedError string
	}{
		{
			name: "success with all fields provided",
			blob: &testBlob{
				content:   []byte("test content"),
				mediaType: "application/vnd.test",
				digest:    digest.FromBytes([]byte("test content")),
			},
			res: &descriptor.Resource{},
			opts: ResourceBlobOCILayerOptions{
				BlobMediaType: "application/vnd.test",
				BlobDigest:    digest.FromBytes([]byte("test content")),
			},
		},
		{
			name: "error on unknown size",
			blob: &testBlob{
				mediaType: "application/vnd.test",
				digest:    digest.FromBytes([]byte("test content")),
			},
			res: &descriptor.Resource{
				Size: blob.SizeUnknown,
			},
			opts: ResourceBlobOCILayerOptions{
				BlobMediaType: "application/vnd.test",
				BlobDigest:    digest.FromBytes([]byte("test content")),
			},
			expectedError: "blob size is unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resourceBlob, err := resourceblob.NewResourceBlob(tt.res, tt.blob)
			require.NoError(t, err)

			desc, err := NewResourceBlobOCILayer(resourceBlob, tt.opts)

			if tt.expectedError != "" {
				assert.ErrorContains(t, err, tt.expectedError)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.blob.mediaType, desc.MediaType)
			assert.Equal(t, tt.blob.digest, desc.Digest)
			assert.Equal(t, int64(len(tt.blob.content)), desc.Size)
		})
	}
}

func TestBlob(t *testing.T) {
	store, err := file.New(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, store.Close())
	})

	ctx := t.Context()
	content := []byte("test content")
	digest := digest.FromBytes(content)

	tests := []struct {
		name          string
		blob          *testBlob
		desc          ociImageSpecV1.Descriptor
		expectedError string
	}{
		{
			name: "successful push",
			blob: &testBlob{
				content:   content,
				mediaType: "application/vnd.test",
				digest:    digest,
			},
			desc: ociImageSpecV1.Descriptor{
				MediaType: "application/vnd.test",
				Digest:    digest,
				Size:      int64(len(content)),
			},
		},
		{
			name: "error on read closer failure",
			blob: &testBlob{
				content:   nil,
				mediaType: "application/vnd.test",
				digest:    digest,
			},
			desc: ociImageSpecV1.Descriptor{
				MediaType: "application/vnd.test",
				Digest:    digest,
				Size:      int64(len(content)),
			},
			expectedError: "failed to get blob reader",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Blob(ctx, store, tt.blob, tt.desc)

			if tt.expectedError != "" {
				assert.ErrorContains(t, err, tt.expectedError)
				return
			}

			assert.NoError(t, err)
		})
	}
}

func TestResourceBlob(t *testing.T) {
	ctx := t.Context()
	content := []byte("test content")
	digest := digest.FromBytes(content)

	tests := []struct {
		name              string
		blob              *testBlob
		resource          *descriptor.Resource
		opts              Options
		expectedError     string
		checkGlobalAccess func(t *testing.T, resource *descriptor.Resource)
	}{
		{
			name: "success with local blob access",
			blob: &testBlob{
				content:   content,
				mediaType: "application/vnd.test",
				digest:    digest,
			},
			resource: &descriptor.Resource{
				Access: &v2.LocalBlob{
					Type:           runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
					LocalReference: digest.String(),
					MediaType:      "application/vnd.test",
				},
			},
			opts: Options{
				AccessScheme:  runtime.NewScheme(),
				BaseReference: "test-ref",
			},
		},
		{
			name: "success with enforced global access",
			blob: &testBlob{
				content:   content,
				mediaType: "application/vnd.test",
				digest:    digest,
			},
			resource: &descriptor.Resource{
				Access: &v2.LocalBlob{
					Type:           runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
					LocalReference: digest.String(),
					MediaType:      "application/vnd.test",
				},
			},
			opts: Options{
				AccessScheme:        runtime.NewScheme(),
				BaseReference:       "test-ref",
				EnforceGlobalAccess: true,
			},
			checkGlobalAccess: func(t *testing.T, resource *descriptor.Resource) {
				access, ok := resource.Access.(*v2.LocalBlob)
				require.True(t, ok, "access should be of type LocalBlob")
				require.NotNil(t, access.GlobalAccess, "global access should be set")

				// Convert the global access to the correct type
				scheme := runtime.NewScheme()
				v2.MustAddToScheme(scheme)
				oci.MustAddToScheme(scheme)

				globalAccess, err := scheme.NewObject(access.GlobalAccess.GetType())
				require.NoError(t, err)
				require.NoError(t, scheme.Convert(access.GlobalAccess, globalAccess))

				switch typed := globalAccess.(type) {
				case *accessv1.OCIImageLayer:
					assert.Equal(t, fmt.Sprintf("test-ref@%s", digest.String()), typed.Reference)
					assert.Equal(t, "application/vnd.test", typed.MediaType)
					assert.Equal(t, digest, typed.Digest)
					assert.Equal(t, int64(len(content)), typed.Size)
				case *accessv1.OCIImage:
					assert.Equal(t, fmt.Sprintf("test-ref@%s", digest.String()), typed.ImageReference)
				default:
					t.Fatalf("unexpected global access type: %T", globalAccess)
				}
			},
		},
		{
			name: "error on empty access type",
			blob: &testBlob{
				content:   content,
				mediaType: "application/vnd.test",
				digest:    digest,
			},
			resource: &descriptor.Resource{
				Access: &v2.LocalBlob{
					Type: runtime.NewVersionedType("", ""),
				},
			},
			opts: Options{
				AccessScheme: runtime.NewScheme(),
			},
			expectedError: "resource access or access type is empty",
		},
		{
			name: "error on nil access",
			blob: &testBlob{
				content:   content,
				mediaType: "application/vnd.test",
				digest:    digest,
			},
			resource: &descriptor.Resource{
				Access: nil,
			},
			opts: Options{
				AccessScheme: runtime.NewScheme(),
			},
			expectedError: "resource access or access type is empty",
		},
		{
			name: "error on unsupported access type",
			blob: &testBlob{
				content:   content,
				mediaType: "application/vnd.test",
				digest:    digest,
			},
			resource: &descriptor.Resource{
				Access: &v2.LocalBlob{
					Type: runtime.NewVersionedType("unsupported", "v1"),
				},
			},
			opts: Options{
				AccessScheme: runtime.NewScheme(),
			},
			expectedError: "error creating resource access: unsupported type: unsupported/v1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := file.New(t.TempDir())
			require.NoError(t, err)
			t.Cleanup(func() {
				require.NoError(t, store.Close())
			})

			v2.MustAddToScheme(tt.opts.AccessScheme)
			oci.MustAddToScheme(tt.opts.AccessScheme)

			resourceBlob, err := resourceblob.NewResourceBlob(tt.resource, tt.blob)
			require.NoError(t, err)
			desc, err := ResourceBlob(ctx, store, resourceBlob, tt.opts)

			if tt.expectedError != "" {
				assert.ErrorContains(t, err, tt.expectedError)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.blob.mediaType, desc.MediaType)

			data, err := store.Fetch(t.Context(), desc)
			require.NoError(t, err)
			t.Cleanup(func() {
				require.NoError(t, data.Close())
			})

			layerData, err := io.ReadAll(data)
			require.NoError(t, err)
			assert.Equal(t, tt.blob.content, layerData)

			if tt.checkGlobalAccess != nil {
				tt.checkGlobalAccess(t, tt.resource)
			}
		})
	}
}

func TestResourceLocalBlob(t *testing.T) {
	store, err := file.New(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, store.Close())
	})

	content := []byte("test content")
	dig := digest.FromBytes(content)

	tests := []struct {
		name          string
		blob          *testBlob
		resource      *descriptor.Resource
		access        *descriptor.LocalBlob
		opts          Options
		expectedError string
	}{
		{
			name: "success with OCI layout media type",
			blob: &testBlob{
				content:   content,
				mediaType: "application/vnd.oci.image.layout.v1+tar",
				digest:    dig,
			},
			resource: &descriptor.Resource{},
			access: &descriptor.LocalBlob{
				MediaType: "application/vnd.oci.image.layout.v1+tar",
			},
			opts: Options{
				AccessScheme:  runtime.NewScheme(),
				BaseReference: "test-ref",
			},
		},
		{
			name: "success with single layer artifact",
			blob: &testBlob{
				content:   content,
				mediaType: "application/vnd.test",
				digest:    dig,
			},
			resource: &descriptor.Resource{},
			access: &descriptor.LocalBlob{
				MediaType: "application/vnd.test",
			},
			opts: Options{
				AccessScheme:  runtime.NewScheme(),
				BaseReference: "test-ref",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v2.MustAddToScheme(tt.opts.AccessScheme)
			oci.MustAddToScheme(tt.opts.AccessScheme)

			resourceBlob, err := resourceblob.NewResourceBlob(tt.resource, tt.blob)
			require.NoError(t, err)
			desc, err := ResourceLocalBlob(t.Context(), store, resourceBlob, tt.access, tt.opts)

			if tt.expectedError != "" {
				assert.ErrorContains(t, err, tt.expectedError)
				return
			}

			assert.NoError(t, err)
			data, err := store.Fetch(t.Context(), desc)
			require.NoError(t, err)
			t.Cleanup(func() {
				require.NoError(t, data.Close())
			})

			layerData, err := io.ReadAll(data)
			require.NoError(t, err)
			assert.Equal(t, tt.blob.content, layerData)
		})
	}
}

func TestResourceLocalBlobOCISingleLayerArtifact(t *testing.T) {
	store, err := file.New(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, store.Close())
	})

	content := []byte("test content")
	digest := digest.FromBytes(content)

	tests := []struct {
		name          string
		blob          *testBlob
		resource      *descriptor.Resource
		access        *descriptor.LocalBlob
		opts          Options
		expectedError string
	}{
		{
			name: "success with valid input",
			blob: &testBlob{
				content:   content,
				mediaType: "application/vnd.test",
				digest:    digest,
			},
			resource: &descriptor.Resource{},
			access: &descriptor.LocalBlob{
				MediaType:      "application/vnd.test",
				LocalReference: digest.String(),
			},
			opts: Options{
				AccessScheme:  runtime.NewScheme(),
				BaseReference: "test-ref",
			},
		},
		{
			name: "error on blob resource layer creation",
			blob: &testBlob{
				content:   nil,
				mediaType: "application/vnd.test",
				digest:    digest,
			},
			resource: &descriptor.Resource{
				Size: blob.SizeUnknown,
			},
			access: &descriptor.LocalBlob{
				MediaType:      "application/vnd.test",
				LocalReference: digest.String(),
			},
			opts: Options{
				AccessScheme:  runtime.NewScheme(),
				BaseReference: "test-ref",
			},
			expectedError: "failed to create resource layer based on blob",
		},
		{
			name: "error on push blob failure",
			blob: &testBlob{
				content:   content,
				mediaType: "application/vnd.test",
				digest:    digest,
			},
			resource: &descriptor.Resource{},
			access: &descriptor.LocalBlob{
				MediaType:      "application/vnd.test",
				LocalReference: digest.String(),
			},
			opts: Options{
				AccessScheme:  runtime.NewScheme(),
				BaseReference: "test-ref",
			},
			expectedError: "failed to push blob",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v2.MustAddToScheme(tt.opts.AccessScheme)
			oci.MustAddToScheme(tt.opts.AccessScheme)

			resourceBlob, err := resourceblob.NewResourceBlob(tt.resource, tt.blob)
			require.NoError(t, err)
			desc, err := ResourceLocalBlobOCILayer(t.Context(), store, resourceBlob, tt.access, tt.opts)

			if tt.expectedError != "" {
				assert.ErrorContains(t, err, tt.expectedError)
				return
			}

			data, err := store.Fetch(t.Context(), desc)
			require.NoError(t, err)
			t.Cleanup(func() {
				require.NoError(t, data.Close())
			})
			layerData, err := io.ReadAll(data)
			require.NoError(t, err)
			assert.Equal(t, tt.blob.content, layerData)
		})
	}
}

func TestResourceLocalBlobOCILayout(t *testing.T) {
	store, err := file.New(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, store.Close())
	})

	ctx := t.Context()
	var buf bytes.Buffer
	writer := tar.NewOCILayoutWriter(&buf)

	desc, err := oras.PackManifest(ctx, writer, oras.PackManifestVersion1_1, "application/custom", oras.PackManifestOptions{})
	require.NoError(t, err)

	require.NoError(t, writer.Close())
	ociLayout := buf.Bytes()

	tests := []struct {
		name          string
		blob          *testBlob
		resource      *descriptor.Resource
		opts          Options
		expectedError string
	}{
		{
			name: "success with valid input",
			blob: &testBlob{
				content:   ociLayout,
				mediaType: "application/vnd.oci.image.layout.v1+tar",
				digest:    digest.FromBytes(ociLayout),
			},
			resource: &descriptor.Resource{},
			opts: Options{
				AccessScheme:  runtime.NewScheme(),
				BaseReference: "test-ref",
			},
		},
		{
			name: "error on invalid OCI layout",
			blob: &testBlob{
				content:   []byte("invalid layout"),
				mediaType: "application/vnd.oci.image.layout.v1+tar",
				digest:    digest.FromBytes([]byte("invalid layout")),
			},
			resource: &descriptor.Resource{},
			opts: Options{
				AccessScheme:  runtime.NewScheme(),
				BaseReference: "test-ref",
			},
			expectedError: "failed to copy OCI layout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v2.MustAddToScheme(tt.opts.AccessScheme)
			oci.MustAddToScheme(tt.opts.AccessScheme)

			resourceBlob, err := resourceblob.NewResourceBlob(tt.resource, tt.blob)
			require.NoError(t, err)

			fromStore, err := ResourceLocalBlobOCILayout(ctx, store, resourceBlob, tt.opts)

			if tt.expectedError != "" {
				assert.ErrorContains(t, err, tt.expectedError)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, ociImageSpecV1.MediaTypeImageManifest, fromStore.MediaType)
			content.Equal(fromStore, desc)
		})
	}
}
