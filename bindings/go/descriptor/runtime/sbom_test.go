package runtime_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// sbomResource builds a resource of type "sbom" carrying a ocm.software/sbom
// label that references the given resource identities.
func sbomResource(t *testing.T, name string, refs ...runtime.Identity) descriptor.Resource {
	t.Helper()
	references := make([]descriptor.SBOMReference, 0, len(refs))
	for _, id := range refs {
		references = append(references, descriptor.SBOMReference{Resource: id})
	}
	label := descriptor.Label{Name: descriptor.LabelSBOM, Version: "v1"}
	require.NoError(t, label.SetValue(descriptor.SBOMLabelValue{References: references}))
	res := descriptor.Resource{Type: descriptor.ResourceTypeSBOM}
	res.Name = name
	res.Labels = []descriptor.Label{label}
	return res
}

func namedResource(name, typ string) descriptor.Resource {
	res := descriptor.Resource{Type: typ}
	res.Name = name
	return res
}

func descWith(resources ...descriptor.Resource) *descriptor.Descriptor {
	d := &descriptor.Descriptor{}
	d.Component.Resources = resources
	return d
}

func TestFindSBOMResources(t *testing.T) {
	cli := namedResource("cli", "executable")
	cliSBOM := sbomResource(t, "cli-sbom", runtime.Identity{"name": "cli"})

	t.Run("matches by partial identity (name only)", func(t *testing.T) {
		desc := descWith(cli, cliSBOM)
		got, err := descriptor.FindSBOMResources(desc, runtime.Identity{"name": "cli"})
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "cli-sbom", got[0].Name)
	})

	t.Run("matches full identity incl. extra identity", func(t *testing.T) {
		desc := descWith(cli, cliSBOM)
		target := runtime.Identity{"name": "cli", "version": "0.11.0", "os": "linux", "architecture": "amd64"}
		got, err := descriptor.FindSBOMResources(desc, target)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "cli-sbom", got[0].Name)
	})

	t.Run("no match returns empty slice and nil error", func(t *testing.T) {
		desc := descWith(cli, cliSBOM)
		got, err := descriptor.FindSBOMResources(desc, runtime.Identity{"name": "other"})
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("ignores non-sbom resource types even with the label", func(t *testing.T) {
		mislabeled := sbomResource(t, "not-really", runtime.Identity{"name": "cli"})
		mislabeled.Type = "executable"
		desc := descWith(cli, mislabeled)
		got, err := descriptor.FindSBOMResources(desc, runtime.Identity{"name": "cli"})
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("returns all matching sbom resources in descriptor order", func(t *testing.T) {
		spdx := sbomResource(t, "cli-sbom-spdx", runtime.Identity{"name": "cli"})
		cyclonedx := sbomResource(t, "cli-sbom-cyclonedx", runtime.Identity{"name": "cli"})
		desc := descWith(cli, spdx, cyclonedx)
		got, err := descriptor.FindSBOMResources(desc, runtime.Identity{"name": "cli"})
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, "cli-sbom-spdx", got[0].Name)
		assert.Equal(t, "cli-sbom-cyclonedx", got[1].Name)
	})

	t.Run("sbom resource without the label is skipped", func(t *testing.T) {
		bare := namedResource("bare-sbom", descriptor.ResourceTypeSBOM)
		desc := descWith(cli, bare)
		got, err := descriptor.FindSBOMResources(desc, runtime.Identity{"name": "cli"})
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("malformed label value returns an error", func(t *testing.T) {
		bad := namedResource("bad-sbom", descriptor.ResourceTypeSBOM)
		bad.Labels = []descriptor.Label{{Name: descriptor.LabelSBOM, Value: []byte(`"not-an-object"`)}}
		desc := descWith(cli, bad)
		_, err := descriptor.FindSBOMResources(desc, runtime.Identity{"name": "cli"})
		require.Error(t, err)
	})

	t.Run("nil descriptor returns nil", func(t *testing.T) {
		got, err := descriptor.FindSBOMResources(nil, runtime.Identity{"name": "cli"})
		require.NoError(t, err)
		assert.Nil(t, got)
	})
}

func TestElementMetaGetLabel(t *testing.T) {
	res := sbomResource(t, "cli-sbom", runtime.Identity{"name": "cli"})

	t.Run("present", func(t *testing.T) {
		got := res.GetLabel(descriptor.LabelSBOM)
		require.NotNil(t, got)
		assert.Equal(t, descriptor.LabelSBOM, got.Name)
	})

	t.Run("absent", func(t *testing.T) {
		assert.Nil(t, res.GetLabel("does-not-exist"))
	})

	t.Run("nil receiver", func(t *testing.T) {
		var m *descriptor.ElementMeta
		assert.Nil(t, m.GetLabel(descriptor.LabelSBOM))
	})
}
