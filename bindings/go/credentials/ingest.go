package credentials

import (
	"context"
	"fmt"
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
// 1. Extracts direct credentials from each consumer
// 2. Separates direct credentials from those requiring plugin-based resolution
// 3. Merges direct credentials for each identity
// 4. Adds identity nodes to the graph
// 5. Stores direct credentials on their respective identity nodes
// 6. Returns the remaining consumers with plugin-based credentials that couldnt be processed yet
//
// The function returns
//   - left-over consumers: Contains only consumers that still have plugin-based credentials to process, all direct
//     credentials are already stored in the graph.
func processDirectCredentials(g *Graph, config *cfgRuntime.Config) ([]cfgRuntime.Consumer, error) {
	directPerIdentity := make(map[string]map[string]string)
	consumers := make([]cfgRuntime.Consumer, 0, len(config.Consumers))

	for _, consumer := range config.Consumers {
		direct, remaining, err := extractDirect(consumer.Credentials)
		if err != nil {
			return nil, fmt.Errorf("extracting consumer credentials failed: %w", err)
		}
		consumer.Credentials = remaining

		if len(direct) > 0 {
			for _, identity := range consumer.Identities {
				node := identity.String()
				if existing, ok := (directPerIdentity)[node]; ok {
					maps.Copy(existing, direct)
				} else {
					(directPerIdentity)[node] = direct
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

	for node, credentials := range directPerIdentity {
		g.setCredentials(node, v1.CredentialsFromMap(credentials))
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
	if g.credentialPluginProvider == nil {
		return fmt.Errorf("no credential plugin provider available")
	}
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
