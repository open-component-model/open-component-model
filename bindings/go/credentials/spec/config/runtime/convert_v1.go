package runtime

import (
	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func ConvertFromV1(config *v1.Config) *Config {
	return &Config{
		Type:         config.Type,
		Repositories: convertRepositories(config.Repositories),
		Consumers:    convertConsumers(config.Consumers),
	}
}

func convertConsumers(consumers []v1.Consumer) []Consumer {
	entries := make([]Consumer, 0, len(consumers))
	for _, consumer := range consumers {
		entries = append(entries, Consumer{
			Identities:  consumer.Identities,
			Credentials: convertCredentials(consumer.Credentials),
		})
	}
	return entries
}

func convertCredentials(credentials []*runtime.Raw) []runtime.Typed {
	entries := make([]runtime.Typed, 0, len(credentials))
	for _, cred := range credentials {
		entries = append(entries, cred.DeepCopy())
	}
	return entries
}

func convertRepositories(repositories []v1.RepositoryConfigEntry) []RepositoryConfigEntry {
	entries := make([]RepositoryConfigEntry, 0, len(repositories))
	for _, repo := range repositories {
		entries = append(entries, RepositoryConfigEntry{
			Repository: repo.Repository.DeepCopy(),
		})
	}
	return entries
}
