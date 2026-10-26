package internal

import (
	"testing"

	"github.com/stretchr/testify/require"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestCheckSources(t *testing.T) {
	source := func(name, access string) descriptorv2.Source {
		src := descriptorv2.Source{Type: "blob"}
		src.Name, src.Version = name, "1.0.0"
		src.Access = &runtime.Raw{Type: runtime.NewUnversionedType(access), Data: []byte(`{"type":"` + access + `"}`)}
		return src
	}
	desc := func(sources ...descriptorv2.Source) *descriptorv2.Descriptor {
		d := &descriptorv2.Descriptor{}
		d.Component.Name, d.Component.Version = "ocm.software/c", "1.0.0"
		d.Component.Sources = sources
		return d
	}

	t.Run("external sources are copied as is", func(t *testing.T) {
		r := require.New(t)
		r.NoError(checkSources(t.Context(), desc(source("git", "Git/v1"))))
	})

	t.Run("no sources", func(t *testing.T) {
		r := require.New(t)
		r.NoError(checkSources(t.Context(), desc()))
	})

	t.Run("local blob sources fail before anything is uploaded", func(t *testing.T) {
		r := require.New(t)
		err := checkSources(t.Context(), desc(source("git", "Git/v1"), source("local", "LocalBlob/v1")))
		r.ErrorContains(err, `"local:1.0.0"`)
		r.ErrorContains(err, "cannot be transferred")
	})
}
