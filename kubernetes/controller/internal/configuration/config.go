// Package configuration provides functionality to load and manage OCM configurations
// from Kubernetes resources (Secrets and ConfigMaps).
package configuration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
)

// GetConfigFromSecret extracts and decodes OCM configuration from a Kubernetes Secret.
// It looks for configuration data under the OCMConfigKey.
func GetConfigFromSecret(secret *corev1.Secret) (*genericv1.Config, error) {
	data, ok := secret.Data[v1alpha1.OCMConfigKey]
	if !ok || len(data) == 0 {
		return nil, errors.New("no ocm config found in secret")
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
		return nil, errors.New("no ocm config found in configmap")
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

// Configuration represents the flattened OCM configuration and adds the hash of the configuration data.
type Configuration struct {
	Hash   []byte
	Config *genericv1.Config
}

// LoadConfigurations loads OCM configurations from a list of OCMConfiguration references.
// It fetches the referenced Secrets/ConfigMaps from the cluster and extracts their configuration into a flat map and
// calculates the hash of the configuration data. This is provided along with the configuration data for caching purposes.
// The object fetching happens concurrently, but Spec declaration order is preserved. Meaning, in whatever order the original
// object declared the configuration, that order is preserved.
func LoadConfigurations(ctx context.Context, k8sClient client.Reader, namespace string, ocmConfigs []v1alpha1.OCMConfiguration) (Configuration, error) {
	if len(ocmConfigs) == 0 {
		return Configuration{}, nil
	}

	objects, err := getConfigurationObjects(ctx, k8sClient, ocmConfigs, namespace)
	if err != nil {
		return Configuration{}, err
	}

	var configs []*genericv1.Config
	for _, obj := range objects {
		cfg, err := GetConfigFromObject(obj)
		if err != nil {
			return Configuration{}, err
		}

		if cfg == nil {
			continue
		}

		configs = append(configs, cfg)
	}

	flattened := genericv1.FlatMap(configs...)
	content, err := json.Marshal(flattened)
	if err != nil {
		return Configuration{}, err
	}

	hasher := sha256.New()
	hasher.Write(content)
	hash := hasher.Sum(nil)

	result := Configuration{
		Config: flattened,
		Hash:   hash,
	}

	return result, nil
}

// gatherConfigurationObjects fetches the referenced Secrets/ConfigMaps from the cluster. It does so concurrently and by
// preserving the order of the input list. The order of the input list is defined by the Spec defining the configuration
// references.
func getConfigurationObjects(ctx context.Context, k8sClient client.Reader, ocmConfigs []v1alpha1.OCMConfiguration, namespace string) ([]client.Object, error) {
	// pre-allocate the output array to the same length as the input list for concurrent operations
	objects := make([]client.Object, len(ocmConfigs))
	fetchGroup, ctx := errgroup.WithContext(ctx)
	for i, ocmConfig := range ocmConfigs {
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

		fetchGroup.Go(func() error {
			key := client.ObjectKey{Namespace: ns, Name: ocmConfig.Name}
			if err := k8sClient.Get(ctx, key, obj); err != nil {
				return fmt.Errorf("failed to get %s %s/%s: %w", ocmConfig.Kind, ns, ocmConfig.Name, err)
			}

			// set the exact location in which this object was found in the input list
			// this operation is concurrent-safe since it's using a specific index.
			objects[i] = obj

			return nil
		})
	}

	if err := fetchGroup.Wait(); err != nil {
		return nil, err
	}

	return objects, nil
}
