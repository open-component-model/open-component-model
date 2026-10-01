package v1alpha1

import (
	"encoding/json"
	"fmt"
	"time"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// ConfigType identifies the OCI caching configuration inside the central
	// generic OCM config.
	ConfigType = "caching.oci.config.ocm.software"
)

// Scheme is the runtime scheme this config type registers under.
var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&Config{},
		runtime.NewVersionedType(ConfigType, Version),
		runtime.NewUnversionedType(ConfigType),
	)
}

// Mode selects cache activation and remote validation policy.
// The empty value means omitted/inherited.
type Mode string

const (
	// ModeAlways enables the caches and validates remote access on every blob
	// hit; references are always resolved upstream.
	ModeAlways Mode = "Always"
	// ModeIfNotPresent enables the caches and serves blob hits and cached
	// digest-reference resolutions locally. Mutable tags still resolve upstream.
	ModeIfNotPresent Mode = "IfNotPresent"
	// ModeNever disables both caches.
	ModeNever Mode = "Never"
)

// Valid reports whether m is empty (omitted) or a known mode.
func (m Mode) Valid() bool {
	switch m {
	case "", ModeAlways, ModeIfNotPresent, ModeNever:
		return true
	default:
		return false
	}
}

// Duration wraps time.Duration. It is serialized as a Go duration string
// (e.g. "30s", "10m", "1h30m") and, unlike legacy timeout types, accepts only
// strings on input.
//
// +ocm:jsonschema-gen=true
// +ocm:jsonschema-gen:schema-from=schemas/Duration.schema.json
type Duration time.Duration

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("duration must be a string like 30s, 10m, or 1h30m: %w", err)
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// NewDuration creates a pointer to a Duration value.
func NewDuration(d time.Duration) *Duration {
	v := Duration(d)
	return &v
}

// Config is the wire format of the OCI caching configuration.
//
//	type: generic.config.ocm.software/v1
//	configurations:
//	  - type: caching.oci.config.ocm.software/v1alpha1
//	    mode: IfNotPresent
//	    ttl: 10m
//	    maxBlobSize: 4194304 # bytes
//
// All fields are optional; omitted fields keep the consumer default or the
// value of an earlier entry.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Config struct {
	// +ocm:jsonschema-gen:enum=caching.oci.config.ocm.software/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=caching.oci.config.ocm.software
	Type runtime.Type `json:"type"`

	// Mode selects cache activation and remote validation policy.
	// Always validates remote access on every blob hit and resolves references
	// upstream, IfNotPresent serves hits locally, Never disables both caches.
	// When omitted, IfNotPresent applies.
	// +ocm:jsonschema-gen:enum=Always,IfNotPresent,Never
	Mode Mode `json:"mode,omitempty"`

	// TTL is how long cached OCI metadata remains reusable, as a positive
	// duration string such as 30s, 10m or 1h30m. Defaults to 10m.
	TTL *Duration `json:"ttl,omitempty"`

	// MaxBlobSize is the largest individual OCI metadata blob in bytes that is
	// retained. Larger blobs are fetched normally but not cached. Must be
	// positive. Defaults to 4194304 (4 MiB).
	// +ocm:jsonschema-gen:minimum=1
	MaxBlobSize *int64 `json:"maxBlobSize,omitempty"`
}

// Validate checks the type and all explicitly set fields.
func (c *Config) Validate() error {
	if c == nil {
		return nil
	}
	if c.Type != (runtime.Type{}) &&
		c.Type != runtime.NewVersionedType(ConfigType, Version) &&
		c.Type != runtime.NewUnversionedType(ConfigType) {
		return fmt.Errorf("invalid config type %q, expected %s/%s", c.Type, ConfigType, Version)
	}
	if !c.Mode.Valid() {
		return fmt.Errorf("invalid mode %q, expected one of %s, %s, %s (or omitted)", c.Mode, ModeAlways, ModeIfNotPresent, ModeNever)
	}
	if c.TTL != nil && time.Duration(*c.TTL) <= 0 {
		return fmt.Errorf("invalid ttl %s, must be positive", time.Duration(*c.TTL))
	}
	if c.MaxBlobSize != nil && *c.MaxBlobSize <= 0 {
		return fmt.Errorf("invalid maxBlobSize %d, must be positive", *c.MaxBlobSize)
	}
	return nil
}

// LookupConfig extracts, validates, and merges all [ConfigType] entries from
// cfg. Returns nil when cfg is nil or carries no matching entries.
func LookupConfig(cfg *genericv1.Config) (*Config, error) {
	if cfg == nil {
		return nil, nil
	}
	filtered, err := genericv1.Filter(cfg, &genericv1.FilterOptions{
		ConfigTypes: []runtime.Type{
			runtime.NewVersionedType(ConfigType, Version),
			runtime.NewUnversionedType(ConfigType),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to filter OCI caching config: %w", err)
	}
	if len(filtered.Configurations) == 0 {
		return nil, nil
	}
	cfgs := make([]*Config, 0, len(filtered.Configurations))
	for _, entry := range filtered.Configurations {
		var c Config
		if err := Scheme.Convert(entry, &c); err != nil {
			return nil, fmt.Errorf("failed to decode OCI caching config: %w", err)
		}
		if err := c.Validate(); err != nil {
			return nil, fmt.Errorf("invalid OCI caching config: %w", err)
		}
		cfgs = append(cfgs, &c)
	}
	return Merge(cfgs...), nil
}

// Merge folds configs left-to-right; the last explicitly set field wins.
// Returns nil if no non-nil config is given. The result carries the canonical
// versioned type.
func Merge(configs ...*Config) *Config {
	var out *Config
	for _, c := range configs {
		if c == nil {
			continue
		}
		if out == nil {
			out = &Config{Type: runtime.NewVersionedType(ConfigType, Version)}
		}
		if c.Mode != "" {
			out.Mode = c.Mode
		}
		if c.TTL != nil {
			out.TTL = NewDuration(time.Duration(*c.TTL))
		}
		if c.MaxBlobSize != nil {
			v := *c.MaxBlobSize
			out.MaxBlobSize = &v
		}
	}
	return out
}
