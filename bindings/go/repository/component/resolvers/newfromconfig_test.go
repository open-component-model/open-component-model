package resolvers

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	resolverruntime "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/runtime"
	ocmv1spec "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/spec"
	resolverspec "ocm.software/open-component-model/bindings/go/configuration/resolvers/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// recordingProvider records the repository specs it is asked to build and the
// credentials handed to it, routing each spec to a distinct fake repository.
type recordingProvider struct {
	mu       sync.Mutex
	specs    []runtime.Typed
	credsFor map[string]runtime.Typed
}

func newRecordingProvider() *recordingProvider {
	return &recordingProvider{credsFor: map[string]runtime.Typed{}}
}

func (p *recordingProvider) GetComponentVersionRepositoryCredentialConsumerIdentity(_ context.Context, spec runtime.Typed) (runtime.Identity, error) {
	raw, _ := json.Marshal(spec)
	return runtime.Identity{"spec": string(raw)}, nil
}

func (p *recordingProvider) GetComponentVersionRepository(_ context.Context, spec runtime.Typed, creds runtime.Typed) (repository.ComponentVersionRepository, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.specs = append(p.specs, spec)
	raw, _ := json.Marshal(spec)
	p.credsFor[string(raw)] = creds
	return &recordingRepo{}, nil
}

func (p *recordingProvider) GetJSONSchemaForRepositorySpecification(_ runtime.Type) ([]byte, error) {
	return nil, nil
}

type recordingRepo struct {
	repository.ComponentVersionRepository
}

func (r *recordingRepo) GetComponentVersion(_ context.Context, component, version string) (*descriptor.Descriptor, error) {
	d := &descriptor.Descriptor{}
	d.Component.Name = component
	d.Component.Version = version
	return d, nil
}

// staticCredentials returns a fixed credential for every identity.
type staticCredentials struct {
	cred runtime.Typed
}

func (s staticCredentials) Resolve(_ context.Context, _ runtime.Identity) (runtime.Typed, error) {
	return s.cred, nil
}

var _ credentials.Resolver = staticCredentials{}

func rawRepo(t *testing.T, name string) *runtime.Raw {
	t.Helper()
	return &runtime.Raw{
		Type: runtime.NewUnversionedType("test-repo"),
		Data: []byte(fmt.Sprintf(`{"type":"test-repo","name":%q}`, name)),
	}
}

// pathMatcherConfig builds a generic config carrying a single resolvers.config
// entry with the given path matcher resolvers.
func pathMatcherConfig(t *testing.T, rs ...*resolverspec.Resolver) *genericv1.Config {
	t.Helper()
	inner := &resolverspec.Config{
		Type:      runtime.NewVersionedType(resolverspec.ConfigType, resolverspec.Version),
		Resolvers: rs,
	}
	raw := &runtime.Raw{}
	require.NoError(t, resolverspec.Scheme.Convert(inner, raw))
	return &genericv1.Config{
		Type:           runtime.NewVersionedType(genericv1.ConfigType, genericv1.ConfigTypeV1),
		Configurations: []*runtime.Raw{raw},
	}
}

// fallbackConfig builds a generic config carrying a single ocm.config entry
// with the given deprecated fallback resolvers.
func fallbackConfig(t *testing.T, rs ...*ocmv1spec.Resolver) *genericv1.Config {
	t.Helper()
	inner := &ocmv1spec.Config{
		Type:      runtime.NewVersionedType(ocmv1spec.ConfigType, ocmv1spec.Version),
		Resolvers: rs,
	}
	raw := &runtime.Raw{}
	require.NoError(t, ocmv1spec.Scheme.Convert(inner, raw))
	return &genericv1.Config{
		Type:           runtime.NewVersionedType(genericv1.ConfigType, genericv1.ConfigTypeV1),
		Configurations: []*runtime.Raw{raw},
	}
}

func specName(t *testing.T, spec runtime.Typed) string {
	t.Helper()
	raw, ok := spec.(*runtime.Raw)
	require.True(t, ok, "expected *runtime.Raw, got %T", spec)
	var m struct {
		Name string `json:"name"`
	}
	require.NoError(t, json.Unmarshal(raw.Data, &m))
	return m.Name
}

func TestNewFromConfig_NilConfigUsesBaseRepo(t *testing.T) {
	r := require.New(t)

	provider := newRecordingProvider()
	base := rawRepo(t, "base")

	resolver, err := NewFromConfig(t.Context(), nil, runtime.NewScheme(runtime.WithAllowUnknown()),
		Options{RepoProvider: provider}, base)
	r.NoError(err)
	r.NotNil(resolver)

	spec, err := resolver.GetRepositorySpecificationForComponent(t.Context(), "any/component", "1.0.0")
	r.NoError(err)
	r.Equal("base", specName(t, spec))
}

func TestNewFromConfig_ConfigOnlyWithNilBaseRepo(t *testing.T) {
	r := require.New(t)

	provider := newRecordingProvider()
	cfg := pathMatcherConfig(t, &resolverspec.Resolver{
		Repository:           rawRepo(t, "configured"),
		ComponentNamePattern: "example.com/*",
	})

	resolver, err := NewFromConfig(t.Context(), cfg, runtime.NewScheme(runtime.WithAllowUnknown()),
		Options{RepoProvider: provider}, nil)
	r.NoError(err)
	r.NotNil(resolver)

	spec, err := resolver.GetRepositorySpecificationForComponent(t.Context(), "example.com/component", "1.0.0")
	r.NoError(err)
	r.Equal("configured", specName(t, spec))

	// No catch-all without a base repository: an unmatched component fails.
	_, err = resolver.GetRepositorySpecificationForComponent(t.Context(), "other.com/component", "1.0.0")
	r.Error(err)
}

func TestNewFromConfig_RootPatternPrecedence(t *testing.T) {
	r := require.New(t)

	provider := newRecordingProvider()
	base := rawRepo(t, "base")
	// A configured matcher also matches the root pattern; the high-priority
	// root pattern (base repo) must still win.
	cfg := pathMatcherConfig(t, &resolverspec.Resolver{
		Repository:           rawRepo(t, "configured"),
		ComponentNamePattern: "example.com/*",
	})

	resolver, err := NewFromConfig(t.Context(), cfg, runtime.NewScheme(runtime.WithAllowUnknown()),
		Options{RepoProvider: provider, ComponentPatterns: []string{"example.com/root"}}, base)
	r.NoError(err)

	spec, err := resolver.GetRepositorySpecificationForComponent(t.Context(), "example.com/root", "1.0.0")
	r.NoError(err)
	r.Equal("base", specName(t, spec), "root pattern wins over configured matcher")

	// A different component still matches the configured matcher.
	spec, err = resolver.GetRepositorySpecificationForComponent(t.Context(), "example.com/other", "1.0.0")
	r.NoError(err)
	r.Equal("configured", specName(t, spec))
}

func TestNewFromConfig_NilPatternConfiguredMatcherPrecedence(t *testing.T) {
	r := require.New(t)

	provider := newRecordingProvider()
	base := rawRepo(t, "base")
	// No component patterns: a configured matcher must win over the base
	// repository catch-all.
	cfg := pathMatcherConfig(t, &resolverspec.Resolver{
		Repository:           rawRepo(t, "configured"),
		ComponentNamePattern: "example.com/*",
	})

	resolver, err := NewFromConfig(t.Context(), cfg, runtime.NewScheme(runtime.WithAllowUnknown()),
		Options{RepoProvider: provider}, base)
	r.NoError(err)

	spec, err := resolver.GetRepositorySpecificationForComponent(t.Context(), "example.com/component", "1.0.0")
	r.NoError(err)
	r.Equal("configured", specName(t, spec))

	// Unmatched components fall through to the base repository catch-all.
	spec, err = resolver.GetRepositorySpecificationForComponent(t.Context(), "other.com/component", "1.0.0")
	r.NoError(err)
	r.Equal("base", specName(t, spec))
}

func TestNewFromConfig_FallbackSelection(t *testing.T) {
	r := require.New(t)

	provider := newRecordingProvider()
	cfg := fallbackConfig(t, &ocmv1spec.Resolver{
		Repository: rawRepo(t, "fallback"),
		Prefix:     "example.com",
		Priority:   ptr.To(10),
	})

	resolver, err := NewFromConfig(t.Context(), cfg, runtime.NewScheme(runtime.WithAllowUnknown()),
		Options{RepoProvider: provider}, nil)
	r.NoError(err)
	r.NotNil(resolver)

	spec, err := resolver.GetRepositorySpecificationForComponent(t.Context(), "example.com/component", "1.0.0")
	r.NoError(err)
	r.Equal("fallback", specName(t, spec))
}

func TestNewFromConfig_MixedResolverTypesRejected(t *testing.T) {
	r := require.New(t)

	provider := newRecordingProvider()
	// Both resolver types present in one config.
	cfg := pathMatcherConfig(t, &resolverspec.Resolver{
		Repository:           rawRepo(t, "pm"),
		ComponentNamePattern: "example.com/*",
	})
	fb := fallbackConfig(t, &ocmv1spec.Resolver{
		Repository: rawRepo(t, "fb"),
		Prefix:     "example.com",
		Priority:   ptr.To(10),
	})
	cfg.Configurations = append(cfg.Configurations, fb.Configurations...)

	_, err := NewFromConfig(t.Context(), cfg, runtime.NewScheme(runtime.WithAllowUnknown()),
		Options{RepoProvider: provider}, nil)
	r.ErrorContains(err, "only one type is allowed")
}

func TestNewFromConfig_RejectsExplicitPathMatchers(t *testing.T) {
	r := require.New(t)

	_, err := NewFromConfig(t.Context(), nil, runtime.NewScheme(runtime.WithAllowUnknown()),
		Options{
			RepoProvider: newRecordingProvider(),
			PathMatchers: []*resolverspec.Resolver{{ComponentNamePattern: "*"}},
		}, nil)
	r.ErrorContains(err, "config is the sole source of resolver lists")
}

func TestNewFromConfig_RejectsExplicitFallbackResolvers(t *testing.T) {
	r := require.New(t)

	_, err := NewFromConfig(t.Context(), nil, runtime.NewScheme(runtime.WithAllowUnknown()),
		Options{
			RepoProvider:      newRecordingProvider(),
			FallbackResolvers: []*resolverruntime.Resolver{{}},
		}, nil)
	r.ErrorContains(err, "config is the sole source of resolver lists")
}

func TestNewFromConfig_ExtractionError(t *testing.T) {
	r := require.New(t)

	// A resolvers.config entry with malformed JSON surfaces an extraction error.
	bad := &runtime.Raw{
		Type: runtime.NewVersionedType(resolverspec.ConfigType, resolverspec.Version),
		Data: []byte(`{"type":"resolvers.config.ocm.software/v1alpha1","resolvers":"not-a-list"}`),
	}
	cfg := &genericv1.Config{
		Type:           runtime.NewVersionedType(genericv1.ConfigType, genericv1.ConfigTypeV1),
		Configurations: []*runtime.Raw{bad},
	}

	_, err := NewFromConfig(t.Context(), cfg, runtime.NewScheme(runtime.WithAllowUnknown()),
		Options{RepoProvider: newRecordingProvider()}, nil)
	r.ErrorContains(err, "extracting resolvers from configuration failed")
}

func TestNewFromConfig_CredentialsForRoutedRepository(t *testing.T) {
	r := require.New(t)

	provider := newRecordingProvider()
	cred := &runtime.Raw{Type: runtime.NewUnversionedType("test-cred"), Data: []byte(`{"type":"test-cred"}`)}
	base := rawRepo(t, "base")
	cfg := pathMatcherConfig(t, &resolverspec.Resolver{
		Repository:           rawRepo(t, "configured"),
		ComponentNamePattern: "example.com/*",
	})

	resolver, err := NewFromConfig(t.Context(), cfg, runtime.NewScheme(runtime.WithAllowUnknown()),
		Options{RepoProvider: provider, CredentialGraph: staticCredentials{cred: cred}}, base)
	r.NoError(err)

	// Route to the configured repository, not the base repository.
	repo, err := resolver.GetComponentVersionRepositoryForComponent(t.Context(), "example.com/component", "1.0.0")
	r.NoError(err)
	r.NotNil(repo)

	provider.mu.Lock()
	defer provider.mu.Unlock()
	var routed runtime.Typed
	for _, s := range provider.specs {
		if specName(t, s) == "configured" {
			routed = s
		}
	}
	r.NotNil(routed, "configured repository must have been built")
	raw, _ := json.Marshal(routed)
	r.Equal(cred, provider.credsFor[string(raw)], "credentials must be resolved for the routed repository")
}
