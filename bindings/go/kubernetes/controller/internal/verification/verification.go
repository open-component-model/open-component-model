package verification

import (
	"fmt"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/pkg/configuration"
	rsasigningv1alpha1 "ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	signingspec "ocm.software/open-component-model/bindings/go/signing/v1alpha1/spec"
)

// signingConfigTypes are the OCM configuration entries that request component signature verification.
var signingConfigTypes = []runtime.Type{
	runtime.NewVersionedType(signingspec.ConfigType, signingspec.Version),
	runtime.NewUnversionedType(signingspec.ConfigType),
}

// Verification is an internal representation of a requested component signature verification. The public key
// comes from the credential graph during resolution.
type Verification struct {
	Signature string        `json:"signature"`
	Verifier  runtime.Typed `json:"verifier"`
}

// GetVerifications reads the requested verifications from the OCM configuration. Only entries that have a
// signature name set are considered.
func GetVerifications(cfg *configuration.Configuration) ([]Verification, error) {
	if cfg == nil || cfg.Config == nil {
		return nil, nil
	}

	filtered, err := genericv1.Filter(cfg.Config, &genericv1.FilterOptions{ConfigTypes: signingConfigTypes})
	if err != nil {
		return nil, fmt.Errorf("failed to filter signing configuration: %w", err)
	}

	var verifications []Verification
	seen := make(map[string]struct{}, len(filtered.Configurations))
	for _, entry := range filtered.Configurations {
		var signingCfg signingspec.Config
		if err := signingspec.Scheme.Convert(entry, &signingCfg); err != nil {
			return nil, fmt.Errorf("failed to decode signing configuration: %w", err)
		}

		if signingCfg.Signature == "" {
			continue
		}

		// more than one entry could be here with the same signature
		if _, ok := seen[signingCfg.Signature]; ok {
			continue
		}
		seen[signingCfg.Signature] = struct{}{}

		verifier, err := verifierForSignature(cfg.Config, signingCfg.Signature)
		if err != nil {
			return nil, err
		}

		verifications = append(verifications, Verification{
			Signature: signingCfg.Signature,
			Verifier:  verifier,
		})
	}

	return verifications, nil
}

// verifierForSignature resolves the verifier specification for an already-requested signature, falling back
// to the default RSASSA-PSS verifier when neither a scoped nor an unscoped entry configures one.
// This behavior is copied from the CLI.
func verifierForSignature(cfg *genericv1.Config, signature string) (runtime.Typed, error) {
	signingCfg, err := signingspec.LookupConfigForSignature(cfg, signature)
	if err != nil {
		return nil, fmt.Errorf("failed to look up signing configuration for signature %q: %w", signature, err)
	}

	if signingCfg != nil && signingCfg.Verifier != nil {
		return signingCfg.Verifier, nil
	}

	spec := &rsasigningv1alpha1.Config{}
	if _, err := rsasigningv1alpha1.Scheme.DefaultType(spec); err != nil {
		return nil, fmt.Errorf("failed to default verifier specification for signature %q: %w", signature, err)
	}

	return spec, nil
}
