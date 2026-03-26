package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	resolverv1 "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// testRepo is a minimal typed repository used to exercise scheme-based conversion.
type testRepo struct {
	Type    runtime.Type `json:"type"`
	BaseURL string       `json:"baseURL"`
}

func (r *testRepo) GetType() runtime.Type        { return r.Type }
func (r *testRepo) SetType(t runtime.Type)       { r.Type = t }
func (r *testRepo) DeepCopyTyped() runtime.Typed { ; return new(*r) }

var testScheme = func() *runtime.Scheme {
	s := runtime.NewScheme()
	s.MustRegisterWithAlias(&testRepo{}, runtime.NewVersionedType("test-repo", "v1"))
	return s
}()

func ptr[T any](v T) *T { return &v }

func rawTestRepo(baseURL string) *runtime.Raw {
	return &runtime.Raw{
		Type: runtime.NewVersionedType("test-repo", "v1"),
		Data: []byte(`{"type":"test-repo/v1","baseURL":"` + baseURL + `"}`),
	}
}

func TestConvertFromV1_NilReturnsNil(t *testing.T) {
	got, err := ConvertFromV1(testScheme, nil)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestConvertFromV1_NoResolversNoAliases(t *testing.T) {
	input := &resolverv1.Config{
		Type: runtime.NewVersionedType(resolverv1.ConfigType, resolverv1.Version),
	}
	got, err := ConvertFromV1(testScheme, input)
	require.NoError(t, err)
	assert.Equal(t, &Config{
		Type:      input.Type,
		Resolvers: nil,
	}, got)
}

func TestConvertFromV1_AliasesAreDropped(t *testing.T) {
	input := &resolverv1.Config{
		Type: runtime.NewVersionedType(resolverv1.ConfigType, resolverv1.Version),
		Aliases: map[string]*runtime.Raw{
			"my-alias": rawTestRepo("registry.example.com"),
		},
	}
	got, err := ConvertFromV1(testScheme, input)
	require.NoError(t, err)
	assert.Nil(t, got.Resolvers, "aliases should be silently dropped")
}

func TestConvertFromV1_ExplicitPriority(t *testing.T) {
	input := &resolverv1.Config{
		Type: runtime.NewVersionedType(resolverv1.ConfigType, resolverv1.Version),
		Resolvers: []*resolverv1.Resolver{
			{Repository: rawTestRepo("registry.example.com"), Priority: new(42)},
		},
	}
	got, err := ConvertFromV1(testScheme, input)
	require.NoError(t, err)
	assert.Equal(t, &Config{
		Type: input.Type,
		Resolvers: []Resolver{
			{
				Repository: &testRepo{Type: runtime.NewVersionedType("test-repo", "v1"), BaseURL: "registry.example.com"},
				Priority:   42,
			},
		},
	}, got)
}

func TestConvertFromV1_NilPriorityDefaultsToDefaultPriority(t *testing.T) {
	input := &resolverv1.Config{
		Type: runtime.NewVersionedType(resolverv1.ConfigType, resolverv1.Version),
		Resolvers: []*resolverv1.Resolver{
			{Repository: rawTestRepo("registry.example.com"), Priority: nil},
		},
	}
	got, err := ConvertFromV1(testScheme, input)
	require.NoError(t, err)
	assert.Equal(t, resolverv1.DefaultLookupPriority, got.Resolvers[0].Priority)
}

func TestConvertFromV1_PrefixIsPreserved(t *testing.T) {
	input := &resolverv1.Config{
		Type: runtime.NewVersionedType(resolverv1.ConfigType, resolverv1.Version),
		Resolvers: []*resolverv1.Resolver{
			{Repository: rawTestRepo("registry.example.com"), Prefix: "ocm.software/"},
		},
	}
	got, err := ConvertFromV1(testScheme, input)
	require.NoError(t, err)
	assert.Equal(t, "ocm.software/", got.Resolvers[0].Prefix)
}

func TestConvertFromV1_MultipleResolversOrderPreserved(t *testing.T) {
	input := &resolverv1.Config{
		Type: runtime.NewVersionedType(resolverv1.ConfigType, resolverv1.Version),
		Resolvers: []*resolverv1.Resolver{
			{Repository: rawTestRepo("first.example.com"), Priority: new(100)},
			{Repository: rawTestRepo("second.example.com"), Priority: new(10)},
		},
	}
	got, err := ConvertFromV1(testScheme, input)
	require.NoError(t, err)
	require.Len(t, got.Resolvers, 2)
	assert.Equal(t, &testRepo{Type: runtime.NewVersionedType("test-repo", "v1"), BaseURL: "first.example.com"}, got.Resolvers[0].Repository)
	assert.Equal(t, &testRepo{Type: runtime.NewVersionedType("test-repo", "v1"), BaseURL: "second.example.com"}, got.Resolvers[1].Repository)
}

func TestConvertFromV1_NilResolverElementReturnsError(t *testing.T) {
	input := &resolverv1.Config{
		Type:      runtime.NewVersionedType(resolverv1.ConfigType, resolverv1.Version),
		Resolvers: []*resolverv1.Resolver{nil},
	}
	_, err := ConvertFromV1(testScheme, input)
	assert.Error(t, err)
}

func TestConvertFromV1_NilRepositoryReturnsError(t *testing.T) {
	input := &resolverv1.Config{
		Type: runtime.NewVersionedType(resolverv1.ConfigType, resolverv1.Version),
		Resolvers: []*resolverv1.Resolver{
			{Repository: nil},
		},
	}
	_, err := ConvertFromV1(testScheme, input)
	assert.Error(t, err)
}

func TestConvertFromV1_UnknownRepositoryTypeReturnsError(t *testing.T) {
	input := &resolverv1.Config{
		Type: runtime.NewVersionedType(resolverv1.ConfigType, resolverv1.Version),
		Resolvers: []*resolverv1.Resolver{
			{
				Repository: &runtime.Raw{
					Type: runtime.NewVersionedType("unknown-repo", "v1"),
					Data: []byte(`{"type":"unknown-repo/v1"}`),
				},
			},
		},
	}
	_, err := ConvertFromV1(testScheme, input)
	assert.Error(t, err)
}
