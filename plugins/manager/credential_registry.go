package manager

// CredentialRegistry contains the plugins that are in this registry.
// TODO: Figure out what the heck this returns, because it's not a plugin since
// that would be a circular dependency. And the key? It's probably cap/type?
type CredentialRegistry struct {
	registry map[string]map[string]any
}

func NewCredentialRegistry() *CredentialRegistry {
	return &CredentialRegistry{
		registry: make(map[string]map[string]any),
	}
}
