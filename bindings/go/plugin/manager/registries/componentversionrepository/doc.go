// Package componentversionrepository implements a plugin-based system for managing OCM component version repositories.
// It provides a registry that supports both internal (Go-based) and external (binary) plugins through a unified interface.
//
// The system implements CRUD operations for component versions, local resources, and sources with credential management
// and type-safe plugin registration. External plugins communicate via UDS or TCP with JSON schema validation if defined.
//
// Register internal plugins using RegisterInternalComponentVersionRepositoryPlugin:
//
//	scheme := runtime.NewScheme()
//	repository.MustAddToScheme(scheme)
//	if err := componentversionrepository.RegisterInternalComponentVersionRepositoryPlugin(
//		scheme,
//		registry,
//		&CapabilitySpec{scheme: scheme, memory: inmemory.New()},
//		&v1.OCIRepository{},
//	); err != nil {
//		panic(err)
//	}
//
// Functionality:
//   - Component version lifecycle management (add, get, list)
//   - Local resource and source storage with blob handling
//   - CapabilitySpec discovery and credential consumer identity resolution
//   - External plugin communication with automatic lifecycle management
//   - Type conversion between internal runtime types and plugin contract types
//
// Architecture components:
//   - Registry: CapabilitySpec registration, lifecycle management, and thread-safe access
//   - Handlers: HTTP request/response processing with authentication and validation
//   - Converters: Data transformation between internal and external plugin formats
//   - Contracts: Type-safe interfaces defining plugin capabilities
package componentversionrepository
