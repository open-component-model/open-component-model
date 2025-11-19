package credentials

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// resolveFromRepository is invoked when the DAG does not yield direct credentials.
// The method ensures that successful resolutions are cached for subsequent calls.
func (g *Graph) resolveFromRepository(ctx context.Context, identity runtime.Identity) (map[string]string, error) {
	if credentials, cached := g.getCredentials(identity.String()); cached {
		return credentials, nil
	}

	plugin, err := g.repositoryPluginProvider.GetRepositoryPlugin(ctx, identity)
	if err != nil {
		// in case of an error, we try to resolve the credentials using the AnyConsumerIdentityType
		// this is a fallback resolution mechanism intended for plugins that do not mind which
		// consumer identity type is used.
		identity := identity.DeepCopy()
		identity.SetType(AnyConsumerIdentityType)
		var anyErr error
		if plugin, anyErr = g.repositoryPluginProvider.GetRepositoryPlugin(ctx, identity); anyErr != nil {
			return nil, errors.Join(err, anyErr)
		}
	}

	// Variables for managing concurrent resolution.
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		resolved map[string]string
		errs     []error
	)

	// Create a cancellable context so that once one repository succeeds, the other goroutines can be cancelled.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	resolve := func(plugin RepositoryPlugin, cfg runtime.Typed) {
		defer wg.Done()

		var credentials map[string]string
		// Obtain the consumer identity for the repository configuration.
		if identity, err := plugin.ConsumerIdentityForConfig(ctx, cfg); err == nil {
			// Attempt to resolve direct credentials from the graph.
			// NOTE: This explicitly does not allow recursing from repository to another repository.
			// This is because the usage of repository credentials resolved by other repository credentials
			// would require dynamic recursion detection via stack and would make the code significantly more complex.
			credentials, _ = g.resolveFromGraph(ctx, identity)
		}
		slog.DebugContext(ctx, "Resolving credentials via repository", "identity", identity, "config", cfg)
		credentials, err := plugin.Resolve(ctx, cfg, identity, credentials)

		mu.Lock()
		defer mu.Unlock()

		switch {
		case err != nil:
			slog.WarnContext(ctx, "repository plugin failed to resolve credentials", slog.Any("identity", identity), slog.Any("config", cfg.GetType()), slog.Any("error", err))
			errs = append(errs, err)
		case resolved == nil:
			resolved = credentials
			cancel()
		}
	}

	g.repositoryConfigurationsMu.Lock()
	repositoryConfigurations := g.repositoryConfigurations
	g.repositoryConfigurationsMu.Unlock()

	for _, repoConfig := range repositoryConfigurations {
		wg.Add(1)
		go resolve(plugin, repoConfig)
	}

	wg.Wait()

	if resolved == nil {
		if len(errs) > 0 {
			slog.WarnContext(ctx, "an error occurred in one or multiple repositories while trying to resolve credentials for identity ...", slog.Any("errors", errs), slog.Any("identity", identity))
			err := fmt.Errorf("an error occurred in one or multiple repositories while trying to resolve credentials for identity ... %q: %w", identity.String(), errors.Join(errs...))
			return nil, err
		}

		// If we get here, then all repository plugins failed to resolve credentials.
		// This is not an error, but rather a signal that the identity could not be resolved indirectly.
		return nil, errors.Join(fmt.Errorf("no repository plugin could resolve credentials for identity %q", identity.String()), ErrNoIndirectCredentials)
	}

	// Cache the resolved credentials for future use.
	g.setCredentials(identity.String(), resolved)

	return resolved, nil
}
