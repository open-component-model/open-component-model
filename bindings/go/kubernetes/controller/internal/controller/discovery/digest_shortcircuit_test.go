package discovery

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	desc "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/repository"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

// ctfRepoAt builds a CTF under dir so the caller can delete it mid-test and
// prove whether a later reconcile touched the repository at all.
func ctfRepoAt(t *testing.T, dir string, descs ...*desc.Descriptor) []byte {
	t.Helper()
	r := require.New(t)

	spec := &ctf.Repository{
		Type:       ocmruntime.Type{Version: "v1", Name: "ctf"},
		FilePath:   dir,
		AccessMode: ctf.AccessModeReadWrite,
	}
	repo, err := ocirepository.NewFromCTFRepoV1(t.Context(), spec)
	r.NoError(err)
	for _, d := range descs {
		r.NoError(repo.AddComponentVersion(t.Context(), d))
	}
	spec.AccessMode = ctf.AccessModeReadOnly
	raw, err := json.Marshal(spec)
	r.NoError(err)

	return raw
}

func digestedRef(name, component, version, value string) desc.Reference {
	ref := makeRef(name, component, version)
	ref.Digest = desc.Digest{
		HashAlgorithm:          "SHA-256",
		NormalisationAlgorithm: "jsonNormalisation/v4alpha1",
		Value:                  value,
	}

	return ref
}

func componentWithDigest(name, namespace, component, version string, repoSpec []byte, value string) *v1alpha1.Component {
	c := readyComponentWithSpec(name, namespace, component, version, repoSpec)
	if value != "" {
		c.Status.Component.Digest = &v2.Digest{
			HashAlgorithm:          "SHA-256",
			NormalisationAlgorithm: "jsonNormalisation/v4alpha1",
			Value:                  value,
		}
	}

	return c
}

func discoveryFor(componentName, namespace string) *v1alpha1.Discovery {
	return &v1alpha1.Discovery{
		ObjectMeta: metav1.ObjectMeta{Name: "discovery", Namespace: namespace},
		Spec: v1alpha1.DiscoverySpec{
			ComponentRef: corev1.LocalObjectReference{Name: componentName},
		},
	}
}

// TestReconcile_UnchangedRootDigestSkipsTraversal proves the skip by deleting
// the repository between reconciles: a second reconcile that still succeeds
// cannot have fetched anything.
func TestReconcile_UnchangedRootDigestSkipsTraversal(t *testing.T) {
	r := require.New(t)
	const ns = "default"

	dir := t.TempDir()
	child := makeDescriptor("ocm.software/child", "1.0.0")
	root := makeDescriptor("ocm.software/root", "1.0.0",
		digestedRef("child", "ocm.software/child", "1.0.0", "childdigest"))
	repoSpec := ctfRepoAt(t, dir, root, child)

	component := componentWithDigest("component", ns, root.Component.Name, root.Component.Version, repoSpec, "rootdigest")
	discovery := discoveryFor(component.Name, ns)
	rec, c := realPluginReconciler(t, component, discovery)
	key := client.ObjectKeyFromObject(discovery)

	_, err := reconcileUntilSettled(t, rec, key)
	r.NoError(err)

	current := &v1alpha1.Discovery{}
	r.NoError(c.Get(t.Context(), key, current))
	r.ElementsMatch([]string{"ocm.software/root", "ocm.software/child"}, componentNamesFromStatus(t, current))
	r.NotEmpty(current.Status.ObservedComponentDigest, "a fully digested graph must record the root digest")

	// Remove the repository. Any traversal from here on must fail.
	r.NoError(os.RemoveAll(dir))

	_, err = rec.Reconcile(t.Context(), ctrl.Request{NamespacedName: key})
	r.NoError(err, "an unchanged root digest must not re-traverse")

	after := &v1alpha1.Discovery{}
	r.NoError(c.Get(t.Context(), key, after))
	r.ElementsMatch([]string{"ocm.software/root", "ocm.software/child"}, componentNamesFromStatus(t, after))

	// Control: a changed root digest must re-traverse, and now fail.
	fresh := &v1alpha1.Component{}
	r.NoError(c.Get(t.Context(), client.ObjectKey{Namespace: ns, Name: component.Name}, fresh))
	fresh.Status.Component.Digest.Value = "rootdigest-changed"
	r.NoError(c.Status().Update(t.Context(), fresh))

	_, err = rec.Reconcile(t.Context(), ctrl.Request{NamespacedName: key})
	r.Error(err, "a changed root digest must re-traverse and hit the deleted repository")
}

// TestReconcile_MissingReferenceDigestForcesFullTraversal covers the trust rule:
// GenerateDigest does not enforce IsSafelyDigestible and v2.Reference.Digest is
// omitempty, so an undigested reference is silently absent from the root's
// digest input. One missing digest therefore invalidates the whole chain.
func TestReconcile_MissingReferenceDigestForcesFullTraversal(t *testing.T) {
	r := require.New(t)
	const ns = "default"

	dir := t.TempDir()
	child := makeDescriptor("ocm.software/child", "1.0.0")
	root := makeDescriptor("ocm.software/root", "1.0.0",
		makeRef("child", "ocm.software/child", "1.0.0")) // no digest
	repoSpec := ctfRepoAt(t, dir, root, child)

	component := componentWithDigest("component", ns, root.Component.Name, root.Component.Version, repoSpec, "rootdigest")
	discovery := discoveryFor(component.Name, ns)
	rec, c := realPluginReconciler(t, component, discovery)
	key := client.ObjectKeyFromObject(discovery)

	_, err := reconcileUntilSettled(t, rec, key)
	r.NoError(err)

	current := &v1alpha1.Discovery{}
	r.NoError(c.Get(t.Context(), key, current))
	r.Empty(current.Status.ObservedComponentDigest, "a broken digest chain must not be recorded")

	r.NoError(os.RemoveAll(dir))

	_, err = rec.Reconcile(t.Context(), ctrl.Request{NamespacedName: key})
	r.Error(err, "without a trusted digest the traversal must run and fail")
}
