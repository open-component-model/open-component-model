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

// TLSConfig holds TLS-level HTTP client settings.
// All fields are pointers; nil means "use default / inherit from parent".
//
// WARNING: Disabling TLS verification (InsecureSkipVerify) makes connections
// vulnerable to man-in-the-middle attacks. Use only in development/testing.
//
// +k8s:deepcopy-gen=true
type TLSConfig struct {
	// InsecureSkipVerify disables verification of the server's TLS certificate
	// chain and host name. Setting this to true makes connections vulnerable to
	// active MITM attacks; use only for development and local registry testing.
	// A warning is logged at transport build time and on every new host connection.
	//
	// Nil means "unset" (TLS verification stays enabled at the top level). When
	// TLSConfig is embedded in HostConfig, nil additionally means "inherit from
	// the global Config"; an explicit false on a host re-enables verification
	// even when the global config sets true. See HostConfig for per-host merge
	// semantics.
	InsecureSkipVerify *bool `json:"insecureSkipVerify,omitempty"`
}

// MergeTLSConfig merges src into dst. Non-nil fields in src override dst.
func MergeTLSConfig(dst, src *TLSConfig) TLSConfig {
	out := TLSConfig{}
	if dst != nil {
		out = *dst
	}
	if src == nil {
		return out
	}
	if src.InsecureSkipVerify != nil {
		out.InsecureSkipVerify = src.InsecureSkipVerify
	}
	return out
}

// RetryConfig holds HTTP client retry settings.
// All fields are pointers; nil means "use default".
//
// Example:
//
//	retry:
//	  maxRetries: 3      # 0 = infinite; -1 = disable; nil uses default (5)
//	  minWait: 100ms
//	  maxWait: 5s
//
// +k8s:deepcopy-gen=true
type RetryConfig struct {
	// MaxRetries is the maximum number of retry attempts after the initial
	// request. Zero means infinite retries. -1 disables retry entirely.
	// Nil uses the library default (5).
	MaxRetries *int `json:"maxRetries,omitempty"`

	// MinWait is the lower bound applied to the backoff duration between
	// retry attempts. Nil uses the library default (200ms).
	MinWait *Timeout `json:"minWait,omitempty"`

	// MaxWait is the upper bound applied to the backoff duration between
	// retry attempts. Nil uses the library default (3s).
	MaxWait *Timeout `json:"maxWait,omitempty"`
}

// Validate checks that retry values are valid.
func (r *RetryConfig) Validate() error {
	if r.MaxRetries != nil && *r.MaxRetries < -1 {
		return fmt.Errorf("invalid value for maxRetries: %d, must be -1 (disable), 0 (infinite), or positive", *r.MaxRetries)
	}
	if r.MinWait != nil && time.Duration(*r.MinWait) < 0 {
		return fmt.Errorf("invalid value for minWait: %s, must be zero or positive", time.Duration(*r.MinWait))
	}
	if r.MaxWait != nil && time.Duration(*r.MaxWait) < 0 {
		return fmt.Errorf("invalid value for maxWait: %s, must be zero or positive", time.Duration(*r.MaxWait))
	}
	if r.MinWait != nil && r.MaxWait != nil && time.Duration(*r.MinWait) > time.Duration(*r.MaxWait) {
		return fmt.Errorf("minWait (%s) must not exceed maxWait (%s)", time.Duration(*r.MinWait), time.Duration(*r.MaxWait))
	}
	return nil
}

// MergeRetryConfig merges src into dst. Non-nil fields in src override dst.
// Returns nil if both inputs are nil.
//
// This allows multiple OCM config layers (e.g. system-wide defaults, user
// overrides) to be composed without requiring each layer to specify every
// field — unset fields are inherited from earlier layers rather than
// resetting them to zero.
func MergeRetryConfig(dst, src *RetryConfig) *RetryConfig {
	if dst == nil && src == nil {
		return nil
	}
	out := &RetryConfig{}
	if dst != nil {
		*out = *dst
	}
	if src == nil {
		return out
	}
	if src.MaxRetries != nil {
		out.MaxRetries = src.MaxRetries
	}
	if src.MinWait != nil {
		out.MinWait = src.MinWait
	}
	if src.MaxWait != nil {
		out.MaxWait = src.MaxWait
	}
	return out
}

// HostConfig contains per-host HTTP settings that override global values.
// All fields are pointers; nil means "inherit from global".
//
// Note: Retry is currently global-only in the transport layer; per-host
// retry config overrides the global policy field by field.
//
// +k8s:deepcopy-gen=true
type HostConfig struct {
	TimeoutConfig `json:",inline"`

	// TLSConfig overrides TLS settings for this host.
	// Fields set here override the corresponding top-level TLS value.
	TLSConfig `json:",inline"`

	// Retry overrides the global retry policy for this host.
	// Fields set here override the corresponding top-level retry value.
	Retry *RetryConfig `json:"retry,omitempty"`
}

// Validate checks the per-host timeout and retry config for valid values.
func (h *HostConfig) Validate() error {
	if err := h.TimeoutConfig.Validate(); err != nil {
		return fmt.Errorf("invalid timeout config: %w", err)
	}
	if h.Retry != nil {
		if err := h.Retry.Validate(); err != nil {
			return fmt.Errorf("invalid retry config: %w", err)
		}
	}
	return nil
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

	// TLSConfig configures TLS verification behaviour.
	// InsecureSkipVerify disables certificate verification globally; use only for
	// development/testing with self-signed certificates.
	TLSConfig `json:",inline"`

	// Retry configures retry behavior for transient failures.
	// Nil uses the library default policy (5 retries, exponential backoff
	// with 200ms–3s bounds).
	Retry *RetryConfig `json:"retry,omitempty"`

	// Hosts maps hostname (or hostname:port) to per-host settings.
	// Fields set here override the corresponding top-level value for that host.
	Hosts map[string]*HostConfig `json:"hosts,omitempty"`
}

// Validate checks the embedded TimeoutConfig, Retry, and each per-host config
// for valid values. Host errors are wrapped with the host key.
func (c *Config) Validate() error {
	if err := c.TimeoutConfig.Validate(); err != nil {
		return err
	}
	if c.Retry != nil {
		if err := c.Retry.Validate(); err != nil {
			return fmt.Errorf("invalid retry config: %w", err)
		}
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
		merged.TLSConfig = MergeTLSConfig(&merged.TLSConfig, &c.TLSConfig)
		merged.Retry = MergeRetryConfig(merged.Retry, c.Retry)

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
