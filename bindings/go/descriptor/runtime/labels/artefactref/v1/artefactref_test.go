package v1_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	artefactref "ocm.software/open-component-model/bindings/go/descriptor/runtime/labels/artefactref/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func referenceLabel(t *testing.T, identity runtime.Identity) descriptor.Label {
	t.Helper()
	raw, err := json.Marshal(artefactref.Reference{Identity: identity})
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
			Value:   json.RawMessage(`{"identity":{"name":"cli","version":"1.0.0"}}`),
			Signing: true,
		}

		ref, ok, err := artefactref.FromLabels([]descriptor.Label{label})
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, runtime.Identity{"name": "cli", "version": "1.0.0"}, ref.Identity)
	})

	t.Run("reports absence without an error", func(t *testing.T) {
		ref, ok, err := artefactref.FromLabels([]descriptor.Label{{Name: "unrelated"}})
		require.NoError(t, err)
		assert.False(t, ok)
		assert.Empty(t, ref.Identity)
	})

	t.Run("distinguishes an empty reference from an absent label", func(t *testing.T) {
		label := descriptor.Label{Name: artefactref.LabelName, Version: artefactref.Version, Value: json.RawMessage(`{}`)}
		ref, ok, err := artefactref.FromLabels([]descriptor.Label{label})
		require.NoError(t, err)
		assert.True(t, ok, "label is present even though it selects nothing")
		assert.Empty(t, ref.Identity)
	})

	t.Run("passes over a label of another version", func(t *testing.T) {
		for _, version := range []string{"v2", "v1beta1", "v2alpha1"} {
			label := descriptor.Label{
				Name:    artefactref.LabelName,
				Version: version,
				Value:   json.RawMessage(`{"identity":{"name":"cli"}}`),
			}
			ref, ok, err := artefactref.FromLabels([]descriptor.Label{label})
			require.NoError(t, err, version)
			assert.False(t, ok, "version %q must not be treated as conforming", version)
			assert.Empty(t, ref.Identity)
		}
	})

	t.Run("accepts a label carrying no version", func(t *testing.T) {
		label := descriptor.Label{Name: artefactref.LabelName, Value: json.RawMessage(`{"identity":{"name":"cli"}}`)}
		ref, ok, err := artefactref.FromLabels([]descriptor.Label{label})
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, "cli", ref.Identity["name"])
	})

	t.Run("finds the conforming label among several versions", func(t *testing.T) {
		labels := []descriptor.Label{
			{Name: artefactref.LabelName, Version: "v2", Value: json.RawMessage(`{"identity":{"name":"future"}}`)},
			{Name: artefactref.LabelName, Version: artefactref.Version, Value: json.RawMessage(`{"identity":{"name":"cli"}}`)},
		}
		ref, ok, err := artefactref.FromLabels(labels)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, "cli", ref.Identity["name"])
	})

	t.Run("rejects a malformed value", func(t *testing.T) {
		label := descriptor.Label{Name: artefactref.LabelName, Version: artefactref.Version, Value: json.RawMessage(`[{"identity":{"name":"cli"}}]`)}
		_, _, err := artefactref.FromLabels([]descriptor.Label{label})
		require.ErrorContains(t, err, artefactref.LabelName)
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
