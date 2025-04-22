package contracts

import "context"

// EmptyBasePlugin can be used by internal implementations to skip having to implement
// the Ping method which will not be called anyway.
type EmptyBasePlugin struct{}

func (*EmptyBasePlugin) Ping(_ context.Context) error {
	return nil
}

// PluginBase is a capability shared by all plugins.
type PluginBase interface {
	// Ping makes sure the plugin is responsive.
	Ping(ctx context.Context) error
}
