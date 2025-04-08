package manager

// Registry provides an abstraction around how to retrieve a plugin from a plugin registry.
type Registry interface {
	GetPlugin(capability Capability) (any, error)
	AddPlugin(id string, plugin any, caps Capability) error
}
