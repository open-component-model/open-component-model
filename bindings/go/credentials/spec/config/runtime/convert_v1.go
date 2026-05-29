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
	entries := make([]Consumer, len(consumers))
	for i, consumer := range consumers {
		entries[i] = Consumer{
			Identities:  convertIdentities(consumer.Identities),
			Credentials: convertCredentials(consumer.Credentials),
		}
	}
	return entries
}

// legacyConsumerIdentityTypes maps deprecated consumer identity type names to
// their current equivalents. Normalization happens at config-load time so that
// credentials stored under an old type name are still found when the runtime
// looks them up under the new name.
//
// History:
//   - "OCIRepository" was renamed to "OCIRegistry" in
//     https://github.com/open-component-model/open-component-model/pull/1964
var legacyConsumerIdentityTypes = map[string]string{
	"OCIRepository": "OCIRegistry",
}

func convertIdentities(identities []runtime.Identity) []runtime.Identity {
	nidentities := make([]runtime.Identity, len(identities))
	for i, identity := range identities {
		id := identity.DeepCopy()
		if typ, ok := id[runtime.IdentityAttributeType]; ok {
			if normalized, ok := legacyConsumerIdentityTypes[typ]; ok {
				id[runtime.IdentityAttributeType] = normalized
			}
		}
		nidentities[i] = id
	}
	return nidentities
}

func convertCredentials(credentials []*runtime.Raw) []runtime.Typed {
	entries := make([]runtime.Typed, len(credentials))
	for i, cred := range credentials {
		entries[i] = cred.DeepCopy()
	}
	return entries
}

func convertRepositories(repositories []v1.RepositoryConfigEntry) []RepositoryConfigEntry {
	entries := make([]RepositoryConfigEntry, len(repositories))
	for i, repo := range repositories {
		entries[i] = RepositoryConfigEntry{
			Repository: repo.Repository.DeepCopy(),
		}
	}
	return entries
}
