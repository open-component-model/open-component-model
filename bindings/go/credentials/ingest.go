package credentials

import (
	"context"
	"fmt"
	"maps"

	. "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"

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
func ingest(ctx context.Context, g *Graph, config *Config, repoTypeScheme *runtime.Scheme) error {
	directPerIdentity := make(map[string]map[string]string)
	consumers := make([]Consumer, 0, len(config.Consumers))

	if err := processDirectCredentials(g, config, &directPerIdentity, &consumers); err != nil {
		return err
	}

	if err := processPluginBasedEdges(ctx, g, consumers); err != nil {
		return err
	}

	if err := processRepositoryConfigurations(g, config, repoTypeScheme); err != nil {
		return err
	}

	return nil
}

// processDirectCredentials handles the first phase of credential processing:
// 1. Extracts direct credentials from each consumer
// 2. Separates direct credentials from those requiring plugin-based resolution
// 3. Merges direct credentials for each identity
// 4. Adds identity nodes to the graph
// 5. Stores direct credentials on their respective identity nodes
//
// The function updates both the directPerIdentity map and the consumers slice:
// - directPerIdentity: Maps identity strings to their direct credentials
// - consumers: Contains only consumers that still have plugin-based credentials to process
func processDirectCredentials(g *Graph, config *Config, directPerIdentity *map[string]map[string]string, consumers *[]Consumer) error {
	for _, consumer := range config.Consumers {
		direct, remaining, err := extractDirect(consumer.Credentials)
		if err != nil {
			return fmt.Errorf("extracting consumer credentials failed: %w", err)
		}
		consumer.Credentials = remaining

		if len(direct) > 0 {
			for _, identity := range consumer.Identities {
				node := identity.String()
				if existing, ok := (*directPerIdentity)[node]; ok {
					maps.Copy(existing, direct)
				} else {
					(*directPerIdentity)[node] = direct
				}

				if err := g.addIdentity(identity); err != nil {
					return err
				}
			}
		}

		if len(consumer.Credentials) > 0 {
			*consumers = append(*consumers, consumer)
		}
	}

	for node, credentials := range *directPerIdentity {
		g.setCredentials(node, credentials)
	}

	return nil
}

// processPluginBasedEdges handles the second phase of credential processing:
// For each consumer identity that has plugin-based credentials:
// 1. Adds the consumer identity as a node in the graph
// 2. Resolves each plugin-based credential to get its identity
// 3. Adds the credential identity as a node in the graph
// 4. Creates an edge from the consumer identity to the credential identity
//
// This phase builds the relationships between identities that require
// plugin-based resolution of credentials.
func processPluginBasedEdges(ctx context.Context, g *Graph, consumers []Consumer) error {
	for _, consumer := range consumers {
		for _, identity := range consumer.Identities {
			node := identity.String()
			if err := g.addIdentity(identity); err != nil {
				return err
			}

			for _, credential := range consumer.Credentials {
				plugin, err := g.getCredentialPlugin(ctx, credential)
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
				if err := g.addEdge(node, credentialNode); err != nil {
					return fmt.Errorf("could not add edge from consumer identity %q to credential identity %q: %w", identity, credentialIdentity, err)
				}
			}
		}
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
func processRepositoryConfigurations(g *Graph, config *Config, repoTypeScheme *runtime.Scheme) error {
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

// extractDirect extracts and separates a slice of raw credentials into two groups:
// 1. Direct credentials of type CredentialsTypeV1 (which are decoded into a merged map).
// 2. All remaining credentials that require plugin-based resolution.
// Returns the merged direct credentials and the slice of remaining credentials.
func extractDirect(creds []runtime.Typed) (map[string]string, []runtime.Typed, error) {
	direct := map[string]string{}
	var remaining []runtime.Typed

	// Iterate over each credential.
	for _, cred := range creds {
		if cred.GetType().IsEmpty() {
			return nil, nil, fmt.Errorf("credential type is empty")
		}

		typed := v1.DirectCredentials{}
		if err := scheme.Convert(cred, &typed); err != nil {
			remaining = append(remaining, cred)
		}

		maps.Copy(direct, typed.Properties)
	}
	return direct, remaining, nil
}
