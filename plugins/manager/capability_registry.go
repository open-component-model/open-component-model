package manager

import "fmt"

// registry contains a map of capabilities with a set of endpoints.
// Endpoints define the endpoint and the corresponding schema if any.
var registry map[string]map[string][]Endpoint

// RegisterCapability can be called from an init() method to register a capability.
// The capability provider must be imported here, for the registration to occur.
// If the capability already exists, this will return an error. In that case, the registration
// MUST panic.
// TODO: Figure out if this is needed at all. Would be nice, but correlating the endpoints provided
// here by the hardcoded handler Struct fields would be a bit of a pain in the butt.
func RegisterCapability(typ string, capability string, endpoints []Endpoint) error {
	if _, ok := registry[typ]; ok {
		return fmt.Errorf("type %s already registered", typ)
	}

	if _, ok := registry[capability]; ok {
		return fmt.Errorf("capability %s already registered", capability)
	}

	if registry[typ] == nil {
		registry[typ] = make(map[string][]Endpoint)
	}

	registry[typ][capability] = endpoints

	return nil
}

// GetEndpointsForType returns the set of configured endpoints for a given capability and type that have been registered.
func GetEndpointsForType(typ, capability string) ([]Endpoint, error) {
	if _, ok := registry[typ]; ok {
		return nil, fmt.Errorf("type %s already registered", typ)
	}

	if _, ok := registry[capability]; ok {
		return nil, fmt.Errorf("capability %s already registered", capability)
	}

	return registry[typ][capability], nil
}
