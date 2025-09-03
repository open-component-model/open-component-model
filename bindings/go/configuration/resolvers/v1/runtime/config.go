package runtime

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Config is the OCM configuration type for configuring regex/glob based
// resolvers.
//
//   - type: resolvers.config.ocm.software
//     resolvers:
//   - repository:
//     type: CommonTransportFormat/v1
//     filePath: ./ocm/primary-transport-archive
//     componentName: ocm.software/core/*
//     semver: >1.0.0
//     priority: 100
//   - repository:
//     type: OCIRegistry/v1
//     baseUrl: ghcr.io
//     componentName: ocm.software/*
//     semver: >1.0.0
//     priority: 10
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type Config struct {
	Type runtime.Type `json:"-"`
	// Resolvers define a list of OCM repository specifications to be used to resolve
	// dedicated component versions using regex/glob patterns.
	// All matching entries are tried to lookup a component version in the following
	//    order:
	//    - highest priority first
	//
	// The default priority is spec.DefaultLookupPriority (10).
	//
	// Repositories with a specified componentName pattern are only tried if the pattern
	// matches the component name using regex or glob syntax.
	//
	// If resolvers are defined, it is possible to use component version names on the
	// command line without a repository. The names are resolved with the specified
	// resolution rule.
	//
	// They are also used as default lookup repositories to lookup component references
	// for recursive operations on component versions («--lookup» option).
	Resolvers []Resolver `json:"-"`
}

// GetType returns the type of the configuration.
func (c *Config) GetType() runtime.Type {
	return c.Type
}

// SetType sets the type of the configuration.
func (c *Config) SetType(t runtime.Type) {
	c.Type = t
}

// Resolver assigns a priority and a component name pattern to a single OCM repository specification
// to allow defining a lookup order for component versions using regex/glob patterns.
//
// +k8s:deepcopy-gen=true
type Resolver struct {
	// Repository is the OCM repository specification to be used for resolving
	// component versions.
	Repository runtime.Typed `json:"-"`

	// ComponentName specifies a regex or glob pattern for matching component names.
	// It limits the usage of the repository to resolve only components with names
	// that match the given pattern.
	// Examples:
	//   - "ocm.software/core/*" (glob pattern)
	//   - "ocm\\.software/.*" (regex pattern)
	//   - "ocm.software/core/.*" (regex pattern)
	ComponentName string `json:"-"`

	// SemVer specifies a semantic version constraint for the component version.
	// It limits the usage of the repository to resolve only component versions
	// that satisfy the given version constraint.
	// Examples:
	//   - ">1.0.0"
	//   - ">=1.0.0 <2.0.0"
	//   - "~1.2.3"
	//   - "^1.2.3"
	SemVer string `json:"-"`

	// An optional priority can be used to influence the lookup order. Larger value
	// means higher priority (default DefaultLookupPriority).
	Priority int `json:"-"`
}
