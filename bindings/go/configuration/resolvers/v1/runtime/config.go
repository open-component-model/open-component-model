package runtime

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Config is the OCM configuration type for configuring glob based
// resolvers.
//
//   - type: resolvers.config.ocm.software
//     resolvers:
//   - repository:
//     type: OCIRegistry
//     baseUrl: ghcr.io
//     subPath: open-component-model/components
//     componentName: ocm.software/core/*
//     semver: >1.0.0
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type Config struct {
	Type runtime.Type `json:"-"`
	// Resolvers define a list of OCM repository specifications to be used to resolve
	// dedicated component versions using glob patterns.
	// All matching entries are tried to lookup a component version in the order
	// they are defined in the configuration.
	//
	// Repositories with a specified componentName pattern are only tried if the pattern
	// matches the component name using glob syntax.
	//
	// If resolvers are defined, it is possible to use component version names on the
	// command line without a repository. The names are resolved with the specified
	// resolution rule.
	//
	// They are also used as default lookup repositories to lookup component references
	// for recursive operations on component versions («--lookup» option).
	Resolvers []Resolver `json:"-"`
}

func (c *Config) GetType() runtime.Type {
	return c.Type
}

func (c *Config) SetType(t runtime.Type) {
	c.Type = t
}

// Resolver assigns a component name pattern to a single OCM repository specification
// to allow defining component version resolution using glob patterns.
//
// +k8s:deepcopy-gen=true
type Resolver struct {
	// Repository is the OCM repository specification to be used for resolving
	// component versions.
	Repository runtime.Typed `json:"-"`

	// ComponentName specifies a glob pattern for matching component names.
	// It limits the usage of the repository to resolve only components with names
	// that match the given pattern.
	// Examples:
	//   - "ocm.software/core/*" (matches any component in the core namespace)
	//   - "*.software/*/test" (matches test components in any software namespace)
	//   - "ocm.software/core/[tc]est" (matches "test" or "cest" in core namespace)
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
}
