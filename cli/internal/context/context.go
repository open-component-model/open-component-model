package context

import (
	"context"
	"sync"

	"github.com/spf13/cobra"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
)

type ctxKey string

const key ctxKey = "ocm.software/open-component-model/cli/internal/context"

// Context is the OCM Command Line context.
// It contains pointers to centrally managed structures that are created
// once and used by many commands at once.
// Note that they integrate with context.Context, but are only passed as pointers
// so that access is always done at O(1) lookup time.
//
// The Context should only be used to transfer centrally passed struct pointers
type Context struct {
	mu sync.RWMutex

	// configuration is the central OCM Config configuration of the CLI.
	// It can be used to initialize other components and is always guaranteed to be
	// available first.
	// In case the config is not set, default values should be used.
	configuration *genericv1.Config

	// pluginManager is the central integration point into the OCM plugin system.
	// Any command that should be extendable by typed plugins should interact
	// with the implementations through the plugin system provided by [manager.PluginManager].
	pluginManager *manager.PluginManager

	// credentialGraph is the central credential system of OCM.
	// It is usually used to resolve credentials for plugins but can also
	// be queried directly for credentials.
	// It is expected that no plugin knows how to resolve credentials but only how to use them.
	// This is why the credential graph works by resolving a consumer identity
	// and then resolving the credentials for that identity.
	//
	// Once resolved they are passed to the corresponding plugin call.
	// Usually plugins can return correct consumer identities based on respective endpoints.
	credentialGraph credentials.GraphResolver

	// filesystemConfig is the central filesystem configuration for OCM.
	// It defines filesystem-related settings like temporary folder locations
	// that can be used by plugins and other components.
	filesystemConfig *filesystemv1alpha1.Config
}

// WithCredentialGraph creates a new context with the given credential graph.
// After this function is called, the credential graph can be retrieved from the context
// using [FromContext] and [Context.Configuration].
func WithCredentialGraph(ctx context.Context, graph credentials.GraphResolver) context.Context {
	ctx, ocmctx := retrieveOrCreateOCMContext(ctx)
	ocmctx.mu.Lock()
	defer ocmctx.mu.Unlock()
	ocmctx.credentialGraph = graph
	return ctx
}

// WithFilesystemConfig creates a new context with the given filesystem configuration.
// After this function is called, the filesystem configuration can be retrieved from the context
// using [FromContext] and [Context.FilesystemConfig].
func WithFilesystemConfig(ctx context.Context, cfg *filesystemv1alpha1.Config) context.Context {
	ctx, ocmctx := retrieveOrCreateOCMContext(ctx)
	ocmctx.mu.Lock()
	defer ocmctx.mu.Unlock()
	ocmctx.filesystemConfig = cfg
	return ctx
}

// WithPluginManager creates a new context with the given plugin manager.
// After this function is called, the plugin manager can be retrieved from the context
// using [FromContext] and [Context.PluginManager].
func WithPluginManager(ctx context.Context, pm *manager.PluginManager) context.Context {
	ctx, ocmctx := retrieveOrCreateOCMContext(ctx)
	ocmctx.mu.Lock()
	defer ocmctx.mu.Unlock()
	ocmctx.pluginManager = pm
	return ctx
}

// WithConfiguration creates a new context with the given configuration.
// After this function is called, the configuration can be retrieved from the context
// using [FromContext] and [Context.Configuration].
func WithConfiguration(ctx context.Context, cfg *genericv1.Config) context.Context {
	ctx, ocmctx := retrieveOrCreateOCMContext(ctx)
	ocmctx.mu.Lock()
	defer ocmctx.mu.Unlock()
	ocmctx.configuration = cfg
	return ctx
}

// Register registers the command to contain a new Context object, with
// the root command set as entrypoint.
// From this point on any call to the Context.RootCommand based on [cobra.Command.Context]
// will return this command.
func Register(cmd *cobra.Command) {
	ctx, ocmctx := retrieveOrCreateOCMContext(cmd.Context())
	ocmctx.mu.Lock()
	defer ocmctx.mu.Unlock()
	cmd.SetContext(ctx)
}

func (ctx *Context) PluginManager() *manager.PluginManager {
	if ctx == nil {
		return nil
	}
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.pluginManager
}

func (ctx *Context) CredentialGraph() credentials.GraphResolver {
	if ctx == nil {
		return nil
	}
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.credentialGraph
}

func (ctx *Context) Configuration() *genericv1.Config {
	if ctx == nil {
		return nil
	}
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.configuration
}

func (ctx *Context) FilesystemConfig() *filesystemv1alpha1.Config {
	if ctx == nil {
		return nil
	}
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.filesystemConfig
}

// FromContext retrieves the OCM context from the given context.
// If the OCM context does not exist, it returns nil.
// Within a command or subcommand which was registered with [Register],
// the context is always available and guaranteed to be present.
func FromContext(ctx context.Context) *Context {
	if ctx == nil {
		return nil
	}

	if v, ok := ctx.Value(key).(*Context); ok {
		return v
	}
	return nil
}

// WithContext creates a new context with the given OCM context.
func WithContext(ctx context.Context, c *Context) context.Context {
	if c == nil {
		return nil
	}
	return context.WithValue(ctx, key, c)
}

// retrieveOrCreateOCMContext retrieves the OCM context from the given context.
// If the OCM context does not exist, it creates a new one and returns it.
func retrieveOrCreateOCMContext(ctx context.Context) (context.Context, *Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	ocmctx := FromContext(ctx)
	if ocmctx == nil {
		ocmctx = &Context{}
		ctx = WithContext(ctx, ocmctx)
	}
	return ctx, ocmctx
}
