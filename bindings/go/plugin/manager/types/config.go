package types

import (
	"time"
)

type ConnectionType string

const (
	Socket ConnectionType = "unix"
	TCP    ConnectionType = "tcp"
)

// Config defines information about the plugin. It contains what type of plugin we are dealing with,
// the id of the plugin and the connection type. The connection type is either unix ( preferred) or
// tcp based. The plugin will perform certain actions based on the connection type such as create the
// socket or designate a port of the manager to contact it on.
type Config struct {
	// ID defines a unique identifier of the plugin. This is ( currently ) derived from the file name of the plugin.
	ID string `json:"id"`
	// Type of the connection. Can be either unix or tcp.
	Type ConnectionType `json:"type"`
	// PluginType determines the type of the plugin: OCMComponentVersionRepository, Credential Provider, Transformation, etc.
	PluginType PluginType `json:"pluginType"`
	// IdleTimeout sets how long the plugin should sit around without work to do. If idle time is exceeded the plugin
	// is automatically terminated to not hog resources indefinitely.
	IdleTimeout *time.Duration `json:"idleTimeout,omitempty"`
}
