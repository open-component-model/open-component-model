package spec

import (
	"encoding/json"
	"fmt"
	"time"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// ConfigType defines the type identifier for HTTP client configurations.
	ConfigType = "http.config.ocm.software"
)

// Default transport timeouts applied when no configuration is provided.
var (
	DefaultTimeout               = Timeout(time.Duration(0))
	DefaultTCPDialTimeout        = Timeout(time.Duration(30 * time.Second))
	DefaultTCPKeepAlive          = Timeout(time.Duration(30 * time.Second))
	DefaultTLSHandshakeTimeout   = Timeout(time.Duration(10 * time.Second))
	DefaultResponseHeaderTimeout = Timeout(time.Duration(10 * time.Second))
	DefaultIdleConnTimeout       = Timeout(time.Duration(90 * time.Second))
)

// Timeout wraps time.Timeout to support JSON/YAML marshaling
// of human-readable duration strings (e.g. "30s", "5m", "1h").
// Use as a pointer (*Timeout) in config structs so that nil means "not set"
// and a zero value means "explicitly disabled".
type Timeout time.Duration

// NewTimeout creates a pointer to a Timeout set to the given time.Duration.
func NewTimeout(d time.Duration) *Timeout {
	v := Timeout(d)
	return &v
}

// Value returns the underlying time.Duration.
// Returns 0 when called on a nil pointer.
func (d *Timeout) Value() time.Duration {
	if d == nil {
		return 0
	}
	return time.Duration(*d)
}

func (d Timeout) String() string {
	return time.Duration(d).String()
}

func (d Timeout) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (d *Timeout) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return fmt.Errorf("failed to parse HTTP client timeout: %w", err)
	}

	switch value := v.(type) {
	case float64:
		*d = Timeout(time.Duration(value))
		return nil
	case string:
		tmp, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("invalid timeout value %q: must be a duration like 30s, 5m, or nanoseconds number: %w", value, err)
		}
		*d = Timeout(tmp)
		return nil
	default:
		return fmt.Errorf("timeout must be a duration string or nanoseconds number, got %T", v)
	}
}

var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&Config{},
		runtime.NewVersionedType(ConfigType, Version),
		runtime.NewUnversionedType(ConfigType),
	)
}

// Config represents the HTTP client configuration.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Config struct {
	// +ocm:jsonschema-gen:enum=http.config.ocm.software/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=http.config.ocm.software
	Type runtime.Type `json:"type"`

	// Timeout specifies the HTTP client timeout as a Go duration string
	// (e.g. "30s", "5m", "1h"). If not set, the timeout is disabled.
	Timeout *Timeout `json:"timeout,omitempty"`

	// ResponseHeaderTimeout specifies the time limit to wait for a server's response headers.
	// If not set, defaults to 10s.
	ResponseHeaderTimeout *Timeout `json:"responseHeaderTimeout,omitempty"`

	// IdleConnTimeout specifies the maximum time an idle connection remains open.
	// If not set, defaults to 90s.
	IdleConnTimeout *Timeout `json:"idleConnTimeout,omitempty"`

	// TCPDialTimeout specifies the time limit for establishing a TCP connection.
	// If not set, defaults to 30s.
	TCPDialTimeout *Timeout `json:"tcpDialTimeout,omitempty"`

	// TCPKeepAlive specifies the interval between TCP keep-alive probes.
	// If not set, defaults to 30s.
	TCPKeepAlive *Timeout `json:"tcpKeepAlive,omitempty"`

	// TLSHandshakeTimeout specifies the maximum time to wait for a TLS handshake.
	// If not set, defaults to 10s.
	TLSHandshakeTimeout *Timeout `json:"tlsHandshakeTimeout,omitempty"`
}

// LookupConfig creates an HTTP configuration from a central generic V1 config.
func LookupConfig(cfg *genericv1.Config) (*Config, error) {
	defaultCfg := &Config{
		Timeout:               &DefaultTimeout,
		TCPDialTimeout:        &DefaultTCPDialTimeout,
		TCPKeepAlive:          &DefaultTCPKeepAlive,
		TLSHandshakeTimeout:   &DefaultTLSHandshakeTimeout,
		ResponseHeaderTimeout: &DefaultResponseHeaderTimeout,
		IdleConnTimeout:       &DefaultIdleConnTimeout,
	}

	if cfg == nil {
		return defaultCfg, nil
	}

	filtered, err := genericv1.Filter(cfg, &genericv1.FilterOptions{
		ConfigTypes: []runtime.Type{
			runtime.NewVersionedType(ConfigType, Version),
			runtime.NewUnversionedType(ConfigType),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to filter config: %w", err)
	}

	cfgs := make([]*Config, 0, len(filtered.Configurations)+1)
	cfgs = append(cfgs, defaultCfg)
	for _, entry := range filtered.Configurations {
		var config Config
		if err := Scheme.Convert(entry, &config); err != nil {
			return nil, fmt.Errorf("failed to decode http config: %w", err)
		}
		cfgs = append(cfgs, &config)
	}

	return Merge(cfgs...), nil
}

// Merge merges the provided configs into a single config.
// The last explicitly set timeout wins.
func Merge(configs ...*Config) *Config {
	if len(configs) == 0 {
		return nil
	}

	merged := new(Config)
	_, _ = Scheme.DefaultType(merged)

	for _, config := range configs {
		if config.Timeout != nil {
			merged.Timeout = config.Timeout
		}
		if config.TCPDialTimeout != nil {
			merged.TCPDialTimeout = config.TCPDialTimeout
		}
		if config.TCPKeepAlive != nil {
			merged.TCPKeepAlive = config.TCPKeepAlive
		}
		if config.TLSHandshakeTimeout != nil {
			merged.TLSHandshakeTimeout = config.TLSHandshakeTimeout
		}
		if config.ResponseHeaderTimeout != nil {
			merged.ResponseHeaderTimeout = config.ResponseHeaderTimeout
		}
		if config.IdleConnTimeout != nil {
			merged.IdleConnTimeout = config.IdleConnTimeout
		}
	}

	return merged
}
