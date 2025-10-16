package configuration

import (
	"context"
	"testing"

	"github.com/fluxcd/pkg/apis/meta"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
)

func TestGetConfigFromSecret(t *testing.T) {
	tests := []struct {
		name    string
		secret  *corev1.Secret
		wantNil bool
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
			wantNil: false,
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
			wantNil: true,
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
			wantNil: true,
			wantErr: true,
		},
		{
			name: "invalid json",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					v1alpha1.OCMConfigKey: []byte(`invalid json`),
				},
			},
			wantNil: false,
			wantErr: true,
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
			if tt.wantNil {
				assert.Nil(t, cfg)
			} else if !tt.wantErr {
				assert.NotNil(t, cfg)
			}
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
						"type": "filesystem.config.ocm.software/v1alpha1",
						"tempFolder": "/tmp/test"
					},
					{
						"type": "whatever.config.ocm.software/v1alpha1",
						"whatever": "whatever"
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
					NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
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
					NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
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
					NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
						Kind: "Secret",
						Name: "test-secret",
					},
				},
				{
					NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
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
					NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
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

func TestLoadConfigurationsInOrder(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, v1alpha1.AddToScheme(scheme))

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
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							Kind: "Secret",
							Name: "test-secret-a",
						},
					},
				},
				{
					{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							Kind: "Secret",
							Name: "test-secret-b",
						},
					},
				},
			},
			secrets: []*corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret-a",
						Namespace: "default",
					},
					Data: map[string][]byte{
						v1alpha1.OCMConfigKey: []byte(`{
							"type": "generic.config.ocm.software/v1",
							"configurations": [
								{
									"type": "filesystem.config.ocm.software/v1alpha1",
									"tempFolder": "/tmp/test"
								},
								{
									"type": "whatever.config.ocm.software/v1alpha1",
									"whatever": "whatever"
								}
						]}`),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret-b",
						Namespace: "default",
					},
					Data: map[string][]byte{
						v1alpha1.OCMConfigKey: []byte(`{
							"type": "generic.config.ocm.software/v1",
							"configurations": [
								{
									"type": "whatever.config.ocm.software/v1alpha1",
									"whatever": "whatever"
								},
								{
									"type": "filesystem.config.ocm.software/v1alpha1",
									"tempFolder": "/tmp/test"
								}
						]}`),
					},
				},
			},
			errorCheck: require.NoError,
			equal:      require.NotEqual,
		},
		{
			name:      "same order should produce the same result always",
			namespace: "default",
			ocmConfigs: [][]v1alpha1.OCMConfiguration{
				{
					{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							Kind: "Secret",
							Name: "test-secret-a",
						},
					},
					{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							Kind: "Secret",
							Name: "test-secret-b",
						},
					},
				},
				{
					{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							Kind: "Secret",
							Name: "test-secret-a",
						},
					},
					{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							Kind: "Secret",
							Name: "test-secret-b",
						},
					},
				},
			},
			secrets: []*corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret-a",
						Namespace: "default",
					},
					Data: map[string][]byte{
						v1alpha1.OCMConfigKey: []byte(`{
							"type": "generic.config.ocm.software/v1",
							"configurations": [
								{
									"type": "filesystem.config.ocm.software/v1alpha1",
									"tempFolder": "/tmp/test"
								},
								{
									"type": "whatever.config.ocm.software/v1alpha1",
									"whatever": "whatever"
								}
						]}`),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret-b",
						Namespace: "default",
					},
					Data: map[string][]byte{
						v1alpha1.OCMConfigKey: []byte(`{
							"type": "generic.config.ocm.software/v1",
							"configurations": [
								{
									"type": "whatever.config.ocm.software/v1alpha1",
									"whatever": "whatever"
								},
								{
									"type": "filesystem.config.ocm.software/v1alpha1",
									"tempFolder": "/tmp/test"
								}
						]}`),
					},
				},
			},
			errorCheck: require.NoError,
			equal:      require.Equal,
		},
		{
			name:      "order of declared configs should matter",
			namespace: "default",
			ocmConfigs: [][]v1alpha1.OCMConfiguration{
				{
					{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							Kind: "Secret",
							Name: "test-secret-a",
						},
					},
					{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							Kind: "Secret",
							Name: "test-secret-b",
						},
					},
				},
				{
					{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							Kind: "Secret",
							Name: "test-secret-b",
						},
					},
					{
						NamespacedObjectKindReference: meta.NamespacedObjectKindReference{
							Kind: "Secret",
							Name: "test-secret-a",
						},
					},
				},
			},
			secrets: []*corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret-a",
						Namespace: "default",
					},
					Data: map[string][]byte{
						v1alpha1.OCMConfigKey: []byte(`{
				"type": "generic.config.ocm.software/v1",
				"configurations": [
					{
						"type": "filesystem.config.ocm.software/v1alpha1",
						"tempFolder": "/tmp/test"
					},
					{
						"type": "whatever.config.ocm.software/v1alpha1",
						"whatever": "whatever"
					}
				]
			}`),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret-b",
						Namespace: "default",
					},
					Data: map[string][]byte{
						v1alpha1.OCMConfigKey: []byte(`{
				"type": "generic.config.ocm.software/v1",
				"configurations": [
					{
						"type": "whatever.config.ocm.software/v1alpha1",
						"whatever": "whatever"
					},
					{
						"type": "filesystem.config.ocm.software/v1alpha1",
						"tempFolder": "/tmp/test"
					}
				]
			}`),
					},
				},
			},

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
