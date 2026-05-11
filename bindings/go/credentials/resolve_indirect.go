package credentials

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ErrNoIndirectCredentials is returned when no indirect credentials are found in the graph.
// This can happen if no repository plugin is configured or if no repository plugin can resolve
// credentials for the given identity.
var ErrNoIndirectCredentials = errors.New("no indirect credentials found in graph")

// resolveFromRepository is invoked when the DAG does not yield direct credentials.
// The method ensures that successful resolutions are cached for subsequent calls.
func (g *Graph) resolveFromRepository(ctx context.Context, identity runtime.Identity) (runtime.Typed, error) {
	node := identity.String()

	if credentials, cached := g.getCredentials(node); cached {
		return credentials, nil
	}

	plugin, err := g.repositoryPluginProvider.GetRepositoryPlugin(ctx, identity)
	if err != nil {
		// in case of an error, we try to resolve the credentials using the AnyConsumerIdentityType
		// this is a fallback resolution mechanism intended for plugins that do not mind which
		// consumer identity type is used.
		fallback := identity.DeepCopy()
		fallback.SetType(AnyConsumerIdentityType)
		var anyErr error
		plugin, anyErr = g.repositoryPluginProvider.GetRepositoryPlugin(ctx, fallback)
		// Independently of the actual error, we return ErrNoIndirectCredentials because, in fact, we cannot provide
		// indirect credentials. The caller should decide how to handle the error.
		if anyErr != nil {
			return nil, errors.Join(err, anyErr, ErrNoIndirectCredentials)
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
		if cfgIdentity, err := plugin.ConsumerIdentityForConfig(ctx, cfg); err == nil {
			// Attempt to resolve direct credentials from the graph.
			// NOTE: This explicitly does not allow recursing from repository to another repository.
			// This is because the usage of repository credentials resolved by other repository credentials
			// would require dynamic recursion detection via stack and would make the code significantly more complex.
			if typed, err := g.resolveFromGraph(ctx, cfgIdentity); err == nil {
				credentials = typedToMap(typed)
			}
		}
		slog.DebugContext(ctx, "Resolving credentials via repository", "identity", identity, "config", cfg)
		credentials, err := plugin.Resolve(ctx, cfg, identity, credentials)

		mu.Lock()
		defer mu.Unlock()

		switch {
		case err != nil:
			slog.DebugContext(ctx, "repository plugin failed to resolve credentials", slog.Any("identity", identity), slog.Any("config", cfg.GetType()), slog.Any("error", err))
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

	// Only check for nil, empty credentials might mean resolved but no username, etc. are provided
	if resolved == nil {
		if len(errs) > 0 {
			// If we have errors and no resolved credentials, we have to assume that the credential lookup failed due errors in the plugins.
			return nil, errors.Join(ErrUnknown, fmt.Errorf("an error occurred in one or multiple repositories while trying to resolve credentials for identity ... %q: %w", node, errors.Join(errs...)))
		}

		// If we get here, then all repository plugins failed to resolve credentials.
		// This is not an error, but rather a signal that the identity could not be resolved indirectly.
		return nil, errors.Join(ErrNoIndirectCredentials, fmt.Errorf("no repository plugin could resolve credentials for identity %q", node))
	}

	// Wrap and cache the resolved credentials.
	typed := &v1.DirectCredentials{
		Type:       runtime.NewVersionedType(v1.CredentialsType, v1.Version),
		Properties: resolved,
	}
	g.setCredentials(node, typed)

	return typed, nil
}
