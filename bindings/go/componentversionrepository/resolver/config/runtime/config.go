package runtime

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Config is the OCM configuration type.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type Config struct {
	Type runtime.Type `json:"-"`
	// Resolvers define a list of OCM repository specifications to be used to resolve
	// dedicated component versions.
	// All matching entries are tried to lookup a component version in the following
	//    order:
	//    - highest priority first
	//    - longest matching sequence of component name segments first.
	// If resolvers are defined, it is possible to use component version names on the
	// command line without a repository. The names are resolved with the specified
	// resolution rule.
	//
	// They are also used as default lookup repositories to lookup component references
	// for recursive operations on component versions («--lookup» option).
	Resolvers []Resolver `json:"-"`
}

// +k8s:deepcopy-gen=true
type Resolver struct {
	// Repository is the OCM repository specification to be used for resolving
	// component versions.
	Repository runtime.Typed `json:"-"`

	// Optionally, a component name prefix can be given.
	// It limits the usage of the repository to resolve only
	// components with the given name prefix (always complete name segments).
	Prefix string `json:"-"`

	// An optional priority can be used to influence the lookup order. Larger value
	// means higher priority (default DefaultLookupPriority).
	Priority int `json:"-"`
}
