package credentials

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	cfgRuntime "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var scheme = runtime.NewScheme()

func init() {
	v1.MustRegister(scheme)
}

// Options represents the configuration options for creating a credential graph.
type Options struct {
	RepositoryPluginProvider
	CredentialPluginProvider
	CredentialRepositoryTypeScheme *runtime.Scheme
	// CredentialTypeSchemeProvider provides access to known credential types (e.g. HelmHTTPCredentials/v1).
	CredentialTypeSchemeProvider CredentialTypeSchemeProvider
	// ConsumerIdentityTypeScheme contains the known consumer identity types (e.g. Wget/v1).
	// If set, config-authored consumer identities whose type is registered as an alias
	// are canonicalized to the unversioned spelling of their default type at ingest
	// time (e.g. HTTP or HTTP/v1 resolve to Wget).
	// Identity lookups always use the unversioned spelling, so canonicalization has to
	// preserve matching behavior for every spelling the scheme knows.
	ConsumerIdentityTypeScheme *runtime.Scheme
}

// ToGraph creates a new credential graph from the provided configuration and options.
// It initializes the graph structure and ingests the configuration into the graph.
func ToGraph(ctx context.Context, config *cfgRuntime.Config, opts Options) (*Graph, error) {
	g := &Graph{
		syncedDag:                    newSyncedDag(),
		credentialPluginProvider:     opts.CredentialPluginProvider,
		repositoryPluginProvider:     opts.RepositoryPluginProvider,
		credentialTypeSchemeProvider: opts.CredentialTypeSchemeProvider,
		consumerIdentityTypeScheme:   opts.ConsumerIdentityTypeScheme,
	}

	if err := ingest(ctx, g, config, opts.CredentialRepositoryTypeScheme); err != nil {
		return nil, err
	}

	return g, nil
}

// Graph represents a credential resolution graph that manages repository configurations
// and provides functionality to resolve credentials for given identities.
// It supports both direct credential resolution (map) and typed credential resolution.
type Graph struct {
	repositoryConfigurationsMu sync.RWMutex    // Mutex to protect access to repository configurations
	repositoryConfigurations   []runtime.Typed // List of repository configurations parsed

	*syncedDag // The underlying DAG structure for managing dependencies

	repositoryPluginProvider     RepositoryPluginProvider     // injection for resolving custom repository types
	credentialPluginProvider     CredentialPluginProvider     // injection for resolving custom credential types
	credentialTypeSchemeProvider CredentialTypeSchemeProvider // optional: enables typed credential ingestion
	consumerIdentityTypeScheme   *runtime.Scheme              // optional: enables canonicalization of consumer identity alias types
}

// canonicalizeConsumerIdentity resolves the type of a config-authored consumer identity
// through the consumer identity type scheme. Registered aliases resolve to the unversioned
// spelling of their default type, which is the spelling identity producers use when matching.
// Identities that don't match anything registered are returned unchanged.
func (g *Graph) canonicalizeConsumerIdentity(identity runtime.Identity) runtime.Identity {
	if g.consumerIdentityTypeScheme == nil {
		return identity
	}
	typ, err := identity.ParseType()
	if err != nil {
		return identity
	}
	canonical, ok := g.consumerIdentityTypeScheme.ResolveCanonicalType(typ)
	if !ok {
		return identity
	}
	canonicalized := identity.Clone()
	canonicalized.SetType(runtime.NewUnversionedType(canonical.GetName()))
	return canonicalized
}

// credentialTypeScheme returns the underlying scheme from the credential type
// provider, or nil if no provider is configured.
func (g *Graph) credentialTypeScheme() *runtime.Scheme {
	if g.credentialTypeSchemeProvider == nil {
		slog.Warn("no credential type scheme provider configured, typed credential ingestion will fallback to DirectCredentials")
		return nil
	}
	credentialTypeScheme := g.credentialTypeSchemeProvider.GetCredentialTypeScheme()
	if credentialTypeScheme == nil {
		slog.Warn("credential type scheme provider returned nil, typed credential ingestion will fallback to DirectCredentials")
	}
	return credentialTypeScheme
}

// Compile-time interface check.
var _ Resolver = (*Graph)(nil)

// Resolve resolves credentials for the given identity and returns them as a runtime.Typed.
// The returned type depends on what was configured: a registered typed credential
// (e.g. *HelmHTTPCredentials) when a CredentialTypeSchemeProvider is configured and the
// config uses a known typed credential, otherwise *v1.DirectCredentials.
func (g *Graph) Resolve(ctx context.Context, identity runtime.Identity) (runtime.Typed, error) {
	if _, err := identity.ParseType(); err != nil {
		err = errors.Join(ErrUnknown, err)
		return nil, fmt.Errorf("to be resolved from the credential graph, a consumer identity type is required: %w", err)
	}

	// Attempt direct resolution via the DAG.
	creds, err := g.resolveFromGraph(ctx, identity)

	// fall back to indirect resolution if we have a repository plugin provider
	// otherwise leave error as is
	if g.repositoryPluginProvider != nil && errors.Is(err, ErrNoDirectCredentials) {
		creds, err = g.resolveFromRepository(ctx, identity)
	}

	if err != nil {
		if errors.Is(err, ErrNoDirectCredentials) || errors.Is(err, ErrNoIndirectCredentials) {
			err = errors.Join(ErrNotFound, err)
			return nil, fmt.Errorf("failed to resolve credentials for identity %q: %w", identity.String(), err)
		}

		err = errors.Join(ErrUnknown, err)
		return nil, fmt.Errorf("failed to resolve credentials for identity %q: %w", identity.String(), err)
	}

	if _, ok := creds.(*v1.DirectCredentials); ok {
		slog.Warn("resolved credentials for identity using direct credentials, consider migrating to typed credentials. "+
			"Follow our migration guide for more details: https://ocm.software/docs/how-to/migrate-legacy-credentials/", "identity", identity.String())
	}

	return creds, nil
}
