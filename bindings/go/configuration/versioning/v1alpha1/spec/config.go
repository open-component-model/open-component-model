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
	// built-in loose-semver scheme. Deprecated alias kept for callers; prefer
	// [versioning.BuiltinLooseSemver] and the other versioning.Builtin* names.
	BuiltinLooseSemver = versioning.BuiltinLooseSemver
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
//	- builtin: loose-semver # opt the built-in loose-semver back in as a fallback
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
	Schemes []VersionScheme `json:"schemes,omitempty"`
}

// VersionScheme describes a single version scheme, either as a regular
// expression with an ordered list of named capture groups used for comparison,
// or as a reference to a built-in scheme.
//
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type VersionScheme struct {
	// Name is an optional human-facing label for a regex (pattern) scheme, used
	// only in diagnostics (e.g. "calver", "build-number"); when omitted the
	// pattern is used instead. It must not be set on a Builtin entry, which
	// carries its own canonical name.
	Name string `json:"name,omitempty"`

	// Builtin selects a named built-in scheme instead of a regular expression.
	// Supported values: "loose-semver" (the historical default), "calver-full"
	// (YYYY.MM.DD), "calver-month" (YYYY.MM), "calver-ubuntu" (YY.MM),
	// "calver-micro" (YYYY.M(M).PATCH), "aws-date" (YYYY-MM-DD), and
	// "build-number" (monotonic integer). Each is exactly the equivalent regex
	// pattern plus comparison groups, documented in the versioning configuration
	// reference. When set, Pattern and ComparisonGroups must be empty. Add an
	// entry with builtin: loose-semver (typically last) to keep recognizing semver
	// versions alongside custom schemes.
	// +ocm:jsonschema-gen:enum=loose-semver,calver-full,calver-month,calver-ubuntu,calver-micro,aws-date,build-number
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

// Lookup creates a new Config from a central generic config.
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

// RegistryFromConfig builds a versioning registry from the central OCM
// configuration. When the config carries no versioning entry, the loose-semver
// default is returned.
func RegistryFromConfig(config *genericv1.Config) (*versioning.Registry, error) {
	cfg, err := Lookup(config)
	if err != nil {
		return nil, fmt.Errorf("failed to look up versioning config: %w", err)
	}
	return cfg.Registry()
}

// Merge merges the provided configs into a single config, concatenating their
// schemes in order.
func Merge(configs ...*Config) *Config {
	if len(configs) == 0 {
		return nil
	}

	merged := new(Config)
	merged.Type = configs[0].Type
	merged.Schemes = make([]VersionScheme, 0)

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
// Builtin set selects a built-in scheme (see [versioning.BuiltinNames]) and must
// not set Name, Pattern, or ComparisonGroups.
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
		if s.Builtin == "" && s.Pattern == "" {
			return nil, fmt.Errorf("versioning scheme at index %d: must set exactly one of pattern or builtin", i)
		}
		if s.Builtin != "" {
			// A builtin entry carries its own canonical name and behavior; name,
			// pattern, and comparisonGroups are superfluous and rejected so configs
			// stay unambiguous.
			if s.Name != "" || s.Pattern != "" || len(s.ComparisonGroups) > 0 {
				return nil, fmt.Errorf("versioning scheme at index %d: builtin %q is mutually exclusive with name, pattern, and comparisonGroups", i, s.Builtin)
			}
			scheme, ok := versioning.BuiltinScheme(s.Builtin)
			if !ok {
				return nil, fmt.Errorf("versioning scheme at index %d: unknown builtin %q, valid builtins are %v", i, s.Builtin, versioning.BuiltinNames())
			}
			schemes = append(schemes, scheme)
			continue
		}
		pattern, err := regexp.Compile(s.Pattern)
		if err != nil {
			return nil, fmt.Errorf("versioning scheme %q (index %d): invalid pattern: %w", s.Name, i, err)
		}
		names := make(map[string]int, len(pattern.SubexpNames()))
		for _, name := range pattern.SubexpNames() {
			if name != "" {
				names[name]++
			}
		}
		for _, group := range s.ComparisonGroups {
			switch names[group] {
			case 0:
				return nil, fmt.Errorf("versioning scheme %q: comparison group %q is not a named capture group in the pattern", s.Name, group)
			case 1:
				// unique named group — extraction is unambiguous.
			default:
				// Go's regexp allows repeating a capture-group name across alternation
				// branches, but only one branch matches, so the others overwrite the
				// extracted value with an empty string and versions that should differ
				// compare equal. Reject the ambiguity at load time.
				return nil, fmt.Errorf("versioning scheme %q: comparison group %q is declared %d times in the pattern; a comparison group must be a single named capture group", s.Name, group, names[group])
			}
		}
		name := s.Name
		if name == "" {
			name = s.Pattern
		}
		schemes = append(schemes, versioning.NewRegexScheme(name, pattern, s.ComparisonGroups))
	}

	return versioning.NewRegistry(schemes...), nil
}
