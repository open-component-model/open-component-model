package discovery

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	desc "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/setup"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/status"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/repository"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

// ctfRepo builds a read-write CTF repository at a fresh temp dir, adds the
// given descriptors, and returns the raw read-only repository spec JSON.
func ctfRepo(t *testing.T, descs ...*desc.Descriptor) []byte {
	t.Helper()
	r := require.New(t)

	spec := &ctf.Repository{
		Type:       ocmruntime.Type{Version: "v1", Name: "ctf"},
		FilePath:   t.TempDir(),
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

// makeDescriptor builds a v2 descriptor with the given references.
func makeDescriptor(name, version string, refs ...desc.Reference) *desc.Descriptor {
	d := &desc.Descriptor{}
	d.Meta.Version = "v2"
	d.Component.Name = name
	d.Component.Version = version
	d.Component.Provider.Name = "ocm.software"
	d.Component.References = refs
	return d
}

func makeRef(name, component, version string) desc.Reference {
	ref := desc.Reference{Component: component}
	ref.Name = name
	ref.Version = version
	return ref
}

// resolverSpec is a single path matcher entry in the resolvers.config.
type resolverSpecEntry struct {
	Repository           json.RawMessage `json:"repository"`
	ComponentNamePattern string          `json:"componentNamePattern,omitempty"`
}

// fallbackSpecEntry is a single deprecated fallback resolver entry.
type fallbackSpecEntry struct {
	Repository json.RawMessage `json:"repository"`
	Prefix     string          `json:"prefix,omitempty"`
	Priority   int             `json:"priority,omitempty"`
}

// pathMatcherOCMConfig builds an .ocmconfig JSON with a single resolvers.config
// entry carrying the given path matcher resolvers.
func pathMatcherOCMConfig(t *testing.T, entries ...resolverSpecEntry) string {
	t.Helper()
	inner := map[string]any{
		"type":      "resolvers.config.ocm.software/v1alpha1",
		"resolvers": entries,
	}
	return marshalOCMConfig(t, inner)
}

// fallbackOCMConfig builds an .ocmconfig JSON with a single ocm.config entry
// carrying the given deprecated fallback resolvers.
func fallbackOCMConfig(t *testing.T, entries ...fallbackSpecEntry) string {
	t.Helper()
	inner := map[string]any{
		"type":      "ocm.config.ocm.software/v1",
		"resolvers": entries,
	}
	return marshalOCMConfig(t, inner)
}

// mixedOCMConfig builds an .ocmconfig JSON carrying both resolver types.
func mixedOCMConfig(t *testing.T, pm []resolverSpecEntry, fb []fallbackSpecEntry) string {
	t.Helper()
	return marshalOCMConfig(t,
		map[string]any{"type": "resolvers.config.ocm.software/v1alpha1", "resolvers": pm},
		map[string]any{"type": "ocm.config.ocm.software/v1", "resolvers": fb},
	)
}

func marshalOCMConfig(t *testing.T, configurations ...map[string]any) string {
	t.Helper()
	generic := map[string]any{
		"type":           "generic.config.ocm.software/v1",
		"configurations": configurations,
	}
	raw, err := json.Marshal(generic)
	require.NoError(t, err)
	return string(raw)
}

func ocmConfigMap(name, namespace, ocmConfig string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Data:       map[string]string{".ocmconfig": ocmConfig},
	}
}

// readyComponentWithSpec builds a ready Component whose resolved identity and
// repository spec are supplied by the caller.
func readyComponentWithSpec(name, namespace, component, version string, repoSpec []byte) *v1alpha1.Component {
	c := &v1alpha1.Component{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Status: v1alpha1.ComponentStatus{
			Component: v1alpha1.ComponentInfo{
				Component:      component,
				Version:        version,
				RepositorySpec: &apiextensionsv1.JSON{Raw: repoSpec},
			},
			Conditions: []metav1.Condition{{
				Type:               v1alpha1.ReadyCondition,
				Status:             metav1.ConditionTrue,
				Reason:             v1alpha1.SucceededReason,
				Message:            "ready",
				LastTransitionTime: metav1.Now(),
			}},
		},
	}
	return c
}

// realPluginReconciler builds a reconciler whose plugin manager is a real
// setup.NewPluginManager (needed to build CTF repositories).
func realPluginReconciler(t *testing.T, objs ...client.Object) (*Reconciler, client.Client) {
	t.Helper()
	rec, c := newReconciler(t, objs...)
	rec.NewPluginManager = func(ctx context.Context, cfg *genericv1.Config) (*manager.PluginManager, error) {
		return setup.NewPluginManager(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}
	return rec, c
}

// reconcileUntilSettled reconciles repeatedly, skipping the initial
// "effective ocm config changed" publications, and returns the first result
// and error of a settled reconcile (or a terminal condition).
func reconcileUntilSettled(t *testing.T, rec *Reconciler, key client.ObjectKey) (ctrl.Result, error) {
	t.Helper()
	var (
		result ctrl.Result
		err    error
	)
	for range 5 {
		result, err = rec.Reconcile(t.Context(), ctrl.Request{NamespacedName: key})
		if err == nil || err.Error() != "effective ocm config changed" {
			return result, err
		}
	}
	t.Fatalf("discovery did not settle past effective-config publication")
	return result, err
}

func componentNamesFromStatus(t *testing.T, d *v1alpha1.Discovery) []string {
	t.Helper()
	names := make([]string, 0, len(d.Status.Components))
	for _, raw := range d.Status.Components {
		var m struct {
			Component struct {
				Name string `json:"name"`
			} `json:"component"`
		}
		require.NoError(t, json.Unmarshal(raw.Raw, &m))
		names = append(names, m.Component.Name)
	}
	return names
}

func TestReconcile_PathMatcherResolutionAcrossRepositories(t *testing.T) {
	r := require.New(t)

	const ns = "default"

	// root (repo A) -> child (repo B) -> grand (repo C); root also references
	// an unmatched "other" that lives in A (catch-all).
	root := makeDescriptor("ocm.software/root", "1.0.0",
		makeRef("child", "ocm.software/child", "1.0.0"),
		makeRef("other", "ocm.software/other", "1.0.0"),
	)
	other := makeDescriptor("ocm.software/other", "1.0.0")
	child := makeDescriptor("ocm.software/child", "1.0.0",
		makeRef("grand", "ocm.software/grand", "1.0.0"),
	)
	grand := makeDescriptor("ocm.software/grand", "1.0.0")

	repoA := ctfRepo(t, root, other)
	repoB := ctfRepo(t, child)
	repoC := ctfRepo(t, grand)

	// A conflicting root matcher points to C (which has no root): the
	// high-priority root pattern (base repo A) must still win.
	ocmConfig := pathMatcherOCMConfig(t,
		resolverSpecEntry{Repository: repoC, ComponentNamePattern: "ocm.software/root"},
		resolverSpecEntry{Repository: repoB, ComponentNamePattern: "ocm.software/child"},
		resolverSpecEntry{Repository: repoC, ComponentNamePattern: "ocm.software/grand"},
	)

	cm := ocmConfigMap("resolvers", ns, ocmConfig)
	component := readyComponentWithSpec("component", ns, root.Component.Name, root.Component.Version, repoA)
	discovery := &v1alpha1.Discovery{
		ObjectMeta: metav1.ObjectMeta{Name: "discovery", Namespace: ns},
		Spec: v1alpha1.DiscoverySpec{
			ComponentRef: corev1.LocalObjectReference{Name: component.Name},
			OCMConfig: []v1alpha1.OCMConfiguration{{
				NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{Kind: "ConfigMap", Name: cm.Name},
			}},
		},
	}

	rec, c := realPluginReconciler(t, cm, component, discovery)

	_, err := reconcileUntilSettled(t, rec, client.ObjectKeyFromObject(discovery))
	r.NoError(err)

	current := &v1alpha1.Discovery{}
	r.NoError(c.Get(t.Context(), client.ObjectKeyFromObject(discovery), current))
	r.Equal(v1alpha1.SucceededReason, status.FindCondition(current, v1alpha1.ReadyCondition).Reason)
	r.ElementsMatch([]string{
		"ocm.software/root",
		"ocm.software/child",
		"ocm.software/grand",
		"ocm.software/other",
	}, componentNamesFromStatus(t, current))
}

func TestReconcile_FallbackResolutionAcrossRepositories(t *testing.T) {
	r := require.New(t)

	const ns = "default"

	// root and shared live in A; child and shared live in B. The base repo A
	// is highest priority, so shared resolves from A and child (missing in A)
	// falls back to B.
	root := makeDescriptor("ocm.software/root", "1.0.0",
		makeRef("child", "ocm.software/child", "1.0.0"),
		makeRef("shared", "ocm.software/shared", "1.0.0"),
	)
	sharedA := makeDescriptor("ocm.software/shared", "1.0.0")
	child := makeDescriptor("ocm.software/child", "1.0.0")
	sharedB := makeDescriptor("ocm.software/shared", "1.0.0")

	repoA := ctfRepo(t, root, sharedA)
	repoB := ctfRepo(t, child, sharedB)

	ocmConfig := fallbackOCMConfig(t,
		fallbackSpecEntry{Repository: repoB, Priority: 10},
	)

	cm := ocmConfigMap("resolvers", ns, ocmConfig)
	component := readyComponentWithSpec("component", ns, root.Component.Name, root.Component.Version, repoA)
	discovery := &v1alpha1.Discovery{
		ObjectMeta: metav1.ObjectMeta{Name: "discovery", Namespace: ns},
		Spec: v1alpha1.DiscoverySpec{
			ComponentRef: corev1.LocalObjectReference{Name: component.Name},
			OCMConfig: []v1alpha1.OCMConfiguration{{
				NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{Kind: "ConfigMap", Name: cm.Name},
			}},
		},
	}

	rec, c := realPluginReconciler(t, cm, component, discovery)

	_, err := reconcileUntilSettled(t, rec, client.ObjectKeyFromObject(discovery))
	r.NoError(err)

	current := &v1alpha1.Discovery{}
	r.NoError(c.Get(t.Context(), client.ObjectKeyFromObject(discovery), current))
	r.Equal(v1alpha1.SucceededReason, status.FindCondition(current, v1alpha1.ReadyCondition).Reason)
	r.ElementsMatch([]string{
		"ocm.software/root",
		"ocm.software/child",
		"ocm.software/shared",
	}, componentNamesFromStatus(t, current))
}

func TestReconcile_MatchedRepositoryMissingChildDoesNotFallThrough(t *testing.T) {
	r := require.New(t)

	const ns = "default"

	// root (repo A) -> child. root and child both live in A, but the child
	// matcher points to B (which lacks the child): path matching selects one
	// repository and must not fall through to the A catch-all.
	root := makeDescriptor("ocm.software/root", "1.0.0",
		makeRef("child", "ocm.software/child", "1.0.0"),
	)
	child := makeDescriptor("ocm.software/child", "1.0.0")

	repoA := ctfRepo(t, root, child)
	repoB := ctfRepo(t) // empty

	// Establish a successful reconcile first with a config that resolves child
	// from A, then swap to the broken config.
	goodConfig := pathMatcherOCMConfig(t,
		resolverSpecEntry{Repository: repoA, ComponentNamePattern: "ocm.software/child"},
	)
	cm := ocmConfigMap("resolvers", ns, goodConfig)
	component := readyComponentWithSpec("component", ns, root.Component.Name, root.Component.Version, repoA)
	discovery := &v1alpha1.Discovery{
		ObjectMeta: metav1.ObjectMeta{Name: "discovery", Namespace: ns},
		Spec: v1alpha1.DiscoverySpec{
			ComponentRef: corev1.LocalObjectReference{Name: component.Name},
			OCMConfig: []v1alpha1.OCMConfiguration{{
				NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{Kind: "ConfigMap", Name: cm.Name},
			}},
		},
	}

	rec, c := realPluginReconciler(t, cm, component, discovery)

	_, err := reconcileUntilSettled(t, rec, client.ObjectKeyFromObject(discovery))
	r.NoError(err)

	settled := &v1alpha1.Discovery{}
	r.NoError(c.Get(t.Context(), client.ObjectKeyFromObject(discovery), settled))
	r.Equal(v1alpha1.SucceededReason, status.FindCondition(settled, v1alpha1.ReadyCondition).Reason)
	successComponents := settled.Status.Components
	successGeneration := settled.Status.ObservedGeneration
	r.NotEmpty(successComponents)

	// Swap the config to route the child to the empty repo B.
	brokenConfig := pathMatcherOCMConfig(t,
		resolverSpecEntry{Repository: repoB, ComponentNamePattern: "ocm.software/child"},
	)
	freshCM := &corev1.ConfigMap{}
	r.NoError(c.Get(t.Context(), client.ObjectKeyFromObject(cm), freshCM))
	freshCM.Data[".ocmconfig"] = brokenConfig
	r.NoError(c.Update(t.Context(), freshCM))

	_, err = reconcileUntilSettled(t, rec, client.ObjectKeyFromObject(discovery))
	r.Error(err)

	current := &v1alpha1.Discovery{}
	r.NoError(c.Get(t.Context(), client.ObjectKeyFromObject(discovery), current))
	ready := status.FindCondition(current, v1alpha1.ReadyCondition)
	r.Equal(metav1.ConditionFalse, ready.Status)
	r.Equal(v1alpha1.ResolutionFailedReason, ready.Reason)
	r.False(status.IsStalled(current), "resolution failures are retryable")
	// The previous successful payload and observed generation are retained.
	r.Equal(successComponents, current.Status.Components)
	r.Equal(successGeneration, current.Status.ObservedGeneration)
}

func TestReconcile_MixedResolverTypesFailRetryablyAndRetainPayload(t *testing.T) {
	r := require.New(t)

	const ns = "default"

	root := makeDescriptor("ocm.software/root", "1.0.0")
	repoA := ctfRepo(t, root)

	// Start with a valid path matcher config to establish a payload.
	goodConfig := pathMatcherOCMConfig(t,
		resolverSpecEntry{Repository: repoA, ComponentNamePattern: "ocm.software/*"},
	)
	cm := ocmConfigMap("resolvers", ns, goodConfig)
	component := readyComponentWithSpec("component", ns, root.Component.Name, root.Component.Version, repoA)
	discovery := &v1alpha1.Discovery{
		ObjectMeta: metav1.ObjectMeta{Name: "discovery", Namespace: ns},
		Spec: v1alpha1.DiscoverySpec{
			ComponentRef: corev1.LocalObjectReference{Name: component.Name},
			OCMConfig: []v1alpha1.OCMConfiguration{{
				NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{Kind: "ConfigMap", Name: cm.Name},
			}},
		},
	}

	rec, c := realPluginReconciler(t, cm, component, discovery)

	_, err := reconcileUntilSettled(t, rec, client.ObjectKeyFromObject(discovery))
	r.NoError(err)

	settled := &v1alpha1.Discovery{}
	r.NoError(c.Get(t.Context(), client.ObjectKeyFromObject(discovery), settled))
	successComponents := settled.Status.Components
	successGeneration := settled.Status.ObservedGeneration
	r.NotEmpty(successComponents)

	// Swap to a config carrying both resolver types.
	mixed := mixedOCMConfig(t,
		[]resolverSpecEntry{{Repository: repoA, ComponentNamePattern: "ocm.software/*"}},
		[]fallbackSpecEntry{{Repository: repoA, Priority: 10}},
	)
	freshCM := &corev1.ConfigMap{}
	r.NoError(c.Get(t.Context(), client.ObjectKeyFromObject(cm), freshCM))
	freshCM.Data[".ocmconfig"] = mixed
	r.NoError(c.Update(t.Context(), freshCM))

	_, err = reconcileUntilSettled(t, rec, client.ObjectKeyFromObject(discovery))
	r.Error(err)

	current := &v1alpha1.Discovery{}
	r.NoError(c.Get(t.Context(), client.ObjectKeyFromObject(discovery), current))
	ready := status.FindCondition(current, v1alpha1.ReadyCondition)
	r.Equal(metav1.ConditionFalse, ready.Status)
	r.Equal(v1alpha1.GetRepositoryFailedReason, ready.Reason)
	r.False(status.IsStalled(current), "mixed resolver config is retryable")
	r.Equal(successComponents, current.Status.Components)
	r.Equal(successGeneration, current.Status.ObservedGeneration)
}

func TestReconcile_InheritedEffectiveConfigResolution(t *testing.T) {
	r := require.New(t)

	const ns = "default"

	root := makeDescriptor("ocm.software/root", "1.0.0",
		makeRef("child", "ocm.software/child", "1.0.0"),
	)
	child := makeDescriptor("ocm.software/child", "1.0.0")
	repoA := ctfRepo(t, root)
	repoB := ctfRepo(t, child)

	ocmConfig := pathMatcherOCMConfig(t,
		resolverSpecEntry{Repository: repoB, ComponentNamePattern: "ocm.software/child"},
	)
	cm := ocmConfigMap("resolvers", ns, ocmConfig)

	// The Component carries the effective config with propagate policy; the
	// Discovery inherits it (no own OCMConfig).
	component := readyComponentWithSpec("component", ns, root.Component.Name, root.Component.Version, repoA)
	component.Status.EffectiveOCMConfig = []v1alpha1.OCMConfiguration{{
		NamespacedObjectKindReference: v1alpha1.NamespacedObjectKindReference{
			APIVersion: "v1", Kind: "ConfigMap", Name: cm.Name, Namespace: ns,
		},
		Policy: v1alpha1.ConfigurationPolicyPropagate,
	}}

	discovery := &v1alpha1.Discovery{
		ObjectMeta: metav1.ObjectMeta{Name: "discovery", Namespace: ns},
		Spec:       v1alpha1.DiscoverySpec{ComponentRef: corev1.LocalObjectReference{Name: component.Name}},
	}

	rec, c := realPluginReconciler(t, cm, component, discovery)

	_, err := reconcileUntilSettled(t, rec, client.ObjectKeyFromObject(discovery))
	r.NoError(err)

	current := &v1alpha1.Discovery{}
	r.NoError(c.Get(t.Context(), client.ObjectKeyFromObject(discovery), current))
	r.Equal(v1alpha1.SucceededReason, status.FindCondition(current, v1alpha1.ReadyCondition).Reason)
	r.ElementsMatch([]string{"ocm.software/root", "ocm.software/child"}, componentNamesFromStatus(t, current))
	r.NotEmpty(current.Status.EffectiveOCMConfig, "inherited effective config is published")
}
