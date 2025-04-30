// Package componentversionrepository provides an implementation for the OCMComponentVersionRepository contract.
// It defines handlers, implementations, and a registry to interact with a repository.
// The registry supports registering outside plugins backed by a binary implementation and internal plugins using
// RegisterInternalComponentVersionRepositoryPlugin function. The usage of that function would look something like this:
//
//	scheme := runtime.NewScheme()
//	repository.MustAddToScheme(scheme)
//	if err := componentversionrepository.RegisterInternalComponentVersionRepositoryPlugin(
//		scheme,
//		&Plugin{scheme: scheme, memory: inmemory.New()},
//		&v1.OCIRepository{},
//	); err != nil {
//		panic(err)
//	}
//
// The package includes functionality for:
//   - Adding and retrieving component versions
//   - Handling local resources related to component versions
//   - Plugin-based registry for managing repository plugins
//   - Type-safe plugin wrappers for interacting with repository resources
//
// The package is divided into three key sections:
// - **Handlers**: Functions that handle HTTP requests related to component version and resource operations.
// - **Implementations**: Core logic for interacting with the repository and executing component version and resource operations.
// - **Registry**: Mechanisms for managing plugins and their registrations within the repository system.
package componentversionrepository
