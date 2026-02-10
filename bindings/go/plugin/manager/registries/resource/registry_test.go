package resource

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	resourcev1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/resource/v1"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var dummyType = runtime.NewVersionedType(dummyv1.Type, dummyv1.Version)

func dummyCapability(schema []byte) resourcev1.CapabilitySpec {
	return resourcev1.CapabilitySpec{
		Type: runtime.NewUnversionedType(string(resourcev1.ResourceRepositoryPluginType)),
		SupportedAccessTypes: []mtypes.Type{{
			Type:       dummyType,
			JSONSchema: schema,
		}},
	}
}

func TestPluginFlow(t *testing.T) {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	path := filepath.Join("..", "..", "..", "tmp", "testdata", "test-plugin-resource")
	_, err := os.Stat(path)
	require.NoError(t, err, "test plugin not found, please build the plugin under tmp/testdata/test-plugin-resource first")
	ctx := context.Background()
	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	registry := NewResourceRegistry(ctx)
	config := mtypes.Config{
		ID:         "test-plugin-1-resource",
		Type:       mtypes.Socket,
		PluginType: resourcev1.ResourceRepositoryPluginType,
	}
	serialized, err := json.Marshal(config)
	require.NoError(t, err)

	pluginCmd := exec.CommandContext(ctx, path, "--config", string(serialized))
	pipe, err := pluginCmd.StdoutPipe()
	require.NoError(t, err)
	stderr, err := pluginCmd.StderrPipe()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Remove("/tmp/test-plugin-1-resource-plugin.socket")
		_ = pluginCmd.Process.Kill()
	})
	plugin := mtypes.Plugin{
		ID:     "test-plugin-1-resource",
		Path:   path,
		Stderr: stderr,
		Config: mtypes.Config{
			ID:         "test-plugin-1-resource",
			Type:       mtypes.Socket,
			PluginType: resourcev1.ResourceRepositoryPluginType,
		},
		Cmd:    pluginCmd,
		Stdout: pipe,
	}
	capability := dummyCapability([]byte(`{}`))
	require.NoError(t, registry.AddPlugin(plugin, &capability))
	retrievedPlugin, err := registry.GetResourcePlugin(ctx, &runtime.Raw{Type: dummyType})
	require.NoError(t, err)
	resource, err := retrievedPlugin.DownloadResource(ctx, &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "test-resource-1",
				Version: "0.1.0",
			},
		},
		Type:     "type",
		Relation: "local",
		Access: &runtime.Raw{
			Type: dummyType,
			Data: []byte(`{ "access": "v1" }`),
		},
	}, map[string]string{})
	require.NoError(t, err)
	reader, err := resource.ReadCloser()
	require.NoError(t, err)
	defer func() {
		_ = reader.Close()
	}()
	content, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Equal(t, "test-resource", string(content))
}

func TestRegisterInternalResourcePlugin(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)

	registry := NewResourceRegistry(ctx)
	plugin := &mockResourcePlugin{}
	r.NoError(registry.RegisterInternalResourcePlugin(plugin))

	tests := []struct {
		name       string
		accessSpec runtime.Typed
		err        require.ErrorAssertionFunc
	}{
		{
			name:       "prototype",
			accessSpec: &dummyv1.Repository{},
			err:        require.NoError,
		},
		{
			name: "canonical type",
			accessSpec: &runtime.Raw{
				Type: runtime.Type{
					Name:    dummyv1.Type,
					Version: dummyv1.Version,
				},
			},
			err: require.NoError,
		},
		{
			name: "short type",
			accessSpec: &runtime.Raw{
				Type: runtime.Type{
					Name:    dummyv1.ShortType,
					Version: dummyv1.Version,
				},
			},
			err: require.NoError,
		},
		{
			name: "invalid type",
			accessSpec: &runtime.Raw{
				Type: runtime.Type{
					Name:    "NonExistingType",
					Version: "v1",
				},
			},
			err: require.Error,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resourceRepository, err := registry.GetResourcePlugin(ctx, tc.accessSpec)
			tc.err(t, err)
			if err != nil {
				return
			}
			r.NotNil(resourceRepository)
		})
	}
}

type mockResourcePlugin struct {
	repository.ResourceRepository
}

var _ Repository = (*mockResourcePlugin)(nil)

func (m *mockResourcePlugin) GetResourceRepositoryScheme() *runtime.Scheme {
	return dummytype.Scheme
}

func (m *mockResourcePlugin) DownloadResource(ctx context.Context, res *descriptor.Resource, credentials map[string]string) (blob.ReadOnlyBlob, error) {
	return inmemory.New(strings.NewReader("test-resource")), nil
}

func (m *mockResourcePlugin) UploadResource(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob, credentials map[string]string) (*descriptor.Resource, error) {
	return res, nil
}

type mockResourcePluginWithErrors struct {
	getCredentialIdentityErr error
	uploadErr                error
	downloadErr              error
	credentialIdentity       runtime.Identity
	uploadResult             *descriptor.Resource
	downloadResult           blob.ReadOnlyBlob
}

var _ Repository = (*mockResourcePluginWithErrors)(nil)

func (m *mockResourcePluginWithErrors) GetResourceRepositoryScheme() *runtime.Scheme {
	return dummytype.Scheme
}

func (m *mockResourcePluginWithErrors) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (runtime.Identity, error) {
	if m.getCredentialIdentityErr != nil {
		return nil, m.getCredentialIdentityErr
	}
	return m.credentialIdentity, nil
}

func (m *mockResourcePluginWithErrors) DownloadResource(ctx context.Context, res *descriptor.Resource, credentials map[string]string) (blob.ReadOnlyBlob, error) {
	if m.downloadErr != nil {
		return nil, m.downloadErr
	}
	return m.downloadResult, nil
}

func (m *mockResourcePluginWithErrors) UploadResource(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob, credentials map[string]string) (*descriptor.Resource, error) {
	if m.uploadErr != nil {
		return nil, m.uploadErr
	}
	return m.uploadResult, nil
}

func TestGetResourceCredentialConsumerIdentity(t *testing.T) {
	ctx := context.Background()
	r := require.New(t)

	t.Run("success", func(t *testing.T) {
		registry := NewResourceRegistry(ctx)
		expectedIdentity := runtime.Identity{"key": "value"}
		plugin := &mockResourcePluginWithErrors{
			credentialIdentity: expectedIdentity,
		}
		r.NoError(registry.RegisterInternalResourcePlugin(plugin))

		resource := &descriptor.Resource{
			Access: &runtime.Raw{
				Type: runtime.Type{
					Name:    dummyv1.Type,
					Version: dummyv1.Version,
				},
			},
		}

		identity, err := registry.GetResourceCredentialConsumerIdentity(ctx, resource)
		r.NoError(err)
		r.Equal(expectedIdentity, identity)
	})

	t.Run("error", func(t *testing.T) {
		registry := NewResourceRegistry(ctx)
		expectedErr := errors.New("test error")
		plugin := &mockResourcePluginWithErrors{
			getCredentialIdentityErr: expectedErr,
		}
		r.NoError(registry.RegisterInternalResourcePlugin(plugin))

		resource := &descriptor.Resource{
			Access: &runtime.Raw{
				Type: runtime.Type{
					Name:    dummyv1.Type,
					Version: dummyv1.Version,
				},
			},
		}

		_, err := registry.GetResourceCredentialConsumerIdentity(ctx, resource)
		r.Error(err)
	})
}

func TestUploadResource(t *testing.T) {
	ctx := context.Background()
	r := require.New(t)

	t.Run("success", func(t *testing.T) {
		registry := NewResourceRegistry(ctx)
		expectedResource := &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name: "uploaded",
				},
			},
		}
		plugin := &mockResourcePluginWithErrors{
			uploadResult: expectedResource,
		}
		r.NoError(registry.RegisterInternalResourcePlugin(plugin))

		resource := &descriptor.Resource{
			Access: &runtime.Raw{
				Type: runtime.Type{
					Name:    dummyv1.Type,
					Version: dummyv1.Version,
				},
			},
		}

		content := inmemory.New(strings.NewReader("test content"))
		result, err := registry.UploadResource(ctx, resource, content, nil)
		r.NoError(err)
		r.Equal(expectedResource, result)
	})

	t.Run("error", func(t *testing.T) {
		registry := NewResourceRegistry(ctx)
		expectedErr := errors.New("upload failed")
		plugin := &mockResourcePluginWithErrors{
			uploadErr: expectedErr,
		}
		r.NoError(registry.RegisterInternalResourcePlugin(plugin))

		resource := &descriptor.Resource{
			Access: &runtime.Raw{
				Type: runtime.Type{
					Name:    dummyv1.Type,
					Version: dummyv1.Version,
				},
			},
		}

		content := inmemory.New(strings.NewReader("test"))
		_, err := registry.UploadResource(ctx, resource, content, nil)
		r.Error(err)
	})
}

func TestDownloadResource(t *testing.T) {
	ctx := context.Background()
	r := require.New(t)

	t.Run("success", func(t *testing.T) {
		registry := NewResourceRegistry(ctx)
		expectedContent := "downloaded content"
		plugin := &mockResourcePluginWithErrors{
			downloadResult: inmemory.New(strings.NewReader(expectedContent)),
		}
		r.NoError(registry.RegisterInternalResourcePlugin(plugin))

		resource := &descriptor.Resource{
			Access: &runtime.Raw{
				Type: runtime.Type{
					Name:    dummyv1.Type,
					Version: dummyv1.Version,
				},
			},
		}

		result, err := registry.DownloadResource(ctx, resource, nil)
		r.NoError(err)

		reader, err := result.ReadCloser()
		r.NoError(err)
		defer reader.Close()

		content, err := io.ReadAll(reader)
		r.NoError(err)
		r.Equal(expectedContent, string(content))
	})

	t.Run("error", func(t *testing.T) {
		registry := NewResourceRegistry(ctx)
		expectedErr := errors.New("download failed")
		plugin := &mockResourcePluginWithErrors{
			downloadErr: expectedErr,
		}
		r.NoError(registry.RegisterInternalResourcePlugin(plugin))

		resource := &descriptor.Resource{
			Access: &runtime.Raw{
				Type: runtime.Type{
					Name:    dummyv1.Type,
					Version: dummyv1.Version,
				},
			},
		}

		_, err := registry.DownloadResource(ctx, resource, nil)
		r.Error(err)
	})
}
