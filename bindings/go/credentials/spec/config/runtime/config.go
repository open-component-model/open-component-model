package runtime

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type Config struct {
	Type         runtime.Type            `json:"-"`
	Repositories []RepositoryConfigEntry `json:"-"`
	Consumers    []Consumer              `json:"-"`
}

// +k8s:deepcopy-gen=true
type RepositoryConfigEntry struct {
	Repository runtime.Typed `json:"-"`
}

// +k8s:deepcopy-gen=true
type Consumer struct {
	Identities  []runtime.Identity `json:"-"`
	Credentials []runtime.Typed    `json:"-"`
}
