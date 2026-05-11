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
	consumers, err := processDirectCredentials(ctx, g, config)
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
func processDirectCredentials(ctx context.Context, g *Graph, config *cfgRuntime.Config) ([]cfgRuntime.Consumer, error) {
	typedPerIdentity := make(map[string]runtime.Typed)
	directPerIdentity := make(map[string]*v1.DirectCredentials)
	consumers := make([]cfgRuntime.Consumer, 0, len(config.Consumers))

	for _, consumer := range config.Consumers {
		resolved, remaining, err := extractResolvable(ctx, g, consumer.Credentials)
		if err != nil {
			return nil, fmt.Errorf("extracting consumer credentials failed: %w", err)
		}
		consumer.Credentials = remaining

		if resolved != nil {
			for _, identity := range consumer.Identities {
				node := identity.String()

				if resolvedDC, ok := resolved.(*v1.DirectCredentials); ok {
					// Accumulate DirectCredentials separately; they serve as fallback only.
					if existing, ok := directPerIdentity[node]; ok {
						maps.Copy(existing.Properties, resolvedDC.Properties)
					} else {
						clone := *resolvedDC
						clone.Properties = make(map[string]string, len(resolvedDC.Properties))
						maps.Copy(clone.Properties, resolvedDC.Properties)
						directPerIdentity[node] = &clone
					}
				} else {
					// Concrete typed credential — last typed credential wins.
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

	// Typed credentials take priority; use DirectCredentials only for identities
	// that have no concrete typed credential.
	for node, dc := range directPerIdentity {
		if _, hasTyped := typedPerIdentity[node]; !hasTyped {
			typedPerIdentity[node] = dc
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
//  1. Credentials resolvable without plugins — either typed credentials known to the CredentialTypeScheme,
//     or DirectCredentials (Credentials/v1). Returns the first successfully resolved typed credential.
//  2. Remaining credentials that require plugin-based resolution.
func extractResolvable(ctx context.Context, g *Graph, creds []runtime.Typed) (runtime.Typed, []runtime.Typed, error) {
	var resolved runtime.Typed
	var mergedDirect *v1.DirectCredentials
	var remaining []runtime.Typed

	for _, cred := range creds {
		if cred.GetType().IsEmpty() {
			return nil, nil, fmt.Errorf("credential type is empty")
		}

		// Try the credential type scheme first (e.g. HelmCredentials/v1, OCICredentials/v1).
		// Only attempt deserialization for types explicitly registered in the scheme.
		// Schemes configured with WithAllowUnknown would otherwise round-trip any type
		// as *runtime.Raw, making unregistered credentials look "resolved" and preventing
		// plugin edge creation.
		if credScheme := g.credentialTypeScheme(); credScheme != nil && credScheme.IsRegistered(cred.GetType()) {
			if typed, err := credScheme.NewObject(cred.GetType()); err == nil {
				if err := credScheme.Convert(cred, typed); err == nil {
					if resolved == nil {
						resolved = typed
					} else {
						// Already have a resolved typed credential — pass additional ones
						// to plugin-based resolution to avoid silent loss.
						remaining = append(remaining, cred)
					}
					continue
				} else {
					slog.WarnContext(ctx, "credential type scheme conversion failed",
						"type", cred.GetType().String(), "error", err)
				}
			}
		}

		// Try DirectCredentials (Credentials/v1 and its aliases).
		// Accumulate into mergedDirect; they are only used as a fallback
		// when no concrete typed credential was resolved.
		var direct v1.DirectCredentials
		if err := scheme.Convert(cred, &direct); err == nil {
			if mergedDirect == nil {
				mergedDirect = &v1.DirectCredentials{
					Type:       direct.Type,
					Properties: make(map[string]string, len(direct.Properties)),
				}
				maps.Copy(mergedDirect.Properties, direct.Properties)
			} else {
				maps.Copy(mergedDirect.Properties, direct.Properties)
			}
			continue
		}

		// Unknown type — pass to plugin-based resolution.
		remaining = append(remaining, cred)
	}

	// Typed credentials take priority; use merged DirectCredentials only as fallback.
	if resolved == nil && mergedDirect != nil {
		resolved = mergedDirect
	}

	return resolved, remaining, nil
}
