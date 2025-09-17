package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const ConfigType = "RSASigningConfiguration"

var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&Config{},
		runtime.NewUnversionedType(ConfigType),
		runtime.NewVersionedType(ConfigType, Version),
	)
}

// Config defines configuration for signing based on AlgorithmRSASSAPSS or AlgorithmRSASSAPKCS1V15.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type Config struct {
	// Type identifies this configuration object’s runtime type.
	Type runtime.Type `json:"type"`

	SignatureEncodingPolicy SignatureEncodingPolicy `json:"signatureEncodingPolicy,omitempty"`

	// SignatureAlgorithm is the signature algorithm to use when creating new signatures.
	// This field is optional and defaults to AlgorithmRSASSAPSS. For verification, this field is ignored
	// and the signature algorithm is inferred from the signature specification.
	SignatureAlgorithm string `json:"signatureAlgorithm,omitempty"`
}

func (cfg *Config) GetSignatureEncodingPolicy() SignatureEncodingPolicy {
	if cfg == nil || cfg.SignatureEncodingPolicy == "" {
		return SignatureEncodingPolicyPEM
	}
	return cfg.SignatureEncodingPolicy
}

func (cfg *Config) GetSignatureAlgorithm() string {
	if cfg == nil || cfg.SignatureAlgorithm == "" {
		return AlgorithmRSASSAPSS
	}
	return cfg.SignatureAlgorithm
}

func (cfg *Config) GetDefaultMediaType() string {
	switch cfg.GetSignatureAlgorithm() {
	case AlgorithmRSASSAPSS:
		return MediaTypePlainRSASSAPSS
	case AlgorithmRSASSAPKCS1V15:
		return MediaTypePlainRSASSAPKCS1V15
	default:
		return ""
	}
}
