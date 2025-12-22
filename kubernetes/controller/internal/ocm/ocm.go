package ocm

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/Masterminds/semver/v3"
	"github.com/mandelsoft/goutils/matcher"
	corev1 "k8s.io/api/core/v1"
	"ocm.software/ocm/api/credentials/extensions/repositories/dockerconfig"
	"ocm.software/ocm/api/ocm"
	"ocm.software/ocm/api/ocm/compdesc"
	utils "ocm.software/ocm/api/ocm/ocmutils"
	common "ocm.software/ocm/api/utils/misc"
	"ocm.software/ocm/api/utils/runtime"
	"ocm.software/ocm/api/utils/semverutils"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
)

// ConfigureContextForSecretOrConfigMap wraps ConfigureContextForSecret and
// ConfigureContextForConfigMaps to configure the ocm context.
func ConfigureContextForSecretOrConfigMap(ctx context.Context, octx ocm.Context, obj ctrl.Object) error {
	var err error
	switch o := obj.(type) {
	case *corev1.Secret:
		err = ConfigureContextForSecret(ctx, octx, o)
	case *corev1.ConfigMap:
		err = ConfigureContextForConfigMaps(ctx, octx, o)
	default:
		return fmt.Errorf("unsupported configuration object type: %T", obj)
	}
	if err != nil {
		return fmt.Errorf("configure context failed for %s "+
			"%s/%s: %w", obj.GetObjectKind(), obj.GetNamespace(), obj.GetName(), err)
	}

	return nil
}

// ConfigureContextForSecret adds the ocm configuration data as well as
// credentials in the docker config json format found in the secret to the
// ocm context.
func ConfigureContextForSecret(_ context.Context, octx ocm.Context, secret *corev1.Secret) error {
	if dockerConfigBytes, ok := secret.Data[corev1.DockerConfigJsonKey]; ok {
		if len(dockerConfigBytes) > 0 {
			spec := dockerconfig.NewRepositorySpecForConfig(dockerConfigBytes, true)

			if _, err := octx.CredentialsContext().RepositoryForSpec(spec); err != nil {
				return fmt.Errorf("failed to apply credentials from docker"+
					"config json in secret %s/%s: %w", secret.Namespace, secret.Name, err)
			}
		}
	}

	if ocmConfigBytes, ok := secret.Data[v1alpha1.OCMConfigKey]; ok {
		if len(ocmConfigBytes) > 0 {
			cfg, err := octx.ConfigContext().GetConfigForData(ocmConfigBytes, runtime.DefaultYAMLEncoding)
			if err != nil {
				return fmt.Errorf("failed to deserialize ocm config data in secret "+
					"%s/%s: %w", secret.Namespace, secret.Name, err)
			}

			err = octx.ConfigContext().ApplyConfig(cfg, fmt.Sprintf("ocm config secret: %s/%s",
				secret.Namespace, secret.Name))
			if err != nil {
				return fmt.Errorf("failed to apply ocm config in secret "+
					"%s/%s: %w", secret.Namespace, secret.Name, err)
			}
		}
	}

	return nil
}

// ConfigureContextForConfigMaps adds the ocm configuration data found in the
// secret to the ocm context.
func ConfigureContextForConfigMaps(_ context.Context, octx ocm.Context, configmap *corev1.ConfigMap) error {
	ocmConfigData, ok := configmap.Data[v1alpha1.OCMConfigKey]
	if !ok {
		return fmt.Errorf("ocm configuration config map does not contain key \"%s\"",
			v1alpha1.OCMConfigKey)
	}
	if len(ocmConfigData) > 0 {
		cfg, err := octx.ConfigContext().GetConfigForData([]byte(ocmConfigData), nil)
		if err != nil {
			return fmt.Errorf("failed to deserialize ocm config data in config map "+
				"%s/%s: %w", configmap.Namespace, configmap.Name, err)
		}
		err = octx.ConfigContext().ApplyConfig(cfg, fmt.Sprintf("%s/%s",
			configmap.Namespace, configmap.Name))
		if err != nil {
			return fmt.Errorf("failed to apply ocm config in config map "+
				"%s/%s: %w", configmap.Namespace, configmap.Name, err)
		}
	}

	return nil
}

// GetEffectiveConfig returns the effective configuration for the given config
// ref provider object. Therefore, references to config maps and secrets (that
// are supposed to contain ocm configuration data) are directly returned.
// Furthermore, references to other ocm objects are resolved and their effective
// configuration (so again, config map and secret references) with policy
// propagate are returned.
func GetEffectiveConfig(ctx context.Context, client ctrl.Client, obj v1alpha1.ConfigRefProvider) ([]v1alpha1.OCMConfiguration, error) {
	configs := obj.GetSpecifiedOCMConfig()

	if len(configs) == 0 {
		return nil, nil
	}

	var refs []v1alpha1.OCMConfiguration
	for _, config := range configs {
		if config.Namespace == "" {
			config.Namespace = obj.GetNamespace()
		}

		if config.Kind == "Secret" || config.Kind == "ConfigMap" {
			if config.APIVersion == "" {
				config.APIVersion = corev1.SchemeGroupVersion.String()
			}
			refs = append(refs, config)
		} else {
			var resource v1alpha1.ConfigRefProvider
			if config.APIVersion == "" {
				return nil, fmt.Errorf("api version must be set for reference of kind %s", config.Kind)
			}

			switch config.Kind {
			case v1alpha1.KindRepository:
				resource = &v1alpha1.Repository{}
			case v1alpha1.KindComponent:
				resource = &v1alpha1.Component{}
			case v1alpha1.KindResource:
				resource = &v1alpha1.Resource{}
			default:
				return nil, fmt.Errorf("unsupported reference kind: %s", config.Kind)
			}

			if err := client.Get(ctx, ctrl.ObjectKey{Namespace: config.Namespace, Name: config.Name}, resource); err != nil {
				return nil, fmt.Errorf("failed to fetch resource %s: %w", config.Name, err)
			}

			for _, ref := range resource.GetEffectiveOCMConfig() {
				if ref.Policy == v1alpha1.ConfigurationPolicyPropagate {
					// do not propagate the policy of the parent resource but set
					// the policy specified in the respective config (of the
					// object being reconciled)
					ref.Policy = config.Policy
					refs = append(refs, ref)
				}
			}
		}
	}

	return refs, nil
}

func RegexpFilter(regex string) (matcher.Matcher[string], error) {
	if regex == "" {
		return func(_ string) bool {
			return true
		}, nil
	}
	match, err := regexp.Compile(regex)
	if err != nil {
		return nil, err
	}

	return func(s string) bool {
		return match.MatchString(s)
	}, nil
}

func GetLatestValidVersion(_ context.Context, versions []string, semvers string, filter ...matcher.Matcher[string]) (*semver.Version, error) {
	constraint, err := semver.NewConstraint(semvers)
	if err != nil {
		return nil, err
	}

	var f matcher.Matcher[string]
	filtered := versions
	if len(filter) > 0 {
		f = filter[0]
		for _, version := range versions {
			if f(version) {
				filtered = append(filtered, version)
			}
		}
	}
	vers, err := semverutils.MatchVersionStrings(filtered, constraint)
	if err != nil {
		return nil, err
	}

	if len(vers) == 0 {
		return nil, fmt.Errorf("no valid versions found for constraint %s", semvers)
	}

	return vers[len(vers)-1], nil
}

func ListComponentDescriptors(_ context.Context, cv ocm.ComponentVersionAccess, r ocm.ComponentVersionResolver) (*Descriptors, error) {
	descriptors := &Descriptors{}
	_, err := utils.Walk(nil, cv, r,
		func(_ common.WalkingState[*compdesc.ComponentDescriptor, ocm.ComponentVersionAccess], cv ocm.ComponentVersionAccess) (bool, error) {
			descriptors.List = append(descriptors.List, cv.GetDescriptor())

			return true, nil
		})
	if err != nil {
		return nil, err
	}

	return descriptors, nil
}

// IsDowngradable checks whether a component version (currentcv) is downgrabale to another component version (latestcv).
func IsDowngradable(_ context.Context, currentcv *descruntime.Descriptor, latestcv *descruntime.Descriptor) (bool, error) {
	// Find the downgradable label
	var downgradableValue json.RawMessage
	for _, label := range currentcv.Component.Labels {
		if label.Name == v1alpha1.OCMLabelDowngradable {
			downgradableValue = label.Value
			break
		}
	}

	if len(downgradableValue) == 0 {
		return false, nil
	}

	var vers string
	err := json.Unmarshal(downgradableValue, &vers)
	if err != nil {
		return false, err
	}
	constraint, err := semver.NewConstraint(vers)
	if err != nil {
		return false, err
	}
	semvers, err := semver.NewVersion(latestcv.Component.Version)
	if err != nil {
		return false, err
	}

	return constraint.Check(semvers), nil
}
