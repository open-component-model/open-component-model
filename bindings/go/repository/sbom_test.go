package repository_test

import (
	goruntime "runtime"
	"testing"

	"github.com/stretchr/testify/assert"

	"ocm.software/open-component-model/bindings/go/repository"
)

func TestNewSBOMOptions(t *testing.T) {
	t.Run("defaults to the architecture of the running process but not its os", func(t *testing.T) {
		o := repository.NewSBOMOptions()
		assert.Equal(t, goruntime.GOARCH, o.Platform.Architecture)
		assert.Empty(t, o.Platform.OS, "defaulting the os would hide artifacts built for another one")
		assert.Equal(t, []string{repository.PredicateTypeSPDX}, o.PredicateTypes)
		assert.False(t, o.AllPlatforms)
	})

	t.Run("a platform refines the current value attribute by attribute", func(t *testing.T) {
		o := repository.NewSBOMOptions(
			repository.WithSBOMPlatform(repository.Platform{OS: "linux", Architecture: "amd64"}),
			repository.WithSBOMPlatform(repository.Platform{Architecture: "arm64"}))
		assert.Equal(t, repository.Platform{OS: "linux", Architecture: "arm64"}, o.Platform)
	})

	t.Run("all platforms is independent of a requested platform", func(t *testing.T) {
		o := repository.NewSBOMOptions(
			repository.WithSBOMPlatform(repository.Platform{OS: "linux"}),
			repository.WithAllSBOMPlatforms())
		assert.True(t, o.AllPlatforms)
		assert.Equal(t, "linux", o.Platform.OS, "the platform is kept, honouring it is up to the implementation")
	})

	t.Run("an empty set of predicate types keeps the default", func(t *testing.T) {
		o := repository.NewSBOMOptions(repository.WithSBOMPredicateTypes())
		assert.Equal(t, []string{repository.PredicateTypeSPDX}, o.PredicateTypes)
	})

	t.Run("predicate types replace the default", func(t *testing.T) {
		o := repository.NewSBOMOptions(repository.WithSBOMPredicateTypes(repository.PredicateTypeCycloneDX))
		assert.Equal(t, []string{repository.PredicateTypeCycloneDX}, o.PredicateTypes)
	})
}

func TestSBOM_MediaType(t *testing.T) {
	for predicateType, want := range map[string]string{
		repository.PredicateTypeSPDX:      repository.MediaTypeSPDXJSON,
		repository.PredicateTypeCycloneDX: repository.MediaTypeCycloneDXJSON,
		"":                                "application/json",
		"https://slsa.dev/provenance/v1":  "application/json",
	} {
		assert.Equal(t, want, repository.SBOM{PredicateType: predicateType}.MediaType(), predicateType)
	}
}
