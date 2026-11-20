package discovery

import (
	"crypto"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/signing"
)

const testNormalisationAlgorithm = signing.LegacyNormalisationAlgo

var testHashAlgorithm = crypto.SHA256.String()

// digestOf computes the descriptor digest the same way the controllers do, so
// the tests pin references with real values instead of hand-written ones.
func digestOf(t *testing.T, d *descriptor.Descriptor) descriptor.Digest {
	t.Helper()
	dig, err := signing.GenerateDigest(t.Context(), d, slog.Default(), testNormalisationAlgorithm, testHashAlgorithm)
	require.NoError(t, err)
	return *dig
}

func v2DigestOf(t *testing.T, d *descriptor.Descriptor) *v2.Digest {
	t.Helper()
	dig := digestOf(t, d)
	return &v2.Digest{
		HashAlgorithm:          dig.HashAlgorithm,
		NormalisationAlgorithm: dig.NormalisationAlgorithm,
		Value:                  dig.Value,
	}
}

// pinnedPair returns a child and a root whose reference entry pins the child's
// digest, in the state they would have after a traversal of untampered content.
func pinnedPair(t *testing.T) (root, child *descriptor.Descriptor, rootKey ComponentKey) {
	t.Helper()
	child = newDescriptor("example.com/child", "1.0.0", withResources(newResource("img")))
	root = newDescriptor("example.com/root", "1.0.0",
		withReferences(newReference("child", "example.com/child", "1.0.0")))
	root.Component.References[0].Digest = digestOf(t, child)
	return root, child, ComponentKey{Name: "example.com/root", Version: "1.0.0"}
}

func TestVerifyIntegrityChainIntact(t *testing.T) {
	r := require.New(t)
	root, child, rootKey := pinnedPair(t)

	err := VerifyIntegrity(t.Context(), &Graph{Descriptors: []*descriptor.Descriptor{root, child}}, rootKey, v2DigestOf(t, root))
	r.NoError(err)
}

func TestVerifyIntegrityTamperedChildFails(t *testing.T) {
	r := require.New(t)
	root, child, rootKey := pinnedPair(t)

	// Tamper with the child AFTER the root pinned its digest: the root still
	// matches its own digest, the pinned edge must not.
	child.Component.Resources = append(child.Component.Resources, newResource("smuggled"))

	err := VerifyIntegrity(t.Context(), &Graph{Descriptors: []*descriptor.Descriptor{root, child}}, rootKey, v2DigestOf(t, root))
	r.Error(err)
	r.ErrorContains(err, "example.com/child")
	r.ErrorContains(err, "digest mismatch")
}

func TestVerifyIntegrityTamperedRootFails(t *testing.T) {
	r := require.New(t)
	root, child, rootKey := pinnedPair(t)

	rootDigest := v2DigestOf(t, root)
	// The Component resource reports a different digest than the descriptor
	// the traversal fetched: the root anchor must not match.
	root.Component.Resources = append(root.Component.Resources, newResource("smuggled"))

	err := VerifyIntegrity(t.Context(), &Graph{Descriptors: []*descriptor.Descriptor{root, child}}, rootKey, rootDigest)
	r.Error(err)
	r.ErrorContains(err, "example.com/root")
	r.ErrorContains(err, "digest mismatch")
}

func TestVerifyIntegrityNilRootDigestStillChecksPinnedEdges(t *testing.T) {
	r := require.New(t)
	root, child, rootKey := pinnedPair(t)
	child.Component.Resources = append(child.Component.Resources, newResource("smuggled"))

	err := VerifyIntegrity(t.Context(), &Graph{Descriptors: []*descriptor.Descriptor{root, child}}, rootKey, nil)
	r.Error(err)
	r.ErrorContains(err, "example.com/child")
}

func TestVerifyIntegrityUnpinnedEdgeIsSkipped(t *testing.T) {
	r := require.New(t)
	child := newDescriptor("example.com/child", "1.0.0", withResources(newResource("img")))
	root := newDescriptor("example.com/root", "1.0.0",
		withReferences(newReference("child", "example.com/child", "1.0.0")))
	rootKey := ComponentKey{Name: "example.com/root", Version: "1.0.0"}

	// No reference digest and no root digest: nothing is pinned, nothing fails.
	// The unanchored state stays visible through Graph.DigestsComplete.
	g := &Graph{Descriptors: []*descriptor.Descriptor{root, child}}
	err := VerifyIntegrity(t.Context(), g, rootKey, nil)
	r.NoError(err)
	r.False(g.DigestsComplete())
}

func TestVerifyIntegrityPinnedChildMissingFromGraphFails(t *testing.T) {
	r := require.New(t)
	root, _, rootKey := pinnedPair(t)

	err := VerifyIntegrity(t.Context(), &Graph{Descriptors: []*descriptor.Descriptor{root}}, rootKey, nil)
	r.Error(err)
	r.ErrorContains(err, "not in the resolved graph")
}
