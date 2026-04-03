package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const ConfigType = "ECDSASigningConfiguration"

var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&Config{},
		runtime.NewUnversionedType(ConfigType),
		runtime.NewVersionedType(ConfigType, Version),
	)
}

// Config defines configuration for signing based on ECDSA with NIST curves (P-256, P-384, P-521).
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Config struct {
	// Type identifies this configuration object's runtime type.
	// +ocm:jsonschema-gen:enum=ECDSASigningConfiguration/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=ECDSASigningConfiguration
	Type runtime.Type `json:"type"`

	SignatureEncodingPolicy SignatureEncodingPolicy `json:"signatureEncodingPolicy,omitempty"`

	SignatureAlgorithm SignatureAlgorithm `json:"signatureAlgorithm,omitempty"`
}

// SetType is a setter function for the config type.
func (t *Config) SetType(typ runtime.Type) {
	t.Type = typ
}

// GetType is a getter function for the config type.
func (t *Config) GetType() runtime.Type {
	return t.Type
}

// DeepCopyInto copies the receiver into out. in must be non-nil.
func (in *Config) DeepCopyInto(out *Config) {
	*out = *in
	out.Type = in.Type
}

// DeepCopy creates a new Config by copying the receiver.
func (in *Config) DeepCopy() *Config {
	if in == nil {
		return nil
	}
	out := new(Config)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyTyped creates a new runtime.Typed by copying the receiver.
func (in *Config) DeepCopyTyped() runtime.Typed {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

func (cfg *Config) GetSignatureEncodingPolicy() SignatureEncodingPolicy {
	if cfg == nil || cfg.SignatureEncodingPolicy == "" {
		return SignatureEncodingPolicyDefault
	}
	return cfg.SignatureEncodingPolicy
}

func (cfg *Config) GetSignatureAlgorithm() SignatureAlgorithm {
	if cfg == nil || cfg.SignatureAlgorithm == "" {
		return AlgorithmECDSAP256
	}
	return cfg.SignatureAlgorithm
}

func (cfg *Config) GetDefaultMediaType() string {
	switch cfg.GetSignatureAlgorithm() {
	case AlgorithmECDSAP256:
		return MediaTypePlainECDSAP256
	case AlgorithmECDSAP384:
		return MediaTypePlainECDSAP384
	case AlgorithmECDSAP521:
		return MediaTypePlainECDSAP521
	default:
		return ""
	}
}
