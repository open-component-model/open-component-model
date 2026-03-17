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
	"slices"
	"strings"
	"sync"

	"github.com/docker/cli/cli/config/configfile"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	ocmconfigv1spec "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/spec"
	resolversv1alpha1spec "ocm.software/open-component-model/bindings/go/configuration/resolvers/v1alpha1/spec"
	credentialsv1spec "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/credentials"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
)

// allowedConfigTypes defines the set of OCM config types accepted by the controller.
// Any config entry whose type is not in this list is silently dropped to reduce the
// attack surface from user-supplied Kubernetes Secrets/ConfigMaps.
var allowedConfigTypes = []runtime.Type{
	// credentials
	runtime.NewVersionedType(credentialsv1spec.ConfigType, credentialsv1spec.Version),
	runtime.NewUnversionedType(credentialsv1spec.ConfigType),
	// legacy fallback resolvers
	runtime.NewVersionedType(ocmconfigv1spec.ConfigType, ocmconfigv1spec.Version),
	runtime.NewUnversionedType(ocmconfigv1spec.ConfigType),
	// path-matcher resolvers (v1alpha1)
	runtime.NewVersionedType(resolversv1alpha1spec.ConfigType, resolversv1alpha1spec.Version),
	runtime.NewUnversionedType(resolversv1alpha1spec.ConfigType),
}

// filterAllowedConfigTypes filters the provided config to only include config entries whose
// types are in the allowedConfigTypes list. Additionally, it strips the deprecated Aliases field
// from any ocm.config.ocm.software entries.
func filterAllowedConfigTypes(cfg *genericv1.Config) (*genericv1.Config, error) {
	filtered, err := genericv1.Filter(cfg, &genericv1.FilterOptions{ConfigTypes: allowedConfigTypes})
	if err != nil {
		return nil, fmt.Errorf("failed to filter config types: %w", err)
	}

	ocmConfigTypes := []runtime.Type{
		runtime.NewVersionedType(ocmconfigv1spec.ConfigType, ocmconfigv1spec.Version),
		runtime.NewUnversionedType(ocmconfigv1spec.ConfigType),
	}

	// The Filter call above only enforces the type allowlist. However, ocm.config.ocm.software
	// entries may carry an Aliases field which is deprecated and ignored with a warning by the
	// OCM library. We strip it proactively here at the config-loading boundary so that
	// user-supplied alias mappings from Kubernetes Secrets/ConfigMaps are never forwarded to
	// the library at all. Resolvers in those same entries are left intact.
	for i, entry := range filtered.Configurations {
		if !slices.Contains(ocmConfigTypes, entry.GetType()) {
			continue
		}

		var ocmCfg ocmconfigv1spec.Config //nolint:staticcheck // we use this on purpose to filter out deprecated types
		if err := ocmconfigv1spec.Scheme.Convert(entry, &ocmCfg); err != nil {
			return nil, fmt.Errorf("failed to convert ocm config entry: %w", err)
		}
		ocmCfg.Aliases = nil

		raw := &runtime.Raw{}
		if err := ocmconfigv1spec.Scheme.Convert(&ocmCfg, raw); err != nil {
			return nil, fmt.Errorf("failed to re-serialise ocm config entry: %w", err)
		}
		filtered.Configurations[i] = raw
	}

	return filtered, nil
}

// GetConfigFromSecret extracts and decodes OCM configuration from a Kubernetes Secret.
// It looks for configuration data under the OCMConfigKey.
func GetConfigFromSecret(secret *corev1.Secret) (*genericv1.Config, error) {
	if data, ok := secret.Data[v1alpha1.OCMConfigKey]; ok {
		if len(data) == 0 {
			return nil, errors.New("no OCM config data found in secret")
		}

		var cfg genericv1.Config
		if err := genericv1.Scheme.Decode(bytes.NewReader(data), &cfg); err != nil {
			return nil, fmt.Errorf("failed to decode ocm config from secret %s/%s: %w",
				secret.Namespace, secret.Name, err)
		}

		return &cfg, nil
	}

	if data, ok := secret.Data[corev1.DockerConfigJsonKey]; ok {
		if len(data) == 0 {
			return nil, errors.New("no docker config found in secret")
		}

		return createConfigFromDockerConfig(data)
	}

	return nil, fmt.Errorf("secret does not contain supported keys %q", []string{v1alpha1.OCMConfigKey, corev1.DockerConfigJsonKey})
}

// createConfigFromDockerConfig creates a generic OCM configuration from a Docker configuration.
// It takes the raw Docker configuration data as input, processes it, and returns a genericv1.Config object.
func createConfigFromDockerConfig(data []byte) (*genericv1.Config, error) {
	// Validate that data is valid Docker config JSON
	if err := json.Unmarshal(data, &configfile.ConfigFile{}); err != nil {
		return nil, fmt.Errorf("invalid docker config: %w", err)
	}

	dockerConfig := &v1.DockerConfig{}
	if _, err := credentials.Scheme.DefaultType(dockerConfig); err != nil {
		return nil, fmt.Errorf("failed to get default type for docker config type %T: %w", dockerConfig, err)
	}

	dockerConfig.DockerConfig = string(data)
	raw := &runtime.Raw{}
	if err := credentials.Scheme.Convert(dockerConfig, raw); err != nil {
		return nil, fmt.Errorf("failed to convert docker config to raw: %w", err)
	}

	credScheme := runtime.NewScheme()
	credentialsv1spec.MustRegister(credScheme)
	credConfig := &credentialsv1spec.Config{
		Repositories: []credentialsv1spec.RepositoryConfigEntry{{Repository: raw}},
	}
	if _, err := credScheme.DefaultType(credConfig); err != nil {
		return nil, fmt.Errorf("failed to get default type for credentials config: %w", err)
	}

	rawCreds := &runtime.Raw{}
	if err := credScheme.Convert(credConfig, rawCreds); err != nil {
		return nil, fmt.Errorf("failed to convert credential config to raw type: %w", err)
	}

	cfg := &genericv1.Config{
		Type:           runtime.Type{Version: genericv1.Version, Name: genericv1.ConfigType},
		Configurations: []*runtime.Raw{rawCreds},
	}

	return cfg, nil
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
// The hash is provided along with the configuration data for caching purposes.
type Configuration struct {
	Hash   []byte
	Config *genericv1.Config
}

// LoadConfigurations loads OCM configurations from a list of OCMConfiguration references.
// It fetches the referenced Secrets/ConfigMaps from the cluster and extracts their configuration into a flat map and
// calculates the hash of the configuration data. The object fetching happens concurrently, but Spec declaration order
// is preserved. Meaning, in whatever order the original object declared the configuration, that order is preserved.
func LoadConfigurations(ctx context.Context, k8sClient client.Reader, namespace string, ocmConfigs []v1alpha1.OCMConfiguration) (*Configuration, error) {
	if len(ocmConfigs) == 0 {
		return nil, nil
	}

	objects, err := getConfigurationObjects(ctx, k8sClient, ocmConfigs, namespace)
	if err != nil {
		return nil, err
	}

	var configs []*genericv1.Config
	for _, obj := range objects {
		cfg, err := GetConfigFromObject(obj)
		if err != nil {
			return nil, err
		}

		if cfg == nil {
			continue
		}

		configs = append(configs, cfg)
	}

	flattened := genericv1.FlatMap(configs...)
	if flattened == nil {
		return nil, nil
	}

	flattenedFiltered, err := filterAllowedConfigTypes(flattened)
	if err != nil {
		return nil, fmt.Errorf("failed to apply config type allowlist: %w", err)
	}

	content, err := json.Marshal(flattenedFiltered)
	if err != nil {
		return nil, err
	}

	hasher := sha256.New()
	hasher.Write(content)
	hash := hasher.Sum(nil)

	result := Configuration{
		Config: flattenedFiltered,
		Hash:   hash,
	}

	return &result, nil
}

// gatherConfigurationObjects fetches the referenced Secrets/ConfigMaps from the cluster. It does so concurrently and by
// preserving the order of the input list. The order of the input list is defined by the Spec defining the configuration
// references.
func getConfigurationObjects(ctx context.Context, k8sClient client.Reader, ocmConfigs []v1alpha1.OCMConfiguration, namespace string) ([]client.Object, error) {
	fetchGroup, ctx := errgroup.WithContext(ctx)
	m := make(map[int]client.Object)
	loopLock := &sync.Mutex{}
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

			loopLock.Lock()
			m[i] = obj
			loopLock.Unlock()

			return nil
		})
	}

	if err := fetchGroup.Wait(); err != nil {
		return nil, err
	}

	objects := make([]client.Object, 0, len(ocmConfigs))
	for i := range ocmConfigs {
		obj, ok := m[i]
		if !ok {
			return nil, fmt.Errorf("failed to get configuration object for index %d", i)
		}

		objects = append(objects, obj)
	}

	return objects, nil
}
