package types

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"time"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// Duration is a [time.Duration] with the JSON representation the plugin
// protocol has always used: an integer number of nanoseconds.
//
// encoding/json/v2 refuses to marshal a [time.Duration] - it has no default
// representation and the format tag option is not enabled in Go 1.27 - so the
// wire format has to be spelled out here.
type Duration time.Duration

var _ interface {
	json.MarshalerTo
	json.UnmarshalerFrom
} = (*Duration)(nil)

// MarshalJSONTo writes the duration as an integer number of nanoseconds.
func (d Duration) MarshalJSONTo(enc *jsontext.Encoder) error {
	return enc.WriteToken(jsontext.Int(int64(d)))
}

// UnmarshalJSONFrom reads a duration from an integer number of nanoseconds.
func (d *Duration) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	var nanoseconds int64
	if err := json.UnmarshalDecode(dec, &nanoseconds); err != nil {
		return err
	}
	*d = Duration(nanoseconds)
	return nil
}

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
	IdleTimeout *Duration `json:"idleTimeout,omitempty"`
	// ConfigTypes defines the configurations that are sent to the plugin.
	ConfigTypes []*runtime.Raw `json:"configTypes,omitempty"`
}
