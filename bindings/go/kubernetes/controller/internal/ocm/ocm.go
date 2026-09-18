package ocm

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime/versioning"
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
		var refs []v1alpha1.OCMConfiguration
		for _, ref := range parent.GetEffectiveOCMConfig() {
			if ref.Policy == v1alpha1.ConfigurationPolicyPropagate {
				refs = append(refs, ref)
			}
		}
		return refs, nil
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

// GetLatestValidVersion returns the newest version satisfying the given semver
// constraint, ordered by the supplied versioning registry. An optional filter
// pre-selects candidate versions.
func GetLatestValidVersion(ctx context.Context, registry *versioning.Registry, versions []string, semvers string, filter ...func(string) bool) (string, error) {
	logger := log.FromContext(ctx)

	candidates := versions
	if len(filter) > 0 && filter[0] != nil {
		f := filter[0]
		candidates = make([]string, 0, len(versions))
		for _, version := range versions {
			if f(version) {
				candidates = append(candidates, version)
			}
		}
	}

	// Drop versions no configured scheme considers well-formed before applying
	// the constraint and sorting. Otherwise an unparseable version (e.g. "zzz")
	// survives an empty constraint and can sort above valid versions through the
	// lexical fallback, causing ApplyDowngradePolicy to pick a bogus candidate.
	valid := make([]string, 0, len(candidates))
	for _, version := range candidates {
		if registry.Valid(version) {
			valid = append(valid, version)
		}
	}

	matched, err := registry.Filter(valid, semvers)
	if err != nil {
		return "", err
	}
	if len(matched) == 0 {
		return "", fmt.Errorf("no valid versions found for constraint %s", semvers)
	}
	if len(matched) < len(valid) {
		logger.Info(fmt.Sprintf("filtered %d version(s) not satisfying constraint %s", len(valid)-len(matched), semvers))
	}

	if err := registry.SortDescending(matched); err != nil {
		return "", fmt.Errorf("sorting versions failed: %w", err)
	}
	return matched[0], nil
}

// ApplyDowngradePolicy returns the candidate version unless it is older than
// the previously reconciled version and Spec.DowngradePolicy denies it.
func ApplyDowngradePolicy(registry *versioning.Registry, component *v1alpha1.Component, candidate string) (string, error) {
	// we didn't yet reconcile anything, return whatever the retrieved version is.
	current := component.Status.Component.Version
	if current == "" {
		return candidate, nil
	}

	if !registry.Valid(current) {
		return "", reconcile.TerminalError(fmt.Errorf("failed to check reconciled version: %q is not a valid version", current))
	}
	c, err := registry.Compare(candidate, current)
	if err != nil {
		return "", reconcile.TerminalError(fmt.Errorf("failed to check reconciled version: %w", err))
	}
	if c >= 0 {
		return candidate, nil
	}

	switch component.Spec.DowngradePolicy {
	case v1alpha1.DowngradePolicyDeny:
		return "", reconcile.TerminalError(fmt.Errorf("component version cannot be downgraded from version %s "+
			"to version %s", current, candidate))
	case v1alpha1.DowngradePolicyAllow:
		return candidate, nil
	default:
		return "", reconcile.TerminalError(errors.New("unknown downgrade policy: " + string(component.Spec.DowngradePolicy)))
	}
}
