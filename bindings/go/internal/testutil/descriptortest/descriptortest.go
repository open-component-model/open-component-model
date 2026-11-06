// Package descriptortest builds component descriptors for tests.
//
// [New] returns a descriptor with schema version v2, the given name and version, and the
// provider ocm.software. Options add or override fields:
//
//	desc := descriptortest.New("ocm.software/app", "1.0.0",
//		descriptortest.WithResources(res),
//		descriptortest.WithReferences(ref),
//	)
package descriptortest

import (
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// DefaultMetaVersion is the schema version [New] sets unless [WithMetaVersion] overrides it.
const DefaultMetaVersion = "v2"

// DefaultProvider is the provider name [New] sets unless [WithProvider] overrides it.
const DefaultProvider = "ocm.software"

// Option changes the descriptor [New] builds.
type Option func(*descriptor.Descriptor)

// New returns a descriptor for the component name in the given version, with
// [DefaultMetaVersion] and [DefaultProvider], after applying opts in order.
func New(name, version string, opts ...Option) *descriptor.Descriptor {
	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: DefaultMetaVersion},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    name,
					Version: version,
				},
			},
			Provider: descriptor.Provider{Name: DefaultProvider},
		},
	}
	for _, opt := range opts {
		opt(desc)
	}
	return desc
}

// WithMetaVersion sets the schema version. An empty string leaves it unset.
func WithMetaVersion(version string) Option {
	return func(d *descriptor.Descriptor) { d.Meta.Version = version }
}

// WithProvider sets the provider name. An empty string leaves it unset.
func WithProvider(name string) Option {
	return func(d *descriptor.Descriptor) { d.Component.Provider.Name = name }
}

// WithResources sets the resources. A slice passed with ... is kept as is, so an empty
// non-nil slice stays non-nil.
func WithResources(resources ...descriptor.Resource) Option {
	return func(d *descriptor.Descriptor) { d.Component.Resources = resources }
}

// WithReferences sets the component references.
func WithReferences(refs ...descriptor.Reference) Option {
	return func(d *descriptor.Descriptor) { d.Component.References = refs }
}

// WithLabels sets the component labels.
func WithLabels(labels ...descriptor.Label) Option {
	return func(d *descriptor.Descriptor) { d.Component.Labels = labels }
}
