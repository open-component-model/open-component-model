package manager

// CredentialRegistry contains the plugins that are in this registry.
type CredentialRegistry struct {
	registry map[string]map[string]any
}

func NewCredentialRegistry() *CredentialRegistry {
	return &CredentialRegistry{
		registry: make(map[string]map[string]any),
	}
}
