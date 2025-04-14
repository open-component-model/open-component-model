// Package credentials provides a flexible and extensible credential management system
// for the Open Component Model (OCM). It implements a graph-based approach to
// credential resolution, supporting both direct and plugin-based credential handling.
//
// The package's core functionality revolves around the Graph type, which represents
// a directed acyclic graph (DAG) of credential relationships. This graph can be
// constructed from a configuration and used to resolve credentials for various
// identities.
//
// Key Features:
// - Direct credential resolution through the graph
// - Plugin-based credential resolution for extensibility
// - Support for repository-specific credential handling
// - Thread-safe operations with synchronized DAG implementation
// - Flexible identity-based credential lookup with wildcard support
// - Caching of resolved credentials for improved performance
// - Concurrent resolution of repository credentials
//
// # Multi-Identity and Credential Mappings
//
// The credential system supports complex credential resolution through a graph-based approach.
// Here's a simple example showing how credentials can be chained:
//
//	+------------------+     +----------------------+     +------------------+
//	|  OCIRegistry/v1  | --> |  HashiCorpVault/v1   | --> |  Credentials/v1  |
//	|  hostname:       |     |  hostname:           |     |  role_id:        |
//	|  quay.io         |     |  myvault.example.com |     |  myvault.example |
//	|                  |     |                      |     |  .com-role       |
//	|                  |     |                      |     |  secret_id:      |
//	|                  |     |                      |     |  myvault.example |
//	|                  |     |                      |     |  .com-secret     |
//	+------------------+     +----------------------+     +------------------+
//
// In this example:
// 1. A Docker registry (quay.io) needs credentials
// 2. These credentials are stored in HashiCorp Vault
// 3. To access Vault, we need role_id and secret_id credentials
//
// The system can also handle multiple identities for the same consumer:
//
//	+------------------+     +----------------------+     +------------------+
//	|  OCIRegistry/v1  |     |  HashiCorpVault/v1  | --> |  Credentials/v1  |
//	|  hostname:       | --> |  hostname:          |     |  role_id:        |
//	|  quay.io         |     |  myvault.example.com|     |  myvault.example |
//	+------------------+     +----------------------+     |  .com-role       |
//	                                                     |  secret_id:      |
//	+------------------+     +----------------------+     |  myvault.example |
//	|  OCIRegistry/v1  | --> |  HashiCorpVault/v1  | --> |  .com-secret     |
//	|  hostname:       |     |  hostname:          |     +------------------+
//	|  docker.io       |     |  myvault.example.com|
//	+------------------+     +----------------------+
//
// In this case, both Docker registries share the same Vault credentials as dependency.
//
// The package supports two main types of plugins:
// - RepositoryPlugin: Handles repository-specific credential resolution
//   - Provides repository configuration type support
//   - Maps repository configurations to consumer identities
//   - Resolves credentials for specific repository configurations
//
// - CredentialPlugin: Provides custom credential resolution logic
//   - Maps credentials to consumer identities
//   - Resolves credentials for specific identities
//
// The credential resolution process follows these steps:
// 1. Direct resolution through the DAG
// 2. If direct resolution fails, fall back to indirect resolution
// 3. Indirect resolution attempts to resolve credentials through repository plugins
// 4. Results are cached for future use
//
// Usage Example:
//
//	config := &Config{...}
//	opts := Options{
//	    GetRepositoryPluginFn: myRepoPlugin,
//	    GetCredentialPluginFn: myCredPlugin,
//	}
//	graph, err := ToGraph(ctx, config, opts)
//	if err != nil {
//	    // handle error
//	}
//	creds, err := graph.Resolve(ctx, identity)
//
// The package is designed to be thread-safe and can be used concurrently from
// multiple goroutines. The DAG used in includes synchronization primitives
// to ensure safe concurrent access.
//
// Error Handling:
// - ErrNoDirectCredentials: Returned when no direct credentials are found in the graph
// - Various resolution errors are returned with detailed context
package credentials
