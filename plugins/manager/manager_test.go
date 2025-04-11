package manager

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

func TestPluginManager(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	r := slog.New(slog.NewTextHandler(buf, nil))
	ctx := context.Background()
	pm := NewPluginManager(ctx, r)
	err := pm.RegisterPluginsAtLocation(ctx, "testdata")
	require.NoError(t, err)
	//access := &ctf.CommonTransportFormat{
	//	Type:     "CommonTransportFormat/v1",
	//	FilePath: "path/to/file",
	//}
	plugins, err := pm.GetWriteOCMRepoPlugin(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Remove("/tmp/ocm_plugin_generic.sock")
		_ = os.Remove("/tmp/ocm_plugin_ctf.sock")
	})
	require.Len(t, plugins, 1)
	p := plugins[0]
	require.NoError(t, p.Ping(ctx))
	require.NoError(t, pm.Shutdown(ctx))
}

func TestCallPluginForNoneExistingEndpoint(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	r := slog.New(slog.NewTextHandler(buf, nil))
	ctx := context.Background()
	pm := NewPluginManager(ctx, r)
	err := pm.RegisterPluginsAtLocation(ctx, "testdata")
	require.NoError(t, err)
	//access := &ctf.CommonTransportFormat{
	//	Type:     "CommonTransportFormat/v1",
	//	FilePath: "path/to/file",
	//}
	plugins, err := pm.GetReadResourceRepoPlugin(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, pm.Shutdown(ctx))
		_ = os.Remove("/tmp/ocm_plugin_generic.sock")
	})
	require.Len(t, plugins, 1)
	//p := plugins[0]

	//err = p.Call(ctx, "not-exist", http.MethodGet, nil, nil, nil, nil)
	//assert.ErrorContains(t, err, "page not found")
}

func TestIdleChecker(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	r := slog.New(slog.NewTextHandler(buf, nil))
	ctx := context.Background()
	pm := NewPluginManager(ctx, r)
	err := pm.RegisterPluginsAtLocation(ctx, "testdata", WithIdleTimeout(1100*time.Millisecond))
	require.NoError(t, err)
	//access := &ctf.CommonTransportFormat{
	//	Type:     "CommonTransportFormat/v1",
	//	FilePath: "path/to/file",
	//}
	plugins, err := pm.GetReadOCMRepoPlugin(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, pm.Shutdown(ctx))
		_ = os.Remove("/tmp/ocm_plugin_generic.sock")
	})
	require.Len(t, plugins, 1)

	// deliberate sleep because we MUST NOT ping the plugin. It literally needs to not do anything
	// if we ping it, that is a thing that it could run.
	time.Sleep(1200 * time.Millisecond)

	p := plugins[0]
	err = p.Ping(ctx)
	assert.ErrorContains(t, err, "connect: no such file or directory")
}

func TestBombarding(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	r := slog.New(slog.NewTextHandler(buf, nil))
	ctx := context.Background()
	pm := NewPluginManager(ctx, r)
	err := pm.RegisterPluginsAtLocation(ctx, "testdata")
	require.NoError(t, err)
	//access := &ctf.CommonTransportFormat{
	//	Type:     "CommonTransportFormat/v1",
	//	FilePath: "path/to/file",
	//}
	plugins, err := pm.GetReadResourceRepoPlugin(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, pm.Shutdown(ctx))
		_ = os.Remove("/tmp/ocm_plugin_generic.sock")
	})
	require.Len(t, plugins, 1)
	p := plugins[0]

	var wg sync.WaitGroup
	for range 500 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			require.NoError(t, p.Ping(ctx))
		}()
	}

	wg.Wait()
}

func TestShutdown(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	r := slog.New(slog.NewTextHandler(buf, nil))
	ctx := context.Background()
	pm := NewPluginManager(ctx, r)
	err := pm.RegisterPluginsAtLocation(ctx, "testdata")
	require.NoError(t, err)
	//access := &ctf.CommonTransportFormat{
	//	Type:     "CommonTransportFormat/v1",
	//	FilePath: "path/to/file",
	//}
	p1, err := pm.GetWriteOCMRepoPlugin(ctx, nil)
	require.NoError(t, err)
	//accessOCI := &oci.OCIArtifact{
	//	Type:           "OCIArtifact/v1",
	//	ImageReference: "ref",
	//}
	p2, err := pm.GetReadOCMRepoPlugin(ctx, nil)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = os.Remove("/tmp/ocm_plugin_generic.sock")
		_ = os.Remove("/tmp/ocm_plugin_ctf.sock")
	})

	require.NoError(t, pm.Shutdown(ctx))

	assert.Eventually(t, func() bool {
		err = p1[0].Ping(ctx)

		return err != nil
	}, 15*time.Second, 100*time.Millisecond)

	assert.Eventually(t, func() bool {
		err = p2[0].Ping(ctx)

		return err != nil
	}, 15*time.Second, 100*time.Millisecond)
}

func TestReturningPluginsForCapabilities(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	r := slog.New(slog.NewTextHandler(buf, nil))
	ctx := context.Background()
	pm := NewPluginManager(ctx, r)
	err := pm.RegisterPluginsAtLocation(ctx, "testdata")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, pm.Shutdown(ctx))
		_ = os.Remove("/tmp/ocm_plugin_generic.sock")
		_ = os.Remove("/tmp/ocm_plugin_ctf.sock")
	})
	//access := &ctf.CommonTransportFormat{
	//	Type:     "CommonTransportFormat/v1",
	//	FilePath: "path/to/file",
	//}
	_, err = fetchPlugin[GenericPluginContract](ctx, nil, "dummy", pm)

	assert.ErrorContains(t, err, "required capability not found in capabilities: dummy")
}

func TestDeduplicationOfPlugins(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	r := slog.New(slog.NewTextHandler(buf, nil))
	ctx := context.Background()
	pm := NewPluginManager(ctx, r)
	err := pm.RegisterPluginsAtLocation(ctx, "testdata")
	require.NoError(t, err)
	//access := &ctf.CommonTransportFormat{
	//	Type:     "CommonTransportFormat/v1",
	//	FilePath: "path/to/file",
	//}
	p1, err := pm.GetReadOCMRepoPlugin(ctx, nil)
	require.NoError(t, err)
	//accessOCI := &oci.OCIArtifact{
	//Type:           "OCIArtifact/v1",
	//ImageReference: "ref",
	//}
	p2, err := pm.GetReadOCMRepoPlugin(ctx, nil)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, pm.Shutdown(ctx))
		_ = os.Remove("/tmp/ocm_plugin_generic.sock")
	})

	assert.Equal(t, p1, p2)
}

func TestCanRetrieveSourceImplementedPlugins(t *testing.T) {
	RegisterPluginImplementationForTypeAndCapabilities(&ImplementedPlugin{
		Base: &MockPlugin{},
		Capabilities: []Capability{
			{
				Capability: ReadWriteComponentVersionRepositoryCapability,
			},
		},
		Type: "OCIArtifact/v1",
		ID:   "this-id-unique",
	})

	buf := bytes.NewBuffer(nil)
	r := slog.New(slog.NewTextHandler(buf, nil))
	ctx := context.Background()
	pm := NewPluginManager(ctx, r)
	//accessOCI := &oci.OCIArtifact{
	//	Type:           "OCIArtifact/v1",
	//	ImageReference: "ref",
	//}
	ps, err := pm.GetReadWriteComponentVersionRepositoryForType(ctx, nil)
	require.NoError(t, err)
	require.Len(t, ps, 1)
	p := ps[0]
	require.NoError(t, p.Ping(ctx))
}

func TestCanRetrieveSourceImplementedPluginsWithError(t *testing.T) {
	RegisterPluginImplementationForTypeAndCapabilities(&ImplementedPlugin{
		Base: &MockPlugin{
			err: errors.New("some error"),
		},
		Capabilities: []Capability{
			{
				Capability: ReadWriteComponentVersionRepositoryCapability,
			},
		},
		Type: "OCIArtifact/v1",
		ID:   "this-id-unique",
	})

	buf := bytes.NewBuffer(nil)
	r := slog.New(slog.NewTextHandler(buf, nil))
	ctx := context.Background()
	pm := NewPluginManager(ctx, r)
	//accessOCI := &oci.OCIArtifact{
	//	Type:           "OCIArtifact/v1",
	//	ImageReference: "ref",
	//}
	ps, err := pm.GetReadWriteComponentVersionRepositoryForType(ctx, nil)
	require.NoError(t, err)
	require.Len(t, ps, 1)
	p := ps[0]
	require.Error(t, p.Ping(ctx))
}

func TestValidationFailedOnAccessSpec(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	r := slog.New(slog.NewTextHandler(buf, nil))
	ctx := context.Background()
	pm := NewPluginManager(ctx, r)
	err := pm.RegisterPluginsAtLocation(ctx, "testdata")
	require.NoError(t, err)
	//access := &oci.OCIArtifact{
	//	Type: "CommonTransportFormat/v1",
	//}
	t.Cleanup(func() {
		require.NoError(t, pm.Shutdown(ctx))
		_ = os.Remove("/tmp/ocm_plugin_generic.sock")
		_ = os.Remove("/tmp/ocm_plugin_ctf.sock")
	})
	_, err = pm.GetWriteOCMRepoPlugin(ctx, nil)
	require.ErrorContains(t, err, "at '': missing property 'filePath'")
}

func TestValidationFailedOnlyIfSpecIsDefined(t *testing.T) {
	RegisterPluginImplementationForTypeAndCapabilities(&ImplementedPlugin{
		Base: &MockPlugin{},
		Capabilities: []Capability{
			{
				Capability: ReadWriteComponentVersionRepositoryCapability,
			},
		},
		Type: "OCIArtifact/v1",
		ID:   "this-id-unique",
	})

	buf := bytes.NewBuffer(nil)
	r := slog.New(slog.NewTextHandler(buf, nil))
	ctx := context.Background()
	pm := NewPluginManager(ctx, r)
	// this schema would be incorrect but the plugin doesn't set a schema.
	//accessOCI := &oci.OCIRegistry{
	//	Type: "OCIArtifact/v1",
	//}
	_, err := pm.GetReadWriteComponentVersionRepositoryForType(ctx, nil)
	require.NoError(t, err)
}

type MockPlugin struct {
	err error
}

func (m *MockPlugin) Ping(ctx context.Context) error {
	return m.err
}

func (m *MockPlugin) Start(ctx context.Context) error {
	return m.err
}

func (m *MockPlugin) Stop(ctx context.Context) error {
	return m.err
}

func (m *MockPlugin) GetComponentVersion(ctx context.Context, request GetComponentVersionRequest, credentials Attributes) (*descriptor.Descriptor, error) {
	return nil, m.err
}

func (m *MockPlugin) GetLocalResource(ctx context.Context, request GetLocalResourceRequest, credentials Attributes) error {
	return m.err
}

func (m *MockPlugin) AddLocalResource(ctx context.Context, request PostLocalResourceRequest, credentials Attributes) (*descriptor.Resource, error) {
	return nil, m.err
}

func (m *MockPlugin) AddComponentVersion(ctx context.Context, request PostComponentVersionRequest, credentials Attributes) error {
	return m.err
}

var _ ReadWriteRepositoryPluginContract = &MockPlugin{}
