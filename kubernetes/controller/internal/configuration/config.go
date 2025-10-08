// Package configuration provides functionality to load and manage OCM configurations
// from Kubernetes resources (Secrets and ConfigMaps).
package configuration

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/ocm"
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

	objects := make([]client.Object, 0, len(ocmConfigs))
	fetchGroup, ctx := errgroup.WithContext(ctx)
	appendMutex := &sync.Mutex{}
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

		fetchGroup.Go(func() error {
			key := client.ObjectKey{Namespace: ns, Name: ocmConfig.Name}
			if err := k8sClient.Get(ctx, key, obj); err != nil {
				return fmt.Errorf("failed to get %s %s/%s: %w", ocmConfig.Kind, ns, ocmConfig.Name, err)
			}

			appendMutex.Lock()
			objects = append(objects, obj)
			appendMutex.Unlock()

			return nil
		})
	}

	if err := fetchGroup.Wait(); err != nil {
		return nil, err
	}

	// hash all the config so we sort based on data
	var configs []struct {
		hash   []byte
		config *genericv1.Config
	}

	hashGroup, ctx := errgroup.WithContext(ctx)
	configMutex := &sync.Mutex{}
	for _, obj := range objects {
		hashGroup.Go(func() error {
			cfg, err := GetConfigFromObject(obj)
			if err != nil {
				return err
			}

			if cfg == nil {
				return nil
			}

			hash, err := ocm.GetObjectHash(obj)
			if err != nil {
				return err
			}

			configMutex.Lock()
			configs = append(configs, struct {
				hash   []byte
				config *genericv1.Config
			}{
				hash:   hash,
				config: cfg,
			})
			configMutex.Unlock()

			return nil
		})
	}

	if err := hashGroup.Wait(); err != nil {
		return nil, err
	}

	sort.Slice(configs, func(i, j int) bool {
		return bytes.Compare(configs[i].hash, configs[j].hash) < 0
	})

	cfgs := make([]*genericv1.Config, 0, len(configs))
	for _, cfg := range configs {
		cfgs = append(cfgs, cfg.config)
	}

	return genericv1.FlatMap(cfgs...), nil
}
