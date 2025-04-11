package v1

import (
	"encoding/json"

	"ocm.software/open-component-model/bindings/go/runtime"
)

const Version = "v1"

const (
	ConfigType      = "credentials.config.ocm.software"
	CredentialsType = "Credentials"
)

func MustRegister(scheme *runtime.Scheme) {
	direct := &DirectCredentials{}
	scheme.MustRegisterWithAlias(direct, runtime.NewUnversionedType(CredentialsType))
	scheme.MustRegisterWithAlias(direct, runtime.NewVersionedType(CredentialsType, Version))
	config := &Config{}
	scheme.MustRegisterWithAlias(config, runtime.NewUnversionedType(ConfigType))
	scheme.MustRegisterWithAlias(config, runtime.NewVersionedType(ConfigType, Version))
}

// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type Config struct {
	Type         runtime.Type            `json:"type"`
	Repositories []RepositoryConfigEntry `json:"repositories,omitempty"`
	Consumers    []Consumer              `json:"consumers,omitempty"`
}

// +k8s:deepcopy-gen=true
type RepositoryConfigEntry struct {
	Repository *runtime.Raw `json:"repository"`
}

// +k8s:deepcopy-gen=true
type Consumer struct {
	Identities  []runtime.Identity `json:"identities"`
	Credentials []*runtime.Raw     `json:"credentials"`
}

// UnmarshalJSON unmarshals a consumer with a single identity into a consumer with multiple identities.
func (a *Consumer) UnmarshalJSON(data []byte) error {
	type ConsumerWithSingleIdentity struct {
		// Legacy Identity field
		runtime.Identity `json:"identity,omitempty"`
		Identities       []runtime.Identity `json:"identities"`
		Credentials      []*runtime.Raw     `json:"credentials"`
	}
	var consumer ConsumerWithSingleIdentity
	if err := json.Unmarshal(data, &consumer); err != nil {
		return err
	}
	if consumer.Identity != nil {
		consumer.Identities = append(consumer.Identities, consumer.Identity)
	}
	if a == nil {
		*a = Consumer{}
	}
	a.Identities = consumer.Identities
	a.Credentials = consumer.Credentials
	return nil
}
