package configuration

import (
	"context"
	"strings"
	"testing"

	"github.com/go-logr/logr/funcr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	ocmconfigv1spec "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/spec"
	resolversv1alpha1spec "ocm.software/open-component-model/bindings/go/configuration/resolvers/v1alpha1/spec"
	credentialsv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
)

func TestGetConfigFromSecret(t *testing.T) {
	tests := []struct {
		name    string
		secret  *corev1.Secret
		want    *genericv1.Config
		wantErr bool
	}{
		{
			name: "valid ocm config in secret",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					v1alpha1.OCMConfigKey: []byte(`{
						"type": "generic.config.ocm.software/v1",
						"configurations": []
					}`),
				},
			},
			want: &genericv1.Config{
				Type:           ocmruntime.Type{Version: genericv1.Version, Name: genericv1.ConfigType},
				Configurations: []*ocmruntime.Raw{},
			},
			wantErr: false,
		},
		{
			name: "no ocm config key",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{},
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "empty ocm config",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					v1alpha1.OCMConfigKey: {},
				},
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "invalid json ocm config",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					v1alpha1.OCMConfigKey: []byte(`invalid json`),
				},
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "valid docker config returns generic config",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					corev1.DockerConfigJsonKey: []byte(`{"auths": {"my-registry.io": {"username":"user","password":"pass","email":""}}}`),
				},
			},
			want: &genericv1.Config{
				Type: ocmruntime.Type{Version: genericv1.Version, Name: genericv1.ConfigType},
				Configurations: []*ocmruntime.Raw{
					{
						Type: ocmruntime.Type{Version: "", Name: credentialsv1.ConfigType},
						Data: []byte(`{"repositories":[{"repository":{"dockerConfig":"{\"auths\": {\"my-registry.io\": {\"username\":\"user\",\"password\":\"pass\",\"email\":\"\"}}}","type":"DockerConfig/v1"}}],"type":"credentials.config.ocm.software"}`),
					},
				},
			},
			wantErr: false,
		},
		{
			name: "empty docker config returns error",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					corev1.DockerConfigJsonKey: {},
				},
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "invalid docker config returns error",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					corev1.DockerConfigJsonKey: []byte(`helloworld`),
				},
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "docker config with multiple registries",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					corev1.DockerConfigJsonKey: []byte(`{"auths":{"registry1.io":{"username": "user1", "password": "pass1"},"registry2.io":{"username":"user2","password":"pass2"}}}`),
				},
			},
			want: &genericv1.Config{
				Type: ocmruntime.Type{Version: genericv1.Version, Name: genericv1.ConfigType},
				Configurations: []*ocmruntime.Raw{
					{
						Type: ocmruntime.Type{Version: "", Name: credentialsv1.ConfigType},
						Data: []byte(`{"repositories":[{"repository":{"dockerConfig":"{\"auths\":{\"registry1.io\":{\"username\": \"user1\", \"password\": \"pass1\"},\"registry2.io\":{\"username\":\"user2\",\"password\":\"pass2\"}}}","type":"DockerConfig/v1"}}],"type":"credentials.config.ocm.software"}`),
					},
				},
			},
			wantErr: false,
		},
		{
			name: "docker config with auth token",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					corev1.DockerConfigJsonKey: []byte(`{"auths":{"my-registry.io":{"auth": "dXNlcjpwYXNz"}}}`),
				},
			},
			want: &genericv1.Config{
				Type: ocmruntime.Type{Version: genericv1.Version, Name: genericv1.ConfigType},
				Configurations: []*ocmruntime.Raw{
					{
						Type: ocmruntime.Type{Version: "", Name: credentialsv1.ConfigType},
						Data: []byte(`{"repositories":[{"repository":{"dockerConfig":"{\"auths\":{\"my-registry.io\":{\"auth\": \"dXNlcjpwYXNz\"}}}","type":"DockerConfig/v1"}}],"type":"credentials.config.ocm.software"}`),
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := GetConfigFromSecret(tt.secret)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.want, cfg)
		})
	}
}

func TestGetConfigFromConfigMap(t *testing.T) {
	tests := []struct {
		name      string
		configMap *corev1.ConfigMap
		wantNil   bool
		wantErr   bool
	}{
		{
			name: "valid ocm config in configmap",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cm",
					Namespace: "default",
				},
				Data: map[string]string{
					v1alpha1.OCMConfigKey: `{
						"type": "generic.config.ocm.software/v1",
						"configurations": []
					}`,
				},
			},
			wantNil: false,
			wantErr: false,
		},
		{
			name: "no ocm config key",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cm",
					Namespace: "default",
				},
				Data: map[string]string{},
			},
			wantNil: true,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := GetConfigFromConfigMap(tt.configMap)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			if tt.wantNil {
				assert.Nil(t, cfg)
			} else if !tt.wantErr {
				assert.NotNil(t, cfg)
			}
		})
	}
}

func TestLoadConfigurations(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, v1alpha1.AddToScheme(scheme))

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			v1alpha1.OCMConfigKey: []byte(`{
				"type": "generic.config.ocm.software/v1",
				"configurations": [
					{
						"type": "credentials.config.ocm.software/v1",
						"repositories": []
					},
					{
						"type": "resolvers.config.ocm.software/v1alpha1",
						"resolvers": []
					}
				]
			}`),
		},
	}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cm",
			Namespace: "default",
		},
		Data: map[string]string{
			v1alpha1.OCMConfigKey: `{
				"type": "generic.config.ocm.software/v1",
				"configurations": []
			}`,
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(secret, configMap).
		Build()

	tests := []struct {
		name        string
		namespace   string
		ocmConfigs  []v1alpha1.OCMConfiguration
		wantErr     bool
		checkResult func(t *testing.T, cfg *genericv1.Config)
	}{
		{
			name:      "load from secret",
			namespace: "default",
			ocmConfigs: []v1alpha1.OCMConfiguration{
				{
					NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
						Kind: "Secret",
						Name: "test-secret",
					},
				},
			},
			wantErr: false,
			checkResult: func(t *testing.T, cfg *genericv1.Config) {
				assert.NotNil(t, cfg)
				assert.Len(t, cfg.Configurations, 2)
			},
		},
		{
			name:      "load from configmap",
			namespace: "default",
			ocmConfigs: []v1alpha1.OCMConfiguration{
				{
					NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
						Kind: "ConfigMap",
						Name: "test-cm",
					},
				},
			},
			wantErr: false,
			checkResult: func(t *testing.T, cfg *genericv1.Config) {
				assert.NotNil(t, cfg)
			},
		},
		{
			name:      "load from both",
			namespace: "default",
			ocmConfigs: []v1alpha1.OCMConfiguration{
				{
					NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
						Kind: "Secret",
						Name: "test-secret",
					},
				},
				{
					NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
						Kind: "ConfigMap",
						Name: "test-cm",
					},
				},
			},
			wantErr: false,
			checkResult: func(t *testing.T, cfg *genericv1.Config) {
				assert.NotNil(t, cfg)
				// FlatMap merges configurations
				assert.Len(t, cfg.Configurations, 2)
			},
		},
		{
			name:      "non-existent secret",
			namespace: "default",
			ocmConfigs: []v1alpha1.OCMConfiguration{
				{
					NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
						Kind: "Secret",
						Name: "non-existent",
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := LoadConfigurations(context.Background(), client, tt.namespace, tt.ocmConfigs)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.checkResult != nil {
					tt.checkResult(t, cfg.Config)
				}
			}
		})
	}
}

// credentialsConfigJSON returns a generic OCM config JSON with credentials
// entries for the given hostnames, in the order provided.
func credentialsConfigJSON(hostnames ...string) []byte {
	consumers := make([]string, 0, len(hostnames))
	for _, h := range hostnames {
		consumers = append(consumers, `{"type":"credentials.config.ocm.software/v1","consumers":[{"identities":[{"hostname":"`+h+`"}],"credentials":[]}]}`)
	}
	return []byte(`{"type":"generic.config.ocm.software/v1","configurations":[` + strings.Join(consumers, ",") + `]}`)
}

func TestLoadConfigurationsInOrder(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, v1alpha1.AddToScheme(scheme))

	secretA := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "test-secret-a", Namespace: "default"},
		Data:       map[string][]byte{v1alpha1.OCMConfigKey: credentialsConfigJSON("registry-a.io", "registry-b.io")},
	}
	secretB := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "test-secret-b", Namespace: "default"},
		Data:       map[string][]byte{v1alpha1.OCMConfigKey: credentialsConfigJSON("registry-b.io", "registry-a.io")},
	}

	tests := []struct {
		name       string
		namespace  string
		secrets    []*corev1.Secret
		ocmConfigs [][]v1alpha1.OCMConfiguration
		wantErr    bool
		errorCheck require.ErrorAssertionFunc
		equal      require.ComparisonAssertionFunc
	}{
		{
			name:      "declared config order shouldn't produce the same result",
			namespace: "default",
			ocmConfigs: [][]v1alpha1.OCMConfiguration{
				{
					{
						NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
							Kind: "Secret",
							Name: "test-secret-a",
						},
					},
				},
				{
					{
						NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
							Kind: "Secret",
							Name: "test-secret-b",
						},
					},
				},
			},
			secrets:    []*corev1.Secret{secretA, secretB},
			errorCheck: require.NoError,
			equal:      require.NotEqual,
		},
		{
			name:      "same order should produce the same result always",
			namespace: "default",
			ocmConfigs: [][]v1alpha1.OCMConfiguration{
				{
					{
						NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
							Kind: "Secret",
							Name: "test-secret-a",
						},
					},
					{
						NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
							Kind: "Secret",
							Name: "test-secret-b",
						},
					},
				},
				{
					{
						NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
							Kind: "Secret",
							Name: "test-secret-a",
						},
					},
					{
						NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
							Kind: "Secret",
							Name: "test-secret-b",
						},
					},
				},
			},
			secrets:    []*corev1.Secret{secretA, secretB},
			errorCheck: require.NoError,
			equal:      require.Equal,
		},
		{
			name:      "order of declared configs should matter",
			namespace: "default",
			ocmConfigs: [][]v1alpha1.OCMConfiguration{
				{
					{
						NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
							Kind: "Secret",
							Name: "test-secret-a",
						},
					},
					{
						NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
							Kind: "Secret",
							Name: "test-secret-b",
						},
					},
				},
				{
					{
						NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
							Kind: "Secret",
							Name: "test-secret-b",
						},
					},
					{
						NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
							Kind: "Secret",
							Name: "test-secret-a",
						},
					},
				},
			},
			secrets:    []*corev1.Secret{secretA, secretB},
			errorCheck: require.NoError,
			equal:      require.NotEqual,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.secrets[0], tt.secrets[1]).
				Build()

			cfgA, err := LoadConfigurations(context.Background(), client, tt.namespace, tt.ocmConfigs[0])
			require.NoError(t, err)

			cfgB, err := LoadConfigurations(context.Background(), client, tt.namespace, tt.ocmConfigs[1])
			tt.errorCheck(t, err)
			tt.equal(t, cfgA, cfgB)
		})
	}
}

func TestFilterAllowedConfigTypes(t *testing.T) {
	makeGenericConfig := func(entries ...string) *genericv1.Config {
		cfg := &genericv1.Config{
			Type: ocmruntime.Type{Version: genericv1.Version, Name: genericv1.ConfigType},
		}
		for _, entry := range entries {
			raw := &ocmruntime.Raw{}
			require.NoError(t, raw.UnmarshalJSON([]byte(entry)))
			cfg.Configurations = append(cfg.Configurations, raw)
		}
		return cfg
	}

	t.Run("nil config is handled", func(t *testing.T) {
		result, err := filterAllowedConfigTypes(t.Context(), nil)
		require.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("empty config returns empty configurations", func(t *testing.T) {
		cfg := &genericv1.Config{
			Type: ocmruntime.Type{Version: genericv1.Version, Name: genericv1.ConfigType},
		}
		result, err := filterAllowedConfigTypes(t.Context(), cfg)
		require.NoError(t, err)
		assert.Empty(t, result.Configurations)
	})

	t.Run("only allowed types pass through", func(t *testing.T) {
		cfg := makeGenericConfig(
			`{"type":"credentials.config.ocm.software/v1","repositories":[]}`,
			`{"type":"resolvers.config.ocm.software/v1alpha1","resolvers":[]}`,
			`{"type":"ocm.config.ocm.software/v1","resolvers":[]}`,
		)
		result, err := filterAllowedConfigTypes(t.Context(), cfg)
		require.NoError(t, err)
		require.Len(t, result.Configurations, 3)
		types := []ocmruntime.Type{result.Configurations[0].GetType(), result.Configurations[1].GetType(), result.Configurations[2].GetType()}
		assert.Contains(t, types, ocmruntime.NewVersionedType(credentialsv1.ConfigType, credentialsv1.Version))
		assert.Contains(t, types, ocmruntime.NewVersionedType(resolversv1alpha1spec.ConfigType, resolversv1alpha1spec.Version))
		assert.Contains(t, types, ocmruntime.NewVersionedType(ocmconfigv1spec.ConfigType, ocmconfigv1spec.Version))
	})

	t.Run("disallowed types are removed", func(t *testing.T) {
		cfg := makeGenericConfig(
			`{"type":"credentials.config.ocm.software/v1","repositories":[]}`,
			`{"type":"filesystem.config.ocm.software/v1alpha1","tempFolder":"/tmp"}`,
			`{"type":"whatever.config.ocm.software/v1alpha1","whatever":"whatever"}`,
		)
		result, err := filterAllowedConfigTypes(t.Context(), cfg)
		require.NoError(t, err)
		require.Len(t, result.Configurations, 1)
		assert.Equal(t,
			ocmruntime.NewVersionedType(credentialsv1.ConfigType, credentialsv1.Version),
			result.Configurations[0].GetType(),
		)
	})

	t.Run("unversioned allowed types pass through", func(t *testing.T) {
		cfg := makeGenericConfig(
			`{"type":"credentials.config.ocm.software","repositories":[]}`,
			`{"type":"resolvers.config.ocm.software","resolvers":[]}`,
		)
		result, err := filterAllowedConfigTypes(t.Context(), cfg)
		require.NoError(t, err)
		require.Len(t, result.Configurations, 2)
		types := []ocmruntime.Type{result.Configurations[0].GetType(), result.Configurations[1].GetType()}
		assert.Contains(t, types, ocmruntime.NewUnversionedType(credentialsv1.ConfigType))
		assert.Contains(t, types, ocmruntime.NewUnversionedType(resolversv1alpha1spec.ConfigType))
	})

	t.Run("aliases stripped from ocm.config.ocm.software versioned", func(t *testing.T) {
		cfg := makeGenericConfig(
			`{"type":"ocm.config.ocm.software/v1","aliases":{"myrepo":{"type":"OCIRegistry","baseUrl":"ghcr.io"}},"resolvers":[]}`,
		)
		result, err := filterAllowedConfigTypes(t.Context(), cfg)
		require.NoError(t, err)
		require.Len(t, result.Configurations, 1)

		var ocmCfg ocmconfigv1spec.Config
		require.NoError(t, ocmconfigv1spec.Scheme.Convert(result.Configurations[0], &ocmCfg))
		assert.Nil(t, ocmCfg.Aliases)
	})

	t.Run("aliases stripped from ocm.config.ocm.software unversioned", func(t *testing.T) {
		cfg := makeGenericConfig(
			`{"type":"ocm.config.ocm.software","aliases":{"myrepo":{"type":"OCIRegistry","baseUrl":"ghcr.io"}},"resolvers":[]}`,
		)
		result, err := filterAllowedConfigTypes(t.Context(), cfg)
		require.NoError(t, err)
		require.Len(t, result.Configurations, 1)

		var ocmCfg ocmconfigv1spec.Config
		require.NoError(t, ocmconfigv1spec.Scheme.Convert(result.Configurations[0], &ocmCfg))
		assert.Nil(t, ocmCfg.Aliases)
	})

	t.Run("resolvers preserved after alias stripping", func(t *testing.T) {
		cfg := makeGenericConfig(
			`{"type":"ocm.config.ocm.software/v1","aliases":{"myrepo":{"type":"OCIRegistry","baseUrl":"ghcr.io"}},"resolvers":[{"repository":{"type":"OCIRegistry","baseUrl":"ghcr.io"},"prefix":"ocm.software","priority":10}]}`,
		)
		result, err := filterAllowedConfigTypes(t.Context(), cfg)
		require.NoError(t, err)
		require.Len(t, result.Configurations, 1)

		var ocmCfg ocmconfigv1spec.Config
		require.NoError(t, ocmconfigv1spec.Scheme.Convert(result.Configurations[0], &ocmCfg))
		assert.Nil(t, ocmCfg.Aliases)
		require.Len(t, ocmCfg.Resolvers, 1)
		assert.Equal(t, "ocm.software", ocmCfg.Resolvers[0].Prefix)
	})

	t.Run("resolvers v1alpha1 passes through unchanged", func(t *testing.T) {
		cfg := makeGenericConfig(
			`{"type":"resolvers.config.ocm.software/v1alpha1","resolvers":[{"repository":{"type":"OCIRegistry","baseUrl":"ghcr.io"},"componentNamePattern":"ocm.software/*"}]}`,
		)
		result, err := filterAllowedConfigTypes(t.Context(), cfg)
		require.NoError(t, err)
		require.Len(t, result.Configurations, 1)

		var resolverCfg resolversv1alpha1spec.Config
		require.NoError(t, resolversv1alpha1spec.Scheme.Convert(result.Configurations[0], &resolverCfg))
		require.Len(t, resolverCfg.Resolvers, 1)
		assert.Equal(t, "ocm.software/*", resolverCfg.Resolvers[0].ComponentNamePattern)
	})

	t.Run("allowed entries from multiple configs are all preserved after FlatMap", func(t *testing.T) {
		cfgA := makeGenericConfig(
			`{"type":"credentials.config.ocm.software/v1","repositories":[]}`,
			`{"type":"filesystem.config.ocm.software/v1alpha1","tempFolder":"/tmp"}`,
		)
		cfgB := makeGenericConfig(
			`{"type":"resolvers.config.ocm.software/v1alpha1","resolvers":[]}`,
			`{"type":"whatever.config.ocm.software/v1alpha1","whatever":"whatever"}`,
		)
		flattened := genericv1.FlatMap(cfgA, cfgB)
		result, err := filterAllowedConfigTypes(t.Context(), flattened)
		require.NoError(t, err)
		// only the credentials and resolvers entries survive; the filesystem and whatever entries are dropped
		require.Len(t, result.Configurations, 2)
		types := []ocmruntime.Type{result.Configurations[0].GetType(), result.Configurations[1].GetType()}
		assert.Contains(t, types, ocmruntime.NewVersionedType(credentialsv1.ConfigType, credentialsv1.Version))
		assert.Contains(t, types, ocmruntime.NewVersionedType(resolversv1alpha1spec.ConfigType, resolversv1alpha1spec.Version))
	})

	t.Run("dropped types are logged at V(1)", func(t *testing.T) {
		cfg := makeGenericConfig(
			`{"type":"credentials.config.ocm.software/v1","repositories":[]}`,
			`{"type":"filesystem.config.ocm.software/v1alpha1","tempFolder":"/tmp"}`,
			`{"type":"whatever.config.ocm.software/v1alpha1","whatever":"whatever"}`,
		)
		var logLines []string
		logger := funcr.New(func(_, args string) {
			logLines = append(logLines, args)
		}, funcr.Options{Verbosity: 1})
		ctx := log.IntoContext(t.Context(), logger)
		result, err := filterAllowedConfigTypes(ctx, cfg)
		require.NoError(t, err)
		require.Len(t, result.Configurations, 1)
		require.Len(t, logLines, 1)
		assert.Contains(t, logLines[0], "dropping config entries with types not in allowlist")
		assert.Contains(t, logLines[0], "filesystem.config.ocm.software/v1alpha1")
		assert.Contains(t, logLines[0], "whatever.config.ocm.software/v1alpha1")
	})
}
