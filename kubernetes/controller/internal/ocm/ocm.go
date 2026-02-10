package ocm

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"

	"github.com/Masterminds/semver/v3"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
)

// GetEffectiveConfig returns the effective configuration for the given config
// ref provider object. Therefore, references to config maps and secrets (that
// are supposed to contain ocm configuration data) are directly returned.
// Furthermore, references to other ocm objects are resolved and their effective
// configuration (so again, config map and secret references) with policy
// propagate are returned.
func GetEffectiveConfig(ctx context.Context, client ctrl.Client, obj v1alpha1.ConfigRefProvider, parent v1alpha1.ConfigRefProvider) ([]v1alpha1.OCMConfiguration, error) {
	configs := obj.GetSpecifiedOCMConfig()

	if len(configs) == 0 && parent != nil {
		for _, ref := range parent.GetEffectiveOCMConfig() {
			if ref.Policy == v1alpha1.ConfigurationPolicyPropagate {
				configs = append(configs, ref)
			}
		}
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

func RegexpFilter(regex string) (func(string) bool, error) {
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

func GetLatestValidVersion(ctx context.Context, versions []string, semvers string, filter ...func(string) bool) (*semver.Version, error) {
	logger := log.FromContext(ctx)
	constraint, err := semver.NewConstraint(semvers)
	if err != nil {
		return nil, err
	}

	var f func(string) bool
	filtered := versions
	if len(filter) > 0 {
		f = filter[0]
		for _, version := range versions {
			if f(version) {
				filtered = append(filtered, version)
			}
		}
	}

	var validVersions semver.Collection
	for _, version := range filtered {
		if v, err := semver.NewVersion(version); err == nil {
			validVersions = append(validVersions, v)
		} else {
			logger.Info(fmt.Sprintf("Invalid version: %s", version))
		}
	}

	var matchedVersions semver.Collection
	for _, validVersion := range validVersions {
		if constraint.Check(validVersion) {
			matchedVersions = append(matchedVersions, validVersion)
		}
	}

	sort.Sort(matchedVersions)

	if len(matchedVersions) == 0 {
		return nil, fmt.Errorf("no valid versions found for constraint %s", semvers)
	}

	return matchedVersions[len(matchedVersions)-1], nil
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
