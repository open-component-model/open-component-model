package repository_test

import (
	"fmt"
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

func TestPlatform_String(t *testing.T) {
	for _, tc := range []struct {
		platform repository.Platform
		want     string
	}{
		{repository.Platform{OS: "linux", Architecture: "arm64"}, "linux/arm64"},
		{repository.Platform{OS: "linux", Architecture: "arm", Variant: "v7"}, "linux/arm/v7"},
		{repository.Platform{Architecture: "amd64"}, "amd64"},
		{repository.Platform{}, ""},
	} {
		assert.Equal(t, tc.want, tc.platform.String())
	}
}

func TestSBOM_String(t *testing.T) {
	document := []byte(`{"spdxVersion":"SPDX-2.3","packages":[{"name":"secret"}]}`)

	t.Run("never renders the document itself", func(t *testing.T) {
		sbom := repository.SBOM{Name: "sbom", ID: "sha256:abc", Data: document}
		assert.NotContains(t, fmt.Sprintf("%v %q %s", sbom, sbom, sbom), "secret",
			"formatting an sbom must not spill it into a log line or an error")
	})

	t.Run("names the document, with its platform when it has one", func(t *testing.T) {
		assert.Equal(t, "sbom (linux/arm64)", repository.SBOM{
			Name:     "sbom",
			Platform: repository.Platform{OS: "linux", Architecture: "arm64"},
		}.String())
	})

	t.Run("falls back to the id, then to a placeholder", func(t *testing.T) {
		assert.Equal(t, "sha256:abc", repository.SBOM{ID: "sha256:abc"}.String())
		assert.Equal(t, "unnamed sbom", repository.SBOM{Data: document}.String())
	})
}
