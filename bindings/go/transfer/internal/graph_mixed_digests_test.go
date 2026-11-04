package internal

import (
	"crypto"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/algorithms"
	jsonv1 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v1"
	v4alpha1 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha2"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha3"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

// A tree mixing reference digests of every verifiable normalisation (and
// legacy spellings) must transfer recursively: every digest is verified under
// its own algorithm and kept unchanged, and a tampered one still fails.
func TestBuildGraphDefinition_VerifiesMixedReferenceDigests(t *testing.T) {
	r := require.New(t)

	// A versioned signing label makes the v4 alphas disagree, so each digest is
	// only valid under its own algorithm.
	child := testDescriptor("ocm.software/child", "1.0.0", nil, nil)
	child.Component.Labels = []descriptor.Label{{Name: "sig", Value: []byte(`"v"`), Signing: true, Version: "v1"}}

	mint := func(norm, hash string) descriptor.Digest {
		d, err := algorithms.Digest(child, norm, hash)
		r.NoError(err)
		return *d
	}
	legacyV3 := mint(v4alpha2.Algorithm, crypto.SHA256.String())
	legacyV3.NormalisationAlgorithm = algorithms.LegacyV3
	legacySpelling := mint(v4alpha1.Algorithm, crypto.SHA256.String())
	legacySpelling.HashAlgorithm = "sha256"
	legacySpelling.Value = strings.ToUpper(legacySpelling.Value)

	digests := map[string]descriptor.Digest{
		"v1":              mint(jsonv1.Algorithm, crypto.SHA256.String()),
		"v3":              legacyV3,
		"v4alpha1":        mint(v4alpha1.Algorithm, crypto.SHA256.String()),
		"v4alpha2":        mint(v4alpha2.Algorithm, crypto.SHA256.String()),
		"v4alpha3":        mint(v4alpha3.Algorithm, crypto.SHA256.String()),
		"v4alpha3-sha512": mint(v4alpha3.Algorithm, crypto.SHA512.String()),
		"legacy-spelling": legacySpelling,
	}
	build := func(digests map[string]descriptor.Digest) (*descriptor.Descriptor, error) {
		var refs []descriptor.Reference
		for name, d := range digests {
			refs = append(refs, descriptor.Reference{
				ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: name, Version: "1.0.0"}},
				Component:   "ocm.software/child",
				Digest:      d,
			})
		}
		parent := testDescriptor("ocm.software/parent", "1.0.0", nil, refs)
		source := testOCIRepo("ghcr.io/source")
		resolver := testMultiResolver(map[string]struct {
			spec runtime.Typed
			desc *descriptor.Descriptor
		}{
			"ocm.software/parent:1.0.0": {spec: source, desc: parent},
			"ocm.software/child:1.0.0":  {spec: source, desc: child},
		})
		roots := testTransferRoots("ocm.software/parent", "1.0.0", testOCIRepo("ghcr.io/target"), resolver)
		tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{Recursive: transferv1alpha1.RecursiveInfinite}, nil)
		if err != nil {
			return nil, err
		}
		v2desc, err := descriptor.ConvertToV2(runtime.NewScheme(), parent)
		r.NoError(err)
		want, err := json.Marshal(v2desc.Component.References)
		r.NoError(err)
		for _, data := range tgd.Environment.Data {
			component := data.(map[string]any)["component"].(map[string]any)
			if component["name"] == "ocm.software/parent" {
				got, err := json.Marshal(component["componentReferences"])
				r.NoError(err)
				r.JSONEq(string(want), string(got), "reference digests must be transferred unchanged")
			}
		}
		return parent, nil
	}

	previous := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	t.Cleanup(func() { slog.SetDefault(previous) })

	_, err := build(digests)
	r.NoError(err)

	for name, d := range digests {
		tampered := map[string]descriptor.Digest{name: d}
		d.Value = strings.Repeat("0", len(d.Value))
		tampered[name] = d
		_, err := build(tampered)
		r.ErrorContains(err, "digest mismatch", "%s: a wrong digest must fail", name)
	}
}
