package manager

import (
	"time"
)

type ConnectionType string

const (
	Socket ConnectionType = "unix"
	TCP    ConnectionType = "tcp"
)

// Config that defines how a connection should be established.
type Config struct {
	// ID defines what ID the plugin should take.
	ID string `json:"id"`
	// Type of the connection.
	Type ConnectionType `json:"type"`
	// Location defines either a socket path or an HTTP url with port.
	Location string `json:"location"`
	// IdleTimeout sets how long the plugin should sit around without work to do.
	IdleTimeout *time.Duration `json:"idleTimeout,omitempty"`
	// AccessSchema represents the access spec that this plugin supports
	// AccessSchema is per type.
	// Schema is type/capability.
	AccessSpec map[string]map[string][]byte `json:"accessSpec"`
}
