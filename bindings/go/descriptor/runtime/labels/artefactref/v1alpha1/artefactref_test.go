package v1alpha1_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sigs.k8s.io/yaml"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	artefactref "ocm.software/open-component-model/bindings/go/descriptor/runtime/labels/artefactref/v1alpha1"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func referenceLabel(t *testing.T, identities ...runtime.Identity) descriptor.Label {
	t.Helper()
	refs := make(artefactref.References, 0, len(identities))
	for _, identity := range identities {
		refs = append(refs, artefactref.Reference{Identity: identity})
	}
	raw, err := json.Marshal(refs)
	require.NoError(t, err)
	return descriptor.Label{
		Name:    artefactref.LabelName,
		Version: artefactref.Version,
		Value:   raw,
		Signing: true,
	}
}

func TestFromLabels(t *testing.T) {
	t.Run("decodes the documented yaml shape", func(t *testing.T) {
		label := descriptor.Label{
			Name:    artefactref.LabelName,
			Version: artefactref.Version,
			Value:   json.RawMessage(`[{"identity":{"name":"cli","version":"1.0.0"}}]`),
			Signing: true,
		}

		refs, ok, err := artefactref.FromLabels([]descriptor.Label{label})
		require.NoError(t, err)
		require.True(t, ok)
		require.Len(t, refs, 1)
		assert.Equal(t, runtime.Identity{"name": "cli", "version": "1.0.0"}, refs[0].Identity)
	})

	t.Run("decodes every entry of the list", func(t *testing.T) {
		label := descriptor.Label{
			Name:    artefactref.LabelName,
			Version: artefactref.Version,
			Value:   json.RawMessage(`[{"identity":{"name":"cli"}},{"identity":{"name":"server","version":"2.0.0"}}]`),
		}

		refs, ok, err := artefactref.FromLabels([]descriptor.Label{label})
		require.NoError(t, err)
		require.True(t, ok)
		require.Len(t, refs, 2)
		assert.Equal(t, runtime.Identity{"name": "cli"}, refs[0].Identity)
		assert.Equal(t, runtime.Identity{"name": "server", "version": "2.0.0"}, refs[1].Identity)
	})

	t.Run("reports absence without an error", func(t *testing.T) {
		refs, ok, err := artefactref.FromLabels([]descriptor.Label{{Name: "unrelated"}})
		require.NoError(t, err)
		assert.False(t, ok)
		assert.Empty(t, refs)
	})

	t.Run("distinguishes an empty list from an absent label", func(t *testing.T) {
		label := descriptor.Label{Name: artefactref.LabelName, Version: artefactref.Version, Value: json.RawMessage(`[]`)}
		refs, ok, err := artefactref.FromLabels([]descriptor.Label{label})
		require.NoError(t, err)
		assert.True(t, ok, "label is present even though it selects nothing")
		assert.Empty(t, refs)
	})

	t.Run("passes over a label of another version", func(t *testing.T) {
		for _, version := range []string{"v1", "v2", "v1beta1", "v2alpha1"} {
			label := descriptor.Label{
				Name:    artefactref.LabelName,
				Version: version,
				Value:   json.RawMessage(`[{"identity":{"name":"cli"}}]`),
			}
			refs, ok, err := artefactref.FromLabels([]descriptor.Label{label})
			require.NoError(t, err, version)
			assert.False(t, ok, "version %q must not be treated as conforming", version)
			assert.Empty(t, refs)
		}
	})

	t.Run("accepts a label carrying no version", func(t *testing.T) {
		label := descriptor.Label{Name: artefactref.LabelName, Value: json.RawMessage(`[{"identity":{"name":"cli"}}]`)}
		refs, ok, err := artefactref.FromLabels([]descriptor.Label{label})
		require.NoError(t, err)
		require.True(t, ok)
		require.Len(t, refs, 1)
		assert.Equal(t, "cli", refs[0].Identity["name"])
	})

	t.Run("finds the conforming label among several versions", func(t *testing.T) {
		labels := []descriptor.Label{
			{Name: artefactref.LabelName, Version: "v2", Value: json.RawMessage(`[{"identity":{"name":"future"}}]`)},
			{Name: artefactref.LabelName, Version: artefactref.Version, Value: json.RawMessage(`[{"identity":{"name":"cli"}}]`)},
		}
		refs, ok, err := artefactref.FromLabels(labels)
		require.NoError(t, err)
		require.True(t, ok)
		require.Len(t, refs, 1)
		assert.Equal(t, "cli", refs[0].Identity["name"])
	})

	t.Run("rejects a value that is not a list", func(t *testing.T) {
		label := descriptor.Label{Name: artefactref.LabelName, Version: artefactref.Version, Value: json.RawMessage(`{"identity":{"name":"cli"}}`)}
		_, _, err := artefactref.FromLabels([]descriptor.Label{label})
		require.ErrorContains(t, err, artefactref.LabelName)
	})
}

func TestReferences_Describes(t *testing.T) {
	subject := runtime.Identity{"name": "my-image", "version": "1.2.3"}

	t.Run("matches when any entry describes the subject", func(t *testing.T) {
		refs := artefactref.References{
			{Identity: runtime.Identity{"name": "other"}},
			{Identity: runtime.Identity{"name": "my-image"}},
		}
		assert.True(t, refs.Describes(subject))
	})

	t.Run("does not match when no entry describes the subject", func(t *testing.T) {
		refs := artefactref.References{
			{Identity: runtime.Identity{"name": "other"}},
			{Identity: runtime.Identity{"name": "my-image", "version": "9.9.9"}},
		}
		assert.False(t, refs.Describes(subject))
	})

	t.Run("an empty list selects nothing", func(t *testing.T) {
		assert.False(t, artefactref.References{}.Describes(subject))
		assert.False(t, artefactref.References(nil).Describes(subject))
	})
}

func TestReference_Describes(t *testing.T) {
	subject := runtime.Identity{"name": "my-image", "version": "1.2.3", "foo": "bar"}

	for _, tc := range []struct {
		name     string
		identity runtime.Identity
		expected bool
	}{
		{
			name:     "fully qualified reference matches",
			identity: runtime.Identity{"name": "my-image", "version": "1.2.3", "foo": "bar"},
			expected: true,
		},
		{
			name:     "omitted version matches any version",
			identity: runtime.Identity{"name": "my-image", "foo": "bar"},
			expected: true,
		},
		{
			name:     "wrong version does not match",
			identity: runtime.Identity{"name": "my-image", "version": "2.0.0", "foo": "bar"},
			expected: false,
		},
		{
			name:     "wrong name does not match",
			identity: runtime.Identity{"name": "other", "foo": "bar"},
			expected: false,
		},
		{
			name:     "missing name does not match",
			identity: runtime.Identity{"foo": "bar"},
			expected: false,
		},
		{
			name:     "extra identity the subject does not have does not match",
			identity: runtime.Identity{"name": "my-image", "version": "1.2.3", "foo": "bar", "os": "linux"},
			expected: false,
		},
		{
			name:     "extra identity left out of the reference does not match",
			identity: runtime.Identity{"name": "my-image", "version": "1.2.3"},
			expected: false,
		},
		{
			name:     "differing extra identity value does not match",
			identity: runtime.Identity{"name": "my-image", "version": "1.2.3", "foo": "other"},
			expected: false,
		},
		{
			name:     "nil reference selects nothing",
			identity: nil,
			expected: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ref := artefactref.Reference{Identity: tc.identity}
			assert.Equal(t, tc.expected, ref.Describes(subject))
		})
	}

	t.Run("a subject without extra identity is matched by name alone", func(t *testing.T) {
		plain := runtime.Identity{"name": "my-image", "version": "1.2.3"}
		ref := artefactref.Reference{Identity: runtime.Identity{"name": "my-image"}}
		assert.True(t, ref.Describes(plain))
	})
}

func TestFindDescribingResources(t *testing.T) {
	image := descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta:    descriptor.ObjectMeta{Name: "my-image", Version: "1.2.3"},
			ExtraIdentity: runtime.Identity{"foo": "bar"},
		},
		Type: "ociImage",
	}

	newDescriptor := func(resources ...descriptor.Resource) *descriptor.Descriptor {
		return &descriptor.Descriptor{Component: descriptor.Component{Resources: resources}}
	}

	sbom := func(name string, extraIdentity runtime.Identity, selector runtime.Identity) descriptor.Resource {
		return descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    name,
					Version: "1.2.3",
					Labels:  []descriptor.Label{referenceLabel(t, selector)},
				},
				ExtraIdentity: extraIdentity,
			},
			Type: "sbom",
		}
	}

	selector := runtime.Identity{"name": "my-image", "version": "1.2.3", "foo": "bar"}

	t.Run("finds the single describing resource", func(t *testing.T) {
		desc := newDescriptor(image, sbom("my-image-sbom", nil, selector))

		found, err := artefactref.FindDescribingResources(desc, image.ToIdentity())
		require.NoError(t, err)
		require.Len(t, found, 1)
		assert.Equal(t, "my-image-sbom", found[0].Name)
	})

	t.Run("finds one describing resource per platform", func(t *testing.T) {
		desc := newDescriptor(
			image,
			sbom("my-image-sbom", runtime.Identity{"architecture": "amd64"}, selector),
			sbom("my-image-sbom", runtime.Identity{"architecture": "arm64"}, selector),
		)

		found, err := artefactref.FindDescribingResources(desc, image.ToIdentity())
		require.NoError(t, err)
		require.Len(t, found, 2, "choosing between platforms is the caller's job, not this function's")
		assert.Equal(t, "amd64", found[0].ExtraIdentity["architecture"])
		assert.Equal(t, "arm64", found[1].ExtraIdentity["architecture"])
	})

	t.Run("reports not found when no resource describes the target", func(t *testing.T) {
		desc := newDescriptor(image, sbom("other-sbom", nil, runtime.Identity{"name": "other"}))

		_, err := artefactref.FindDescribingResources(desc, image.ToIdentity())
		require.ErrorIs(t, err, artefactref.ErrNotFound)
		assert.ErrorContains(t, err, "my-image", "the message has to name what was looked for")
	})

	t.Run("finds a resource whose label lists the target among several subjects", func(t *testing.T) {
		multi := descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "combined-sbom",
					Version: "1.2.3",
					Labels: []descriptor.Label{referenceLabel(t,
						runtime.Identity{"name": "other-image"},
						selector,
					)},
				},
			},
			Type: "sbom",
		}
		desc := newDescriptor(image, multi)

		found, err := artefactref.FindDescribingResources(desc, image.ToIdentity())
		require.NoError(t, err)
		require.Len(t, found, 1)
		assert.Equal(t, "combined-sbom", found[0].Name)
	})

	t.Run("reports not found when no resource carries the label", func(t *testing.T) {
		_, err := artefactref.FindDescribingResources(newDescriptor(image), image.ToIdentity())
		require.ErrorIs(t, err, artefactref.ErrNotFound)
	})

	t.Run("propagates a malformed label", func(t *testing.T) {
		broken := descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "broken-sbom",
					Version: "1.2.3",
					Labels: []descriptor.Label{{
						Name:    artefactref.LabelName,
						Version: artefactref.Version,
						Value:   json.RawMessage(`"not-a-list"`),
					}},
				},
			},
		}

		_, err := artefactref.FindDescribingResources(newDescriptor(image, broken), image.ToIdentity())
		require.ErrorContains(t, err, "broken-sbom")
	})
}

// TestFindDescribingResources_SpecExample runs the yaml the specification documents,
// end to end through the v2 decoding path, so the shape this package accepts stays the
// shape the specification writes.
// https://github.com/open-component-model/ocm-spec/blob/main/doc/01-model/06-conventions.md#artefact-linking-label
func TestFindDescribingResources_SpecExample(t *testing.T) {
	const componentDescriptor = `
meta:
  schemaVersion: v2
component:
  name: ocm.software/test
  version: 1.2.3
  provider: ocm.software
  resources:
    - name: my-image
      version: 1.2.3
      type: ociImage
      relation: local
      access:
        type: localBlob/v1
      extraIdentity:
        foo: bar
    - name: my-image-sbom
      version: 1.2.3
      type: sbom
      relation: local
      access:
        type: localBlob/v1
      extraIdentity:
        architecture: amd64
      labels:
        - name: ocm.software/artefact-references
          version: v1alpha1
          value:
            - identity:
                name: my-image
                version: 1.2.3
                foo: bar
`

	var v2Descriptor v2.Descriptor
	require.NoError(t, yaml.Unmarshal([]byte(componentDescriptor), &v2Descriptor))
	desc, err := descriptor.ConvertFromV2(&v2Descriptor)
	require.NoError(t, err)

	image := desc.Component.Resources[0]
	found, err := artefactref.FindDescribingResources(desc, image.ToIdentity())
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.Equal(t, "my-image-sbom", found[0].Name)
}
