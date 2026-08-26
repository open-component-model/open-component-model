package runtime

import (
	"fmt"

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

func ConvertToV1(scheme *runtime.Scheme, config *Config) (*v1.Config, error) {
	repositories, err := convertToV1Repositories(scheme, config.Repositories)
	if err != nil {
		return nil, err
	}
	consumers, err := convertToV1Consumers(scheme, config.Consumers)
	if err != nil {
		return nil, err
	}
	return &v1.Config{
		Type:         config.Type,
		Repositories: repositories,
		Consumers:    consumers,
	}, nil
}

func convertToV1Consumers(scheme *runtime.Scheme, consumers []Consumer) ([]v1.Consumer, error) {
	entries := make([]v1.Consumer, len(consumers))
	for i, consumer := range consumers {
		credentials, err := convertToV1Credentials(scheme, consumer.Credentials)
		if err != nil {
			return nil, err
		}
		entries[i] = v1.Consumer{
			Identities:  deepCopyIdentities(consumer.Identities),
			Credentials: credentials,
		}
	}
	return entries, nil
}

func convertToV1Credentials(scheme *runtime.Scheme, credentials []runtime.Typed) ([]*runtime.Raw, error) {
	entries := make([]*runtime.Raw, len(credentials))
	for i, cred := range credentials {
		var raw runtime.Raw
		if err := scheme.Convert(cred, &raw); err != nil {
			return nil, fmt.Errorf("credential at index %d: %w", i, err)
		}
		entries[i] = &raw
	}
	return entries, nil
}

func convertToV1Repositories(scheme *runtime.Scheme, repositories []RepositoryConfigEntry) ([]v1.RepositoryConfigEntry, error) {
	entries := make([]v1.RepositoryConfigEntry, len(repositories))
	for i, repo := range repositories {
		var raw runtime.Raw
		if err := scheme.Convert(repo.Repository, &raw); err != nil {
			return nil, fmt.Errorf("repository at index %d: %w", i, err)
		}
		entries[i] = v1.RepositoryConfigEntry{
			Repository: &raw,
		}
	}
	return entries, nil
}
