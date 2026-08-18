package v1

import (
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const CredentialPluginType types.PluginType = "credentialPlugin" //nolint:gosec // G101 false positive: plugin-type discriminator, not a credential value

var Scheme *runtime.Scheme

func init() {
	Scheme = runtime.NewScheme()
	Scheme.MustRegisterWithAlias(&CapabilitySpec{}, runtime.NewUnversionedType(string(CredentialPluginType)))
}

// CapabilitySpec specifies the supported types of a plugin for
// a particular capability type.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type CapabilitySpec struct {
	Type                           runtime.Type `json:"type"`
	SupportedCredentialPluginTypes []types.Type `json:"supportedCredentialPluginTypes"`
	// CustomCredentialTypes allows plugins to introduce new credential types that are not predefined in the system.
	// The credential graph has no compiled-in Go type for them, so it resolves and hands them to the
	// plugin as runtime.Raw. The plugin converts them back into its own credential type.
	CustomCredentialTypes []types.Type `json:"customCredentialTypes,omitempty"`
}
