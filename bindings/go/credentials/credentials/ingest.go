package credentials

import (
	"context"
	"fmt"
	"maps"

	. "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func ingest(ctx context.Context, g *Graph, config *Config, repoTypeScheme *runtime.Scheme) error {
	directPerIdentity := make(map[string]map[string]string)
	consumers := make([]Consumer, 0, len(config.Consumers))

	// STEP 1: Extract direct credentials from the configuration.
	// This step both precaches credentials and prunes consumers that do not require plugin-based resolution.
	for _, consumer := range config.Consumers {
		// Extract direct credentials and separate out any plugin-based credentials.
		direct, remaining, err := extractDirect(consumer.Credentials)
		if err != nil {
			return fmt.Errorf("extracting consumer credentials failed: %w", err)
		}
		consumer.Credentials = remaining

		if len(direct) > 0 {
			for _, identity := range consumer.Identities {
				node := identity.String()
				// Merge credentials if the identity already exists.
				if existing, ok := directPerIdentity[node]; ok {
					maps.Copy(existing, direct)
				} else {
					directPerIdentity[node] = direct
				}

				// Add the node as a vertex in the graph if it does not already exist.
				if err := g.addIdentityToGraph(identity); err != nil {
					return err
				}
			}
		}

		// Retain consumers that still have plugin-based credentials for further processing.
		if len(consumer.Credentials) > 0 {
			consumers = append(consumers, consumer)
		}
	}

	for node, credentials := range directPerIdentity {
		g.addToCache(node, credentials)
	}

	// STEP 2: Process plugin-based edges.
	// For each consumer identity, add an edge in the graph that links it to the identity
	// obtained by processing its plugin-based credentials.
	for _, consumer := range consumers {
		for _, identity := range consumer.Identities {
			node := identity.String()
			if err := g.addIdentityToGraph(identity); err != nil {
				return err
			}

			for _, credential := range consumer.Credentials {
				plugin, err := g.getCredentialPlugin(ctx, credential)
				if err != nil {
					return fmt.Errorf("getting credential plugin failed: %w", err)
				}
				credentialIdentity, err := plugin.GetConsumerIdentity(credential)
				if err != nil {
					return fmt.Errorf("could not get consumer identity for %v: %w", credential, err)
				}
				if err := g.addIdentityToGraph(credentialIdentity); err != nil {
					return fmt.Errorf("could not add identity %q to graph: %w", credential, err)
				}

				credentialNode := credentialIdentity.String()
				if err := g.addEdge(node, credentialNode); err != nil {
					return fmt.Errorf("could not add edge from consumer identity %q to credential identity %q: %w", identity, credentialIdentity, err)
				}
			}
		}
	}

	// STEP 3: Process repositoryConfigurations
	for _, repository := range config.Repositories {
		repository := repository.Repository
		typed, err := repoTypeScheme.NewObject(repository.GetType())
		if err != nil {
			return fmt.Errorf("could not create new object of type %q: %w", repository.GetType(), err)
		}
		if err := scheme.Convert(repository, typed); err != nil {
			return fmt.Errorf("could not convert repository to typed object: %w", err)
		}
		g.repositoryConfigurationsMu.Lock()
		g.repositoryConfigurations = append(g.repositoryConfigurations, typed)
		g.repositoryConfigurationsMu.Unlock()
	}

	return nil
}
