package contracts

import "context"

// EmptyBasePlugin can be used by internal implementations to skip having to implement
// the Ping method which will not be called. Ping is only required for external plugins
// in which case it is used as a health check to see that a binary based plugin ( that uses
// a web server implementation ) is up and running. Internally, only the implementation is needed
// therefore, there is nothing to "ping".
type EmptyBasePlugin struct{}

func (*EmptyBasePlugin) Ping(_ context.Context) error {
	return nil
}

// PluginBase is a capability shared by all plugins.
// All plugins should implement this basic interface. It contains the Ping method which is used as a Health Check.
// This interface is also used during finding plugins. All plugins are collected as base plugins and then are
// type asserted into the right interface from there.
type PluginBase interface {
	// Ping makes sure the plugin is responsive.
	Ping(ctx context.Context) error
}
