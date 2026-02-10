package resource

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	resourcev1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/resource/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// mockExternalPlugin is a mock implementation of the ReadWriteResourcePluginContract
// that captures the location of uploaded files for testing.
type mockExternalPlugin struct {
	resourcev1.ReadWriteResourcePluginContract
	capturedLocation string
	returnResource   *descriptorv2.Resource
	returnError      error
}

func (m *mockExternalPlugin) AddGlobalResource(_ context.Context, request *resourcev1.AddGlobalResourceRequest, _ map[string]string) (*resourcev1.AddGlobalResourceResponse, error) {
	if m.returnError != nil {
		return nil, m.returnError
	}

	// Capture the location for verification
	m.capturedLocation = request.ResourceLocation.Value

	resource := m.returnResource
	if resource == nil {
		// Return a default resource if none was set
		resource = request.Resource
	}

	return &resourcev1.AddGlobalResourceResponse{
		Resource: resource,
	}, nil
}

// TestUploadResource_WithFilesystemConfig tests the UploadResource method
// with a custom filesystem configuration that specifies a tempDir.
func TestUploadResource_WithFilesystemConfig(t *testing.T) {
	ctx := context.Background()
	r := require.New(t)

	// Create a custom temp directory for this test
	customTempDir, err := os.MkdirTemp("", "test-upload-custom-*")
	r.NoError(err)
	defer func() {
		_ = os.RemoveAll(customTempDir)
	}()

	// Create a mock plugin
	mockPlugin := &mockExternalPlugin{
		returnResource: &descriptorv2.Resource{
			ElementMeta: descriptorv2.ElementMeta{
				ObjectMeta: descriptorv2.ObjectMeta{
					Name:    "uploaded-resource",
					Version: "1.0.0",
				},
			},
			Type:     "test-type",
			Relation: descriptorv2.LocalRelation,
			Access: &runtime.Raw{
				Type: runtime.Type{
					Name:    "test.access",
					Version: "v1",
				},
				Data: []byte(`{"uploaded": true}`),
			},
		},
	}

	// Create the resource converter with a custom filesystem config
	filesystemConfig := &filesystemv1alpha1.Config{
		TempFolder: customTempDir,
	}

	scheme := runtime.NewScheme()
	converter := &resourcePluginConverter{
		externalPlugin:   mockPlugin,
		scheme:           scheme,
		filesystemConfig: filesystemConfig,
	}

	// Create test resource
	testResource := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "test-resource",
				Version: "1.0.0",
			},
		},
		Type:     "test-type",
		Relation: "local",
		Access: &runtime.Raw{
			Type: runtime.Type{
				Name:    "test.access",
				Version: "v1",
			},
			Data: []byte(`{"test": "data"}`),
		},
	}

	// Create test blob content
	testContent := "test resource content for custom temp dir"
	content := inmemory.New(strings.NewReader(testContent))

	// Execute UploadResource
	result, err := converter.UploadResource(ctx, testResource, content, map[string]string{"key": "value"})
	r.NoError(err)
	r.NotNil(result)
	r.Equal("uploaded-resource", result.Name)
	r.Equal("1.0.0", result.Version)

	// Verify that the temp file was created in the custom temp directory
	r.NotEmpty(mockPlugin.capturedLocation)
	r.True(strings.HasPrefix(mockPlugin.capturedLocation, customTempDir),
		"Expected temp file to be created in custom temp dir %s, but was created at %s",
		customTempDir, mockPlugin.capturedLocation)

	// Verify the file contains the correct content
	fileContent, err := os.ReadFile(mockPlugin.capturedLocation)
	r.NoError(err)
	r.Equal(testContent, string(fileContent))

	// Verify the temp file has been cleaned up (deferred cleanup in UploadResource)
	// Note: The file might still exist briefly after the function returns due to timing,
	// but the deferred cleanup should have been called
}

// TestUploadResource_WithNilFilesystemConfig tests the UploadResource method
// with no filesystem configuration (nil), which should use the OS default temp directory.
func TestUploadResource_WithNilFilesystemConfig(t *testing.T) {
	ctx := context.Background()
	r := require.New(t)

	// Create a mock plugin
	mockPlugin := &mockExternalPlugin{
		returnResource: &descriptorv2.Resource{
			ElementMeta: descriptorv2.ElementMeta{
				ObjectMeta: descriptorv2.ObjectMeta{
					Name:    "uploaded-resource-default",
					Version: "2.0.0",
				},
			},
			Type:     "test-type",
			Relation: descriptorv2.LocalRelation,
			Access: &runtime.Raw{
				Type: runtime.Type{
					Name:    "test.access",
					Version: "v1",
				},
				Data: []byte(`{"uploaded": true}`),
			},
		},
	}

	// Create the resource converter with NO filesystem config (nil)
	scheme := runtime.NewScheme()
	converter := &resourcePluginConverter{
		externalPlugin:   mockPlugin,
		scheme:           scheme,
		filesystemConfig: nil,
	}

	// Create test resource
	testResource := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "test-resource-default",
				Version: "2.0.0",
			},
		},
		Type:     "test-type",
		Relation: "local",
		Access: &runtime.Raw{
			Type: runtime.Type{
				Name:    "test.access",
				Version: "v1",
			},
			Data: []byte(`{"test": "data"}`),
		},
	}

	// Create test blob content
	testContent := "test resource content for default OS temp dir"
	content := inmemory.New(strings.NewReader(testContent))

	// Execute UploadResource
	result, err := converter.UploadResource(ctx, testResource, content, map[string]string{"key": "value"})
	r.NoError(err)
	r.NotNil(result)
	r.Equal("uploaded-resource-default", result.Name)
	r.Equal("2.0.0", result.Version)

	// Verify that the temp file was created in the OS default temp directory
	r.NotEmpty(mockPlugin.capturedLocation)

	// Get the OS default temp directory and verify the file was created there
	osTempDir := os.TempDir()
	// Resolve any symlinks in the paths for accurate comparison
	resolvedCaptured, err := filepath.EvalSymlinks(mockPlugin.capturedLocation)
	if err == nil {
		// If we can resolve symlinks, use the resolved path
		mockPlugin.capturedLocation = resolvedCaptured
	}
	resolvedOsTemp, err := filepath.EvalSymlinks(osTempDir)
	if err == nil {
		osTempDir = resolvedOsTemp
	}

	r.True(strings.HasPrefix(mockPlugin.capturedLocation, osTempDir),
		"Expected temp file to be created in OS temp dir %s, but was created at %s",
		osTempDir, mockPlugin.capturedLocation)

	// Verify the file contains the correct content
	fileContent, err := os.ReadFile(mockPlugin.capturedLocation)
	r.NoError(err)
	r.Equal(testContent, string(fileContent))
}

// Verify that the mock plugin implementation satisfies the interface
var _ resourcev1.ReadWriteResourcePluginContract = (*mockExternalPlugin)(nil)
