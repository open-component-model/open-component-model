package runtime

import (
	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func ConvertFromV1(config *v1.Config) *Config {
	return &Config{
		Type:         config.Type,
		Repositories: convertFromV1Repositories(config.Repositories),
		Consumers:    convertFromV1Consumers(config.Consumers),
	}
}

func convertFromV1Consumers(consumers []v1.Consumer) []Consumer {
	entries := make([]Consumer, len(consumers))
	for i, consumer := range consumers {
		entries[i] = Consumer{
			Identities:  deepCopyIdentities(consumer.Identities),
			Credentials: convertFromV1Credentials(consumer.Credentials),
		}
	}
	return entries
}

func deepCopyIdentities(identities []runtime.Identity) []runtime.Identity {
	nidentities := make([]runtime.Identity, len(identities))
	for i, identity := range identities {
		nidentities[i] = identity.DeepCopy()
	}
	return nidentities
}

func convertFromV1Credentials(credentials []*runtime.Raw) []runtime.Typed {
	entries := make([]runtime.Typed, len(credentials))
	for i, cred := range credentials {
		entries[i] = cred.DeepCopy()
	}
	return entries
}

func convertFromV1Repositories(repositories []v1.RepositoryConfigEntry) []RepositoryConfigEntry {
	entries := make([]RepositoryConfigEntry, len(repositories))
	for i, repo := range repositories {
		entries[i] = RepositoryConfigEntry{
			Repository: repo.Repository.DeepCopy(),
		}
	}
	return entries
}

func ConvertToV1(config *Config) *v1.Config {
	return &v1.Config{
		Type:         config.Type,
		Repositories: convertToV1Repositories(config.Repositories),
		Consumers:    convertToV1Consumers(config.Consumers),
	}
}

func convertToV1Consumers(consumers []Consumer) []v1.Consumer {
	entries := make([]v1.Consumer, len(consumers))
	for i, consumer := range consumers {
		entries[i] = v1.Consumer{
			Identities:  deepCopyIdentities(consumer.Identities),
			Credentials: convertToV1Credentials(consumer.Credentials),
		}
	}
	return entries
}

func convertToV1Credentials(credentials []runtime.Typed) []*runtime.Raw {
	entries := make([]*runtime.Raw, len(credentials))
	for i, cred := range credentials {
		entries[i] = cred.(*runtime.Raw).DeepCopy()
	}
	return entries
}

func convertToV1Repositories(repositories []RepositoryConfigEntry) []v1.RepositoryConfigEntry {
	entries := make([]v1.RepositoryConfigEntry, len(repositories))
	for i, repo := range repositories {
		entries[i] = v1.RepositoryConfigEntry{
			Repository: repo.Repository.(*runtime.Raw).DeepCopy(),
		}
	}
	return entries
}
