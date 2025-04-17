package runtime

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Config represents the top-level configuration for credentials management.
// It contains repository configurations and consumer definitions.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type Config struct {
	Type         runtime.Type            `json:"-"`
	Repositories []RepositoryConfigEntry `json:"-"`
	Consumers    []Consumer              `json:"-"`
}

// RepositoryConfigEntry represents a single repository configuration entry.
// It contains the raw repository configuration data.
//
// +k8s:deepcopy-gen=true
type RepositoryConfigEntry struct {
	Repository runtime.Typed `json:"-"`
}

// Consumer represents a consumer of credentials with associated identities and credentials.
// It supports both single and multiple identity configurations.
//
// +k8s:deepcopy-gen=true
type Consumer struct {
	Identities  []runtime.Identity `json:"-"`
	Credentials []runtime.Typed    `json:"-"`
}
