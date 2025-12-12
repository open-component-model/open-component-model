package v1

import (
	"encoding/json"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// Version specifies the current version of the credentials configuration
const Version = "v1"

const (
	// ConfigType defines the type identifier for credential configurations
	ConfigType = "credentials.config.ocm.software"
	// CredentialsType defines the type identifier for credentials
	CredentialsType = "Credentials"
)

// MustRegister registers the credential configuration types with the runtime scheme.
// It registers both versioned and unversioned types for DirectCredentials and Config.
func MustRegister(scheme *runtime.Scheme) {
	direct := &DirectCredentials{}
	scheme.MustRegisterWithAlias(direct, runtime.NewUnversionedType(CredentialsType))
	scheme.MustRegisterWithAlias(direct, runtime.NewVersionedType(CredentialsType, Version))
	config := &Config{}
	scheme.MustRegisterWithAlias(config, runtime.NewUnversionedType(ConfigType))
	scheme.MustRegisterWithAlias(config, runtime.NewVersionedType(ConfigType, Version))
}

// Config represents the top-level configuration for credentials management.
// It contains repository configurations and consumer definitions.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Config struct {
	// Type specifies the type of the configuration
	// +ocm:jsonschema-gen:enum=credentials.config.ocm.software/v1
	// +ocm:jsonschema-gen:enum:deprecated=credentials.config.ocm.software
	Type runtime.Type `json:"type"`
	// Repositories contains configuration entries for repositories
	Repositories []RepositoryConfigEntry `json:"repositories,omitempty"`
	// Consumers defines the consumers and their associated credentials
	Consumers []Consumer `json:"consumers,omitempty"`
}

// RepositoryConfigEntry represents a single repository configuration entry.
// It contains the raw repository configuration data.
//
// +k8s:deepcopy-gen=true
type RepositoryConfigEntry struct {
	// Repository contains the raw repository configuration data
	Repository *runtime.Raw `json:"repository"`
}

// Consumer represents a consumer of credentials with associated identities and credentials.
// It supports both single and multiple identity configurations.
//
// +k8s:deepcopy-gen=true
type Consumer struct {
	// Identities contains the list of identities associated with this consumer
	Identities []runtime.Identity `json:"identities"`
	// Credentials contains the list of credentials associated with this consumer
	Credentials []*runtime.Raw `json:"credentials"`
}

// UnmarshalJSON implements custom JSON unmarshaling for Consumer.
// It handles both legacy single-identity format and the current multi-identity format.
// This ensures backward compatibility with older configurations.
func (a *Consumer) UnmarshalJSON(data []byte) error {
	type ConsumerWithSingleIdentity struct {
		// Legacy Identity field for backward compatibility
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
