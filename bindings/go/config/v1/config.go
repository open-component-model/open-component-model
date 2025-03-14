package v1

import (
	"encoding/json"

	"ocm.software/open-component-model/bindings/go/runtime"
)

var DefaultConfigTypeRegistry = runtime.NewScheme()

func init() {
	DefaultConfigTypeRegistry.MustRegisterWithAlias(&Config{}, runtime.NewUngroupedVersionedType(ConfigType, "v1"))
}

type Attribute string

type Attributes map[string]Attribute

const (
	ConfigType   = "credentials.config.ocm.software"
	ConfigTypeV1 = ConfigType + "/" + "v1"
)

type Config struct {
	runtime.Type `json:"type"`
	Repositories []RepositoryConfigEntry `json:"repositories,omitempty"`
	Consumers    []Consumer              `json:"consumers,omitempty"`
}

type RepositoryConfigEntry struct {
	Repository Repository `json:"repository"`
}

type Repository struct {
	runtime.Typed `json:",inline"`
}

func (a *Repository) UnmarshalJSON(data []byte) error {
	raw := &runtime.Raw{}
	if err := json.Unmarshal(data, raw); err != nil {
		return err
	}
	a.Typed = raw
	return nil
}

func (a *Repository) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.Typed)
}

type Consumer struct {
	Identities  []Identity    `json:"identities"`
	Credentials []Credentials `json:"credentials"`
}

// UnmarshalJSON unmarshals a consumer with a single identity into a consumer with multiple identities.
func (a *Consumer) UnmarshalJSON(data []byte) error {
	type ConsumerWithSingleIdentity struct {
		// Legacy Identity field
		Identity    `json:"identity,omitempty"`
		Identities  []Identity    `json:"identities"`
		Credentials []Credentials `json:"credentials"`
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

type Credentials struct {
	runtime.Typed `json:",inline"`
}

func (a *Credentials) UnmarshalJSON(data []byte) error {
	a.Typed = &runtime.Raw{}
	return json.Unmarshal(data, a.Typed)
}

func (a *Credentials) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.Typed)
}

type DirectCredentials struct {
	runtime.Type `json:"type"`
	Properties   Attributes `json:"properties"`
}
