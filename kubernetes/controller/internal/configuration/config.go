// Package configuration provides functionality to load and manage OCM configurations
// from Kubernetes resources (Secrets and ConfigMaps).
package configuration

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
)

// GetConfigFromSecret extracts and decodes OCM configuration from a Kubernetes Secret.
// It looks for configuration data under the OCMConfigKey.
func GetConfigFromSecret(secret *corev1.Secret) (*genericv1.Config, error) {
	data, ok := secret.Data[v1alpha1.OCMConfigKey]
	if !ok || len(data) == 0 {
		return nil, nil
	}

	var cfg genericv1.Config
	if err := genericv1.Scheme.Decode(bytes.NewReader(data), &cfg); err != nil {
		return nil, fmt.Errorf("failed to decode ocm config from secret %s/%s: %w",
			secret.Namespace, secret.Name, err)
	}

	return &cfg, nil
}

// GetConfigFromConfigMap extracts and decodes OCM configuration from a Kubernetes ConfigMap.
// It looks for configuration data under the OCMConfigKey.
func GetConfigFromConfigMap(configMap *corev1.ConfigMap) (*genericv1.Config, error) {
	data, ok := configMap.Data[v1alpha1.OCMConfigKey]
	if !ok || len(data) == 0 {
		return nil, nil
	}

	var cfg genericv1.Config
	if err := genericv1.Scheme.Decode(strings.NewReader(data), &cfg); err != nil {
		return nil, fmt.Errorf("failed to decode ocm config from configmap %s/%s: %w",
			configMap.Namespace, configMap.Name, err)
	}

	return &cfg, nil
}

// GetConfigFromObject extracts configuration from either a Secret or ConfigMap.
func GetConfigFromObject(obj client.Object) (*genericv1.Config, error) {
	switch o := obj.(type) {
	case *corev1.Secret:
		return GetConfigFromSecret(o)
	case *corev1.ConfigMap:
		return GetConfigFromConfigMap(o)
	default:
		return nil, fmt.Errorf("unsupported configuration object type: %T", obj)
	}
}

// LoadConfigurations loads OCM configurations from a list of OCMConfiguration references.
// It fetches the referenced Secrets/ConfigMaps from the cluster and extracts their configuration.
// All configurations are merged using FlatMap into a single Config.
func LoadConfigurations(ctx context.Context, k8sClient client.Reader, namespace string, ocmConfigs []v1alpha1.OCMConfiguration) (*genericv1.Config, error) {
	if len(ocmConfigs) == 0 {
		return &genericv1.Config{}, nil
	}

	configs := make([]*genericv1.Config, 0, len(ocmConfigs))

	// TODO: This needs to make sure that the config is ordered and resolved in the SAME WAY.
	for _, ocmConfig := range ocmConfigs {
		ns := ocmConfig.Namespace
		if ns == "" {
			ns = namespace
		}

		var obj client.Object
		switch ocmConfig.Kind {
		case "Secret":
			obj = &corev1.Secret{}
		case "ConfigMap":
			obj = &corev1.ConfigMap{}
		default:
			return nil, fmt.Errorf("unsupported configuration kind: %s", ocmConfig.Kind)
		}

		key := client.ObjectKey{Namespace: ns, Name: ocmConfig.Name}
		if err := k8sClient.Get(ctx, key, obj); err != nil {
			return nil, fmt.Errorf("failed to get %s %s/%s: %w", ocmConfig.Kind, ns, ocmConfig.Name, err)
		}

		cfg, err := GetConfigFromObject(obj)
		if err != nil {
			return nil, err
		}

		// It's possible that config is nil if the secret doesn't have the required key or there is no data.
		if cfg != nil {
			configs = append(configs, cfg)
		}
	}

	return genericv1.FlatMap(configs...), nil
}

// FilterForType is a convenience wrapper around genericv1.FilterForType
// that filters configurations by type T.
// Note: This isn't really used at the moment.
func FilterForType[T runtime.Typed](scheme *runtime.Scheme, config *genericv1.Config) ([]T, error) {
	return genericv1.FilterForType[T](scheme, config)
}
