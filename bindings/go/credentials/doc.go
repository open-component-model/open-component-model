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
//
// - Direct credential resolution through the graph
// - Plugin-based credential resolution for extensibility
// - Support for repository-specific credential handling
// - Thread-safe operations with synchronized DAG implementation
// - Flexible identity-based credential lookup with wildcard support
// - Caching of resolved credentials for improved performance
// - Concurrent resolution of repository credentials
//
// # Core Concept
//
// The concept is based on 4 data structures:
//
//   - Consumer Identities: These are unique identifiers that require / consume credentials. They are always
//     represented as [runtime.Identity].
//   - Credentials: These are always key value pairs in the form of map[string]string,
//     they can also be considered the "provider" counter-part to the consumer
//   - A Directed Acyclic Graph (DAG): This is a graph structure where each node represents a consumer identity
//     and its associated credentials. The edges between nodes represent the dependencies between
//     different consumer identities and their credentials.
//   - The [ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime.Config] structure:
//     This is the configuration structure that defines both definitions containing the consumer identities,
//     and the credentials as well as relationships through them.
//
// The most basic config example links one Consumer to one Credential:
//
//	type: credentials.config.ocm.software
//	consumers:
//	- identity:
//	    type: OCIRegistry/v1
//	    hostname: docker.io
//	  credentials:
//	    - type: Credentials/v1
//	      properties:
//	        username: "foobar"
//
// This is ingested into the graph via [ToGraph]. Note that a graph configuration may need to be converted from
// its serialization format to the internal runtime representation.
//
// # Multi-Identity and Credential Mappings
//
// The credential system supports complex credential resolution through a graph-based approach.
// Here's an example showing how credentials could be chained (this makes use of a CredentialPlugin and a custom type):
//
//	type: credentials.config.ocm.software
//	consumers:
//	- identity:
//	    type: OCIRegistry/v1
//	    hostname: quay.io
//	  credentials:
//	    - type: HashiCorpVault/v1alpha1
//	      serverURL: "https://myvault.example.com/"
//	      mountPath: "my-engine/my-engine-root"
//	      path: "my/path/to/my/secret"
//	      credentialsName: "my-secret-name"
//	- identity:
//	    type: HashiCorpVault/v1alpha1
//	    hostname: myvault.example.com
//	  credentials:
//	    - type: Credentials/v1
//	      properties:
//	        role_id: "repository.vault.com-role"
//	        secret_id: "repository.vault.com-secret"
//
// In this example:
//  1. A Docker registry (quay.io) receives credentials from a HashiCorp Vault Instance.
//  2. To access Vault, we need role_id and secret_id credentials, which in turn come from the credentials
//     defined in the second consumer identity. Thus, the credential properties specified for the first consumer
//     (the Docker registry) are themselves used to request credentials from the graph.
//
// The system can also handle multiple identities for the same consumer:
//
//	+------------------+     +----------------------+    +-------------------+
//	|  OCIRegistry/v1  |     |  HashiCorpVault/v1  | --> |  Credentials/v1   |
//	|  hostname:       | --> |  hostname:          |     |  role_id:   ...   |
//	|  quay.io         |     |  myvault.example.com|     |  secret_id: ...   |
//	+------------------+     +----------------------+    |                   |
//	                                                     |                   |
//	+------------------+     +----------------------+    |                   |
//	|  OCIRegistry/v1  | --> |  HashiCorpVault/v1  | --> |                   |
//	|  hostname:       |     |  hostname:          |     +-------------------+
//	|  docker.io       |     |  myvault.example.com|
//	+------------------+     +----------------------+
//
// In this case, both Docker registries share the same Vault credentials as dependency.
//
// # Multiple Credential Sources and Merging behavior
//
// It is possible to define multiple credential sources for consumer identities. In this case
// the system will merge them in order. Assume the following example:
//
//	type: credentials.config.ocm.software
//	consumers:
//	- identity:
//	    type: OCIRegistry/v1
//	    hostname: docker.io
//	  credentials:
//	    - type: Credentials/v1
//	      properties:
//	        username: "foobar"
//	    - type: Credentials/v1
//	      properties:
//	        password: "foobar"
//
// In this case, the credentials provided for docker.io will be merged into a single credential map. In case of
// conflicts, the last credential will override preceding providers.
//
// # Wildcard Support
//
// The system supports wildcard matching for consumer identities via [runtime.IdentityAttributePath].
// This allows for flexible credential resolution for providers with glob-like matching capabilities.
//
// For example, the following configuration
//
//	type: credentials.config.ocm.software
//	consumers:
//	- identity:
//	    type: OCIRegistry/v1
//	    hostname: ghcr.io/my-org/*
//	  credentials:
//	    - type: Credentials/v1
//	      properties:
//	        username: "foobar"
//
// Makes use of [runtime.Identity.Match] and [runtime.IdentityMatchesPath] to allow any of the following resolutions:
//
//	creds, err := graph.Resolve(ctx, runtime.Identity{"path": "ghcr.io/my-org/testrepo"}) // matches
//	creds, err := graph.Resolve(ctx, runtime.Identity{"path": "ghcr.io/my-org/some-other"}) // matches
//	creds, err := graph.Resolve(ctx, runtime.Identity{"path": "ghcr.io/my-other-org/path"}) // does not match
//
// Note that wildcard matches always get resolved AFTER equivalence matches. This means that if two identities
// exist, one with a wildcard and one with an exact match, the exact match will always be preferred.
//
// # Plugins and Extensibility through custom Types
//
// The Graph itself only supports resolution of [ocm.software/open-component-model/bindings/go/credentials/spec/config/v1.DirectCredentials].
// Any Type that is not a DirectCredentials needs a supporting CredentialPlugin implementation for successful resolution.
//
// CredentialPlugin - Extending the Graph and with new Credential Sources.
//
// This standard type of plugin provides custom credential resolution logic for credential consumers in the graph
//   - It maps custom credential [runtime.Type]'s (such as a Vault Integration to HashiCorpVault/v1alpha1) to an implementation
//   - Then, it allows for receiving credentials from external sources that are not known in the graph
//   - The plugin is responsible for implementing the logic to resolve credentials for the custom type
//   - The Graph will call the plugin to resolve credentials when it encounters a custom type and also passes it
//     any credentials that are available for it. To determine which credentials are available, the plugin
//     gets called with CredentialPlugin.GetConsumerIdentity with the credential identity as input arguments.
//     This returns the consumer identity for the custom type.
//
// # Fallback Resolution with Repositories
//
// While the Graph is useful for deterministic resolution and linking of credentials, oftentimes it is not possible
// to know which credentials are needed exactly for a specific repository. This is where the RepositoryPlugin comes into play.
// Inside the [ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime.Config] it is possible to define
// a set of available credential repositories which act as fallback providers in case the graph does not yield any results.
//
// These kind of plugins are useful when
// - The credentials are not known in advance
// - The credentials are available only through fetching / retrieving them from a remote source by value
//
// A well known example is the .dockerconfig, which provides access to docker credential helpers which cannot be
// included directly in the graph. This could be included as a configuration as such:
//
//	type: credentials.config.ocm.software
//	repositories:
//	- repository:
//	  type: DockerConfig/v1
//	  dockerConfigFile: "~/.docker/config.json"
//
// Note that repositories are always consulted AFTER no direct credentials are found.
// They act as a store that is independent from the graph.
//
// There are a few limitations to this:
//
// - The repository plugin can request credentials of its own via an identity provided with RepositoryPlugin.ConsumerIdentityForConfig
// - The credentials for a repository plugin can ONLY be resolved from the Graph via CredentialPlugin or [ocm.software/open-component-model/bindings/go/credentials/spec/config/v1.DirectCredentials]
// - The credentials required for a credential plugin CANNOT be resolved via a repository plugin
// - A repository plugin may be called for ANY consumer identity (currently there is no filtering implemented, but this may change)
//
// # Usage
//
//	config := &Config{...} // alternatively parse from yaml via serialization
//	opts := Options{
//	    GetCredentialPluginFn: myCredPluginResolver, // optional but needed if repository fallbacks are wanted
//	    GetRepositoryPluginFn: myRepoPluginResolver, // optional but needed if type support other than DirectCredentials are needed
//	}
//	graph, err := ToGraph(ctx, config, opts)
//	if err != nil {
//	    // handle error
//	}
//	creds, err := graph.Resolve(ctx, identity)
//
// The package is designed to be thread-safe and can be used concurrently from
// multiple goroutines. The DAG used in this package includes synchronization primitives
// to ensure safe concurrent access.
//
// The only Entrypoint to the graph is the [Graph.Resolve] method. This expects any identity and returns either an
// error or the successfully resolved credentials.
//
// Error Handling:
//   - [ErrNoDirectCredentials]: Returned when no direct credentials are found in the graph
//   - Various resolution errors are returned with detailed context
package credentials

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var _ = runtime.Identity{}
