package v1alpha1

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

// DefaultTimeout is the default HTTP client timeout used when no
// configuration is provided.
const DefaultTimeout = Timeout(30 * time.Second)

// Timeout wraps time.Duration to support JSON/YAML marshaling
// of human-readable duration strings (e.g. "30s", "5m", "1h").
//
// +ocm:jsonschema-gen=true
// +ocm:jsonschema-gen:schema-from=schemas/Timeout.schema.json
type Timeout time.Duration

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

// NewTimeout creates a pointer to a Timeout value.
func NewTimeout(d time.Duration) *Timeout {
	t := Timeout(d)
	return &t
}

var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&Config{},
		runtime.NewVersionedType(ConfigType, Version),
		runtime.NewUnversionedType(ConfigType),
	)
}

// TimeoutConfig holds HTTP client timeout settings.
// All fields are pointers; nil means "use default".
//
// +k8s:deepcopy-gen=true
type TimeoutConfig struct {
	// Timeout specifies a time limit for requests made by the HTTP
	// client. The timeout includes connection time, any redirects,
	// and reading the response body. A timeout of zero means no
	// timeout.
	Timeout *Timeout `json:"timeout,omitempty"`

	// TCPDialTimeout is the maximum amount of time a dial will wait
	// for a TCP connect to complete. When dialing a host name with
	// multiple IP addresses, the timeout may be divided between them.
	// The operating system may impose its own earlier timeout.
	TCPDialTimeout *Timeout `json:"tcpDialTimeout,omitempty"`

	// TCPKeepAlive specifies the interval between keep-alive probes
	// for an active network connection. If negative, keep-alive
	// probes are disabled.
	TCPKeepAlive *Timeout `json:"tcpKeepAlive,omitempty"`

	// TLSHandshakeTimeout specifies the maximum amount of time to
	// wait for a TLS handshake. Zero means no timeout.
	TLSHandshakeTimeout *Timeout `json:"tlsHandshakeTimeout,omitempty"`

	// ResponseHeaderTimeout specifies the amount of time to wait for
	// a server's response headers after fully writing the request
	// (including its body, if any). This time does not include the
	// time to read the response body.
	ResponseHeaderTimeout *Timeout `json:"responseHeaderTimeout,omitempty"`

	// IdleConnTimeout is the maximum amount of time an idle
	// (keep-alive) connection will remain idle before closing itself.
	// Zero means no limit.
	IdleConnTimeout *Timeout `json:"idleConnTimeout,omitempty"`
}

// Validate checks that timeout values are non-negative.
// TCPKeepAlive is not validated because any negative value
// disables keep-alive probes (consistent with Go's net.Dialer.KeepAlive).
func (c *TimeoutConfig) Validate() error {
	for _, check := range []struct {
		name string
		val  *Timeout
	}{
		{"timeout", c.Timeout},
		{"tcpDialTimeout", c.TCPDialTimeout},
		{"tlsHandshakeTimeout", c.TLSHandshakeTimeout},
		{"responseHeaderTimeout", c.ResponseHeaderTimeout},
		{"idleConnTimeout", c.IdleConnTimeout},
	} {
		if check.val != nil && time.Duration(*check.val) < 0 {
			return fmt.Errorf("invalid value for %s: %s, must be zero or positive", check.name, time.Duration(*check.val))
		}
	}
	return nil
}

// MergeTimeoutConfig merges src into dst. Non-nil fields in src override dst.
func MergeTimeoutConfig(dst, src *TimeoutConfig) TimeoutConfig {
	out := TimeoutConfig{}
	if dst != nil {
		out = *dst
	}
	if src == nil {
		return out
	}
	if src.Timeout != nil {
		out.Timeout = src.Timeout
	}
	if src.TCPDialTimeout != nil {
		out.TCPDialTimeout = src.TCPDialTimeout
	}
	if src.TCPKeepAlive != nil {
		out.TCPKeepAlive = src.TCPKeepAlive
	}
	if src.TLSHandshakeTimeout != nil {
		out.TLSHandshakeTimeout = src.TLSHandshakeTimeout
	}
	if src.ResponseHeaderTimeout != nil {
		out.ResponseHeaderTimeout = src.ResponseHeaderTimeout
	}
	if src.IdleConnTimeout != nil {
		out.IdleConnTimeout = src.IdleConnTimeout
	}
	return out
}

// HostConfig contains per-host HTTP timeout settings that override global values.
// All fields are pointers; nil means "inherit from global".
//
// +k8s:deepcopy-gen=true
type HostConfig struct {
	TimeoutConfig `json:",inline"`
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

	TimeoutConfig `json:",inline"`

	// Hosts maps hostname (or hostname:port) to per-host timeout settings.
	// Fields set here override the corresponding top-level value for that host.
	Hosts map[string]*HostConfig `json:"hosts,omitempty"`
}

// Validate checks the embedded TimeoutConfig and each per-host TimeoutConfig
// for non-negative timeout values. Host errors are wrapped with the host key
// so the caller knows which entry failed.
func (c *Config) Validate() error {
	if err := c.TimeoutConfig.Validate(); err != nil {
		return err
	}
	for host, hc := range c.Hosts {
		if hc == nil {
			continue
		}
		if err := hc.Validate(); err != nil {
			return fmt.Errorf("host %q: %w", host, err)
		}
	}
	return nil
}

// ResolveHTTPConfig resolves the HTTP configuration from a central generic V1
// config and validates it. A nil cfg is allowed; it produces a Config carrying
// only DefaultTimeout.
func ResolveHTTPConfig(cfg *genericv1.Config) (*Config, error) {
	c, err := LookupConfig(cfg)
	if err != nil {
		return nil, err
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("invalid http configuration: %w", err)
	}
	return c, nil
}

// LookupConfig creates an HTTP configuration from a central generic V1 config.
func LookupConfig(cfg *genericv1.Config) (*Config, error) {
	var merged *Config
	if cfg != nil {
		cfg, err := genericv1.Filter(cfg, &genericv1.FilterOptions{
			ConfigTypes: []runtime.Type{
				runtime.NewVersionedType(ConfigType, Version),
				runtime.NewUnversionedType(ConfigType),
			},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to filter config: %w", err)
		}
		cfgs := make([]*Config, 0, len(cfg.Configurations))
		for _, entry := range cfg.Configurations {
			var config Config
			if err := Scheme.Convert(entry, &config); err != nil {
				return nil, fmt.Errorf("failed to decode http config: %w", err)
			}
			cfgs = append(cfgs, &config)
		}
		merged = Merge(cfgs...)
		if merged == nil {
			merged = &Config{}
		}
	} else {
		merged = new(Config)
	}

	if merged.Timeout == nil {
		merged.Timeout = NewTimeout(time.Duration(DefaultTimeout))
	}

	return merged, nil
}

// Merge merges the provided configs into a single config.
// For pointer fields the last non-nil value wins.
// Hosts maps are merged entry-by-entry; last value per key wins.
func Merge(configs ...*Config) *Config {
	if len(configs) == 0 {
		return nil
	}

	merged := new(Config)
	_, _ = Scheme.DefaultType(merged)

	for _, c := range configs {
		if c == nil {
			continue
		}
		merged.TimeoutConfig = MergeTimeoutConfig(&merged.TimeoutConfig, &c.TimeoutConfig)

		if len(c.Hosts) > 0 {
			if merged.Hosts == nil {
				merged.Hosts = make(map[string]*HostConfig)
			}
			for k, v := range c.Hosts {
				merged.Hosts[k] = v
			}
		}
	}

	return merged
}
