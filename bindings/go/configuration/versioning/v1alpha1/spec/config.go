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

	// BuiltinLooseSemver is the VersionScheme.Builtin value selecting the
	// built-in loose-semver scheme (see [versioning.NewLooseSemverScheme]).
	BuiltinLooseSemver = "loose-semver"
)

var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&Config{},
		runtime.NewVersionedType(ConfigType, Version),
		runtime.NewUnversionedType(ConfigType),
	)
}

// Config is the OCM configuration type for teaching OCM about additional
// component version schemes using ordered matchers.
//
//	type: versioning.config.ocm.software/v1alpha1
//	schemes:
//	- name: calver-date
//	  pattern: '^(?P<year>\d{4})\.(?P<month>\d{2})\.(?P<day>\d{2})$'
//	  comparisonGroups: [year, month, day]
//	- name: semver          # opt the built-in loose-semver back in as a fallback
//	  builtin: loose-semver
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
	// The list is evaluated in order: for any version the first scheme that
	// claims it wins. Unlike the historical default, the built-in loose-semver
	// scheme is NOT appended automatically once schemes are configured: only the
	// listed schemes apply. To keep recognizing semver versions, add an explicit
	// entry with builtin: loose-semver (typically last, as a fallback). List
	// more specific schemes before broader ones. When no config is present at
	// all, OCM still uses loose semver (see [versioning.Default]).
	Schemes []*VersionScheme `json:"schemes,omitempty"`
}

// VersionScheme describes a single version scheme, either as a regular
// expression with an ordered list of named capture groups used for comparison,
// or as a reference to a built-in scheme.
//
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type VersionScheme struct {
	// Name is a stable identifier for the scheme (e.g. "calver", "build-number").
	Name string `json:"name"`

	// Builtin selects a built-in scheme instead of a regular expression. The only
	// supported value is "loose-semver", which uses the same loose semantic
	// versioning behavior OCM applies by default. When set, Pattern and
	// ComparisonGroups must be empty. Add an entry with builtin: loose-semver
	// (typically last) to keep recognizing semver versions alongside custom
	// schemes.
	Builtin string `json:"builtin,omitempty"`

	// Pattern is a Go (RE2) regular expression that a version must match for
	// this scheme to claim it. Use named capture groups for the fields that
	// determine ordering, e.g.
	// "^(?P<year>\\d{4})\\.(?P<month>\\d{2})\\.(?P<day>\\d{2})$". Mutually
	// exclusive with Builtin.
	Pattern string `json:"pattern,omitempty"`

	// ComparisonGroups lists the named capture groups from Pattern used to order
	// versions, most significant first. Numeric groups compare as integers;
	// non-numeric groups compare lexically. When empty, whole matched strings
	// compare lexically. Must be empty when Builtin is set.
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

// Registry compiles the configured schemes into a [versioning.Registry], in the
// order given.
//
// A regex scheme's pattern is compiled with [regexp.Compile]; every name in its
// comparisonGroups must be a named capture group in that pattern. A scheme with
// Builtin set selects a built-in scheme (currently only "loose-semver") and must
// not set Pattern or ComparisonGroups.
//
// The built-in loose-semver scheme is NOT appended automatically: once schemes
// are configured, only the listed schemes apply. Add an explicit entry with
// builtin: loose-semver (typically last) to keep recognizing semver versions. A
// nil or empty config yields [versioning.Default] (loose semver only).
func (c *Config) Registry() (*versioning.Registry, error) {
	if c == nil || len(c.Schemes) == 0 {
		return versioning.Default(), nil
	}

	schemes := make([]versioning.Scheme, 0, len(c.Schemes))
	for i, s := range c.Schemes {
		if s == nil {
			return nil, fmt.Errorf("versioning scheme at index %d is null", i)
		}
		if s.Builtin == "" && s.Pattern == "" {
			return nil, fmt.Errorf("versioning scheme %q (index %d): must set exactly one of pattern or builtin", s.Name, i)
		}
		if s.Builtin != "" {
			if s.Pattern != "" || len(s.ComparisonGroups) > 0 {
				return nil, fmt.Errorf("versioning scheme %q (index %d): builtin is mutually exclusive with pattern and comparisonGroups", s.Name, i)
			}
			switch s.Builtin {
			case BuiltinLooseSemver:
				schemes = append(schemes, versioning.NewLooseSemverScheme())
			default:
				return nil, fmt.Errorf("versioning scheme %q (index %d): unknown builtin %q", s.Name, i, s.Builtin)
			}
			continue
		}
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

	return versioning.NewRegistry(schemes...), nil
}
