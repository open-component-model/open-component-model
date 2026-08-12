package spec

import (
	"fmt"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ConfigType is the type identifier of the signing configuration as it appears
// as an entry inside the central generic configuration.
const ConfigType = "signing.config.ocm.software"

var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&Config{},
		runtime.NewVersionedType(ConfigType, Version),
		runtime.NewUnversionedType(ConfigType),
	)
}

// Config is the canonical wire format for signing settings. It is carried as an
// entry inside the central generic configuration
// (generic.config.ocm.software/v1) and extracted with [LookupConfig], making
// the central OCM configuration the single source of truth for how component
// versions are signed and verified.
//
// The [Config.Signer] and [Config.Verifier] fields hold the polymorphic signer
// and verifier specifications unchanged: their runtime type (for example
// RSASigningConfiguration/v1alpha1 or SigstoreSigningConfiguration/v1alpha1)
// is what selects the signing handler plugin. They are stored as [runtime.Raw]
// so this package does not need to depend on any concrete signer
// implementation; the consumer resolves them at the point of use.
//
//	type: generic.config.ocm.software/v1
//	configurations:
//	  - type: signing.config.ocm.software/v1alpha1
//	    signer:
//	      type: RSASigningConfiguration/v1alpha1
//	      signatureAlgorithm: RSASSA-PSS
//	      signatureEncodingPolicy: Plain
//	      signature: release
//	    verifier:
//	      type: RSASigningConfiguration/v1alpha1
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Config struct {
	// +ocm:jsonschema-gen:enum=signing.config.ocm.software/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=signing.config.ocm.software
	Type runtime.Type `json:"type"`

	// Signature is the name of the signature this config applies to, the same
	// name that defines the credentials used to produce it. If empty, the config
	// applies to every signature.
	Signature string `json:"signature,omitempty"`

	// Signer is the signer specification used when signing a component version
	// (using `ocm sign component-version`). Its runtime type selects
	// the signing handler plugin; the remaining fields configure that handler
	// (algorithm, encoding, keyless backend, ...). It never carries
	// credentials - keys are always resolved from the credentials
	// configuration. If nil, consumers fall back to the default
	// (RSASSA-PSS with Plain encoding).
	Signer *runtime.Raw `json:"signer,omitempty"`

	// Verifier is the verifier specification used when verifying a component
	// version (using `ocm verify component-version`). Its runtime
	// type selects the verification handler plugin. If nil, consumers fall back
	// to the default (RSASSA-PSS).
	Verifier *runtime.Raw `json:"verifier,omitempty"`
}

// Validate rejects a non-matching [Config.Type] and signer/verifier
// specifications that carry no runtime type. An empty Type is allowed so
// callers constructing a Config programmatically (without going through
// [Scheme.Decode]) do not need to set it explicitly.
func (cfg *Config) Validate() error {
	if cfg == nil {
		return nil
	}

	if !cfg.Type.IsEmpty() {
		if cfg.Type.Name != ConfigType || (cfg.Type.Version != "" && cfg.Type.Version != Version) {
			return fmt.Errorf("invalid type %q (must be %q or %q)",
				cfg.Type, ConfigType, runtime.NewVersionedType(ConfigType, Version))
		}
	}

	if cfg.Signer != nil && cfg.Signer.GetType().IsEmpty() {
		return fmt.Errorf("signer specification must have a type")
	}
	if cfg.Verifier != nil && cfg.Verifier.GetType().IsEmpty() {
		return fmt.Errorf("verifier specification must have a type")
	}
	return nil
}

// LookupConfig extracts the signing configuration that applies to every
// signature. Entries scoped to a signature are ignored.
func LookupConfig(cfg *genericv1.Config) (*Config, error) {
	return LookupConfigForSignature(cfg, "")
}

// LookupConfigForSignature extracts the signing configuration for the given
// signature name. Entries applying to every signature are merged first, then
// the ones scoped to that name, so a scoped entry wins regardless of file
// order. Returns nil if no entry applies.
func LookupConfigForSignature(cfg *genericv1.Config, signature string) (*Config, error) {
	if cfg == nil {
		return nil, nil
	}
	filtered, err := genericv1.Filter(cfg, &genericv1.FilterOptions{
		ConfigTypes: []runtime.Type{
			runtime.NewVersionedType(ConfigType, Version),
			runtime.NewUnversionedType(ConfigType),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to filter config: %w", err)
	}

	// we first need the configurations without a signature, then those which have a
	// signature.
	noSignatureConfigurations := make([]*Config, 0, len(filtered.Configurations))
	withSignatureConfigurations := make([]*Config, 0, len(filtered.Configurations))
	for _, entry := range filtered.Configurations {
		var config Config
		if err := Scheme.Convert(entry, &config); err != nil {
			return nil, fmt.Errorf("failed to decode signing config: %w", err)
		}
		if err := config.Validate(); err != nil {
			return nil, fmt.Errorf("invalid signing config: %w", err)
		}
		switch config.Signature {
		case "":
			noSignatureConfigurations = append(noSignatureConfigurations, &config)
		case signature:
			withSignatureConfigurations = append(withSignatureConfigurations, &config)
		}
	}
	// preserve overwrite ordering.
	return Merge(append(noSignatureConfigurations, withSignatureConfigurations...)...), nil
}

// Merge merges the provided configs into a single config. Later entries win: a
// non-nil Signer or Verifier and a non-empty Signature override whatever
// earlier entries set. Returns nil if no configs are provided.
func Merge(configs ...*Config) *Config {
	if len(configs) == 0 {
		return nil
	}

	merged := new(Config)
	merged.Type = runtime.NewVersionedType(ConfigType, Version)
	for _, cfg := range configs {
		if cfg == nil {
			continue
		}
		if cfg.Signature != "" {
			merged.Signature = cfg.Signature
		}
		if cfg.Signer != nil {
			merged.Signer = cfg.Signer
		}
		if cfg.Verifier != nil {
			merged.Verifier = cfg.Verifier
		}
	}
	return merged
}
