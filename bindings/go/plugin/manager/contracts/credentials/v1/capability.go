package v1

import (
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const CredentialRepositoryPluginType types.PluginType = "credentialRepository" //nolint:gosec // not hardcoded cred

var Scheme *runtime.Scheme

func init() {
	Scheme = runtime.NewScheme()
	Scheme.MustRegisterWithAlias(&CapabilitySpec{}, runtime.NewUnversionedType(string(CredentialRepositoryPluginType)))
}

// CapabilitySpec specifies the supported types of a plugin for
// a particular capability type.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type CapabilitySpec struct {
	Type                           runtime.Type `json:"type"`
	SupportedConsumerIdentityTypes []types.Type `json:"supportedConsumerIdentityTypes"`
	// TODO(fabianburth): customize / optimize for credentials
	//  currently, it uses the general types.Type, but we might want to tailor this
	//  to credentials specifically.
	SupportedCredentialRepositorySpecTypes []types.Type `json:"supportedCredentialRepositorySpecTypes"`
	// CustomCredentialTypes allows plugins to introduce new credential types that are not predefined in the system.
	// The graph internally provides them as runtime.Raw and the plugins itself need to convert them back.
	CustomCredentialTypes []types.Type `json:"customCredentialTypes,omitempty"`
}
