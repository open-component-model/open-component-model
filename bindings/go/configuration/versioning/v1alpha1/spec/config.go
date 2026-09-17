package spec

import (
	"fmt"
	"regexp"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/runtime/versioning"
)

const (
	ConfigType = "versioning.config.ocm.software"
	Version    = "v1alpha1"
)

var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&Config{},
		runtime.NewVersionedType(ConfigType, Version),
		runtime.NewUnversionedType(ConfigType),
	)
}

// Config is the OCM configuration type for teaching OCM about additional
// component version schemes using ordered, regex-based matchers.
//
//	type: versioning.config.ocm.software/v1alpha1
//	schemes:
//	- name: calver-date
//	  pattern: '^(?P<year>\d{4})\.(?P<month>\d{2})\.(?P<day>\d{2})$'
//	  comparisonGroups: [year, month, day]
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Config struct {
	// +ocm:jsonschema-gen:enum=versioning.config.ocm.software/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=versioning.config.ocm.software
	Type runtime.Type `json:"type"`

	// Schemes defines an ordered list of version schemes.
	//
	// The list is evaluated in order: for any version the first scheme whose
	// pattern matches wins. The built-in loose-semver scheme is always appended
	// as the final fallback, so semver versions keep working and adding schemes
	// is purely additive. List more specific schemes before broader ones.
	Schemes []*VersionScheme `json:"schemes,omitempty"`
}

// VersionScheme describes a single version scheme as a regular expression plus
// an ordered list of named capture groups used for comparison.
//
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type VersionScheme struct {
	// Name is a stable identifier for the scheme (e.g. "calver", "build-number").
	Name string `json:"name"`

	// Pattern is a Go (RE2) regular expression that a version must match for
	// this scheme to claim it. Use named capture groups for the fields that
	// determine ordering, e.g.
	// "^(?P<year>\\d{4})\\.(?P<month>\\d{2})\\.(?P<day>\\d{2})$".
	Pattern string `json:"pattern"`

	// ComparisonGroups lists the named capture groups from Pattern used to order
	// versions, most significant first. Numeric groups compare as integers;
	// non-numeric groups compare lexically. When empty, whole matched strings
	// compare lexically.
	ComparisonGroups []string `json:"comparisonGroups,omitempty"`
}

// Lookup creates a new Config from a central V1 config.
func Lookup(cfg *genericv1.Config) (*Config, error) {
	if cfg == nil {
		return nil, nil
	}
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
			return nil, fmt.Errorf("failed to decode versioning config: %w", err)
		}
		cfgs = append(cfgs, &config)
	}
	return Merge(cfgs...), nil
}

// Merge merges the provided configs into a single config, concatenating their
// schemes in order.
func Merge(configs ...*Config) *Config {
	if len(configs) == 0 {
		return nil
	}

	merged := new(Config)
	merged.Type = configs[0].Type
	merged.Schemes = make([]*VersionScheme, 0)

	for _, cfg := range configs {
		merged.Schemes = append(merged.Schemes, cfg.Schemes...)
	}

	return merged
}

// Registry compiles the configured schemes into a [versioning.Registry].
//
// Each scheme's pattern is compiled with [regexp.Compile]; every name in a
// scheme's comparisonGroups must be a named capture group in that pattern. The
// built-in loose-semver scheme is appended as the final fallback so unmatched
// versions still order. A nil or empty config yields [versioning.Default].
func (c *Config) Registry() (*versioning.Registry, error) {
	if c == nil || len(c.Schemes) == 0 {
		return versioning.Default(), nil
	}

	schemes := make([]versioning.Scheme, 0, len(c.Schemes)+1)
	for i, s := range c.Schemes {
		pattern, err := regexp.Compile(s.Pattern)
		if err != nil {
			return nil, fmt.Errorf("versioning scheme %q (index %d): invalid pattern: %w", s.Name, i, err)
		}
		names := make(map[string]struct{}, len(pattern.SubexpNames()))
		for _, name := range pattern.SubexpNames() {
			if name != "" {
				names[name] = struct{}{}
			}
		}
		for _, group := range s.ComparisonGroups {
			if _, ok := names[group]; !ok {
				return nil, fmt.Errorf("versioning scheme %q: comparison group %q is not a named capture group in the pattern", s.Name, group)
			}
		}
		schemes = append(schemes, versioning.NewRegexScheme(s.Name, pattern, s.ComparisonGroups))
	}
	// Append loose-semver as the final fallback.
	schemes = append(schemes, versioning.Default().Schemes()...)

	return versioning.NewRegistry(schemes...), nil
}
