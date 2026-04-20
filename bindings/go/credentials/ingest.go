package credentials

import (
	"context"
	"fmt"
	"log/slog"
	"maps"

	cfgRuntime "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ingest processes the credential configuration by:
// 1. Extracting and processing direct credentials
// 2. Creating edges for plugin-based credentials
// 3. Processing repository configurations
//
// The function builds a graph where:
// - Nodes represent identities (both consumer and credential identities)
// - Edges represent relationships between identities
// - Direct credentials are stored on their respective identity nodes inside the vertex
// - Repository configurations are stored in the Graph for later use
func ingest(ctx context.Context, g *Graph, config *cfgRuntime.Config, repoTypeScheme *runtime.Scheme) error {
	validateConsumerIdentityTypes(ctx, g, config)

	consumers, err := processDirectCredentials(g, config)
	if err != nil {
		return fmt.Errorf("failed to process direct credentials: %w", err)
	}

	if err := processPluginBasedEdges(ctx, g, consumers); err != nil {
		return fmt.Errorf("failed to process edges based on plugins: %w", err)
	}

	if err := processRepositoryConfigurations(g, config, repoTypeScheme); err != nil {
		return fmt.Errorf("failed to process repository configurations: %w", err)
	}

	return nil
}

// processDirectCredentials handles the first phase of credential processing:
// 1. Extracts credentials resolvable without plugins (DirectCredentials or typed credentials known to the scheme)
// 2. Separates them from credentials requiring plugin-based resolution
// 3. Stores resolved credentials as runtime.Typed on their identity nodes
// 4. Returns remaining consumers with plugin-based credentials
func processDirectCredentials(g *Graph, config *cfgRuntime.Config) ([]cfgRuntime.Consumer, error) {
	typedPerIdentity := make(map[string]runtime.Typed)
	consumers := make([]cfgRuntime.Consumer, 0, len(config.Consumers))

	for _, consumer := range config.Consumers {
		resolved, remaining, err := extractResolvable(g, consumer.Credentials)
		if err != nil {
			return nil, fmt.Errorf("extracting consumer credentials failed: %w", err)
		}
		consumer.Credentials = remaining

		if resolved != nil {
			for _, identity := range consumer.Identities {
				node := identity.String()
				if existing, ok := typedPerIdentity[node]; ok {
					// If both are DirectCredentials, merge properties.
					// Otherwise last write wins (typed credentials replace generic ones).
					existingDC, okE := existing.(*v1.DirectCredentials)
					resolvedDC, okR := resolved.(*v1.DirectCredentials)
					if okE && okR {
						maps.Copy(existingDC.Properties, resolvedDC.Properties)
					} else {
						typedPerIdentity[node] = resolved
					}
				} else {
					typedPerIdentity[node] = resolved
				}

				if err := g.addIdentity(identity); err != nil {
					return nil, err
				}
			}
		}

		if len(consumer.Credentials) > 0 {
			consumers = append(consumers, consumer)
		}
	}

	for node, typed := range typedPerIdentity {
		g.setCredentials(node, typed)
	}

	return consumers, nil
}

// processPluginBasedEdges handles the second phase of credential processing:
// For each consumer identity that has plugin-based credentials, call processConsumerCredential
func processPluginBasedEdges(ctx context.Context, g *Graph, consumers []cfgRuntime.Consumer) error {
	for _, consumer := range consumers {
		for _, identity := range consumer.Identities {
			node := identity.String()
			if err := g.addIdentity(identity); err != nil {
				return err
			}
			for _, credential := range consumer.Credentials {
				if err := processConsumerCredential(ctx, g, credential, node, identity); err != nil {
					return fmt.Errorf("failed to process consumer credential: %w", err)
				}
			}
		}
	}
	return nil
}

// processConsumerCredential handles the processing of a single consumer credential:
// 1. Retrieves the appropriate plugin for the credential type
// 2. Resolves the consumer identity for the credential
// 3. Adds the credential identity as a node in the graph
// 4. Creates an edge from the consumer identity to the credential identity
func processConsumerCredential(ctx context.Context, g *Graph, credential runtime.Typed, node string, identity runtime.Identity) error {
	plugin, err := g.credentialPluginProvider.GetCredentialPlugin(ctx, credential)
	if err != nil {
		return fmt.Errorf("getting credential plugin failed: %w", err)
	}
	credentialIdentity, err := plugin.GetConsumerIdentity(ctx, credential)
	if err != nil {
		return fmt.Errorf("could not get consumer identity for %v: %w", credential, err)
	}
	if err := g.addIdentity(credentialIdentity); err != nil {
		return fmt.Errorf("could not add identity %q to graph: %w", credential, err)
	}

	credentialNode := credentialIdentity.String()
	if err := g.addEdge(node, credentialNode, map[string]any{
		"kind": "resolution-relevant",
	}); err != nil {
		return fmt.Errorf("could not add edge from consumer identity %q to credential identity %q: %w", identity, credentialIdentity, err)
	}
	return nil
}

// processRepositoryConfigurations handles the final phase of credential processing:
// For each repository configuration:
// 1. Creates a new typed object based on the repository type
// 2. Converts the repository configuration to the typed object
// 3. Stores the typed object in the graph's repository configurations
//
// This phase ensures that repository-specific configurations are properly
// stored and can be accessed when needed.
func processRepositoryConfigurations(g *Graph, config *cfgRuntime.Config, repoTypeScheme *runtime.Scheme) error {
	for _, repository := range config.Repositories {
		repository := repository.Repository
		typed, err := repoTypeScheme.NewObject(repository.GetType())
		if err != nil {
			return fmt.Errorf("could not create new object of type %q: %w", repository.GetType(), err)
		}
		if err := repoTypeScheme.Convert(repository, typed); err != nil {
			return fmt.Errorf("could not convert repository to typed object: %w", err)
		}
		g.repositoryConfigurationsMu.Lock()
		g.repositoryConfigurations = append(g.repositoryConfigurations, typed)
		g.repositoryConfigurationsMu.Unlock()
	}
	return nil
}

// extractResolvable separates credentials into two groups:
// 1. Credentials resolvable without plugins — either typed credentials known to the CredentialTypeScheme,
//    or DirectCredentials (Credentials/v1). Returns the first successfully resolved typed credential.
// 2. Remaining credentials that require plugin-based resolution.
func extractResolvable(g *Graph, creds []runtime.Typed) (runtime.Typed, []runtime.Typed, error) {
	var resolved runtime.Typed
	var remaining []runtime.Typed

	for _, cred := range creds {
		if cred.GetType().IsEmpty() {
			return nil, nil, fmt.Errorf("credential type is empty")
		}

		// Try the credential type scheme first (e.g. HelmCredentials/v1, OCICredentials/v1).
		if g.credentialTypeScheme() != nil {
			if typed, err := g.credentialTypeScheme().NewObject(cred.GetType()); err == nil {
				if err := g.credentialTypeScheme().Convert(cred, typed); err == nil {
					resolved = typed
					continue
				}
			}
		}

		// Try DirectCredentials (Credentials/v1 and its aliases).
		var direct v1.DirectCredentials
		if err := scheme.Convert(cred, &direct); err == nil {
			if resolved == nil {
				resolved = &direct
			} else if existingDC, ok := resolved.(*v1.DirectCredentials); ok {
				// Merge multiple DirectCredentials into one.
				maps.Copy(existingDC.Properties, direct.Properties)
			}
			continue
		}

		// Unknown type — pass to plugin-based resolution.
		remaining = append(remaining, cred)
	}

	return resolved, remaining, nil
}

// validateConsumerIdentityTypes checks that all consumer identity types in the config
// are registered in the ConsumerIdentityTypeScheme and that credential types are
// compatible with the identity type. Unknown types produce warnings — they are not
// rejected because plugins loaded later may introduce new types.
func validateConsumerIdentityTypes(ctx context.Context, g *Graph, config *cfgRuntime.Config) {
	if g.consumerIdentityTypeScheme() == nil {
		return
	}
	for _, consumer := range config.Consumers {
		for _, identity := range consumer.Identities {
			identityType, err := identity.ParseType()
			if err != nil {
				slog.WarnContext(ctx, "consumer identity has unparseable type",
					"identity", identity.String(), "error", err)
				continue
			}

			identityObj, err := g.consumerIdentityTypeScheme().NewObject(identityType)
			if err != nil {
				slog.WarnContext(ctx, "consumer identity type not registered in scheme",
					"type", identityType.String(),
					"identity", identity.String(),
				)
				continue
			}

			// If the identity type declares accepted credential types, validate
			// This does not fail on purpose - credentials should still be passed as the user configured them
			acceptor, ok := identityObj.(CredentialAcceptor)
			if !ok {
				continue
			}
			accepted := acceptor.AcceptedCredentialTypes()

			for _, cred := range consumer.Credentials {
				credType := cred.GetType()
				if credType.IsEmpty() || scheme.IsRegistered(credType) {
					// DirectCredentials/Credentials are always accepted
					continue
				}
				if !isAccepted(g.credentialTypeScheme(), credType, accepted) {
					slog.WarnContext(ctx, "credential type not accepted by identity type",
						"credentialType", credType.String(),
						"identityType", identityType.String(),
						"acceptedTypes", accepted,
					)
				}
			}
		}
	}
}

// isAccepted checks whether a credential type is in the list of accepted types.
// It first tries exact matching, then falls back to alias resolution through the
// scheme — so that e.g. "HelmHTTPCredentials" (unversioned alias) matches
// "HelmHTTPCredentials/v1" (default type) and vice versa.
func isAccepted(credentialTypeScheme *runtime.Scheme, credType runtime.Type, accepted []runtime.Type) bool {
	for _, a := range accepted {
		if a.Equal(credType) {
			return true
		}
	}
	if credentialTypeScheme == nil {
		return false
	}
	resolved := credentialTypeScheme.ResolveType(credType)
	for _, a := range accepted {
		if credentialTypeScheme.ResolveType(a).Equal(resolved) {
			return true
		}
	}
	return false
}
