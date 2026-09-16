package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	desc "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	internaldiscovery "ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/discovery"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/ocm"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/setup"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/status"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/test"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/repository"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	return scheme
}

func newReconciler(t *testing.T, objs ...client.Object) (*Reconciler, client.Client) {
	t.Helper()
	scheme := newTestScheme(t)
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&v1alpha1.Discovery{}, &v1alpha1.Component{}).
		Build()

	r := &Reconciler{
		BaseReconciler: &ocm.BaseReconciler{
			Client:           fakeClient,
			Scheme:           scheme,
			EventRecorder:    record.NewFakeRecorder(100),
			NewPluginManager: test.StaticPluginManager(nil),
		},
	}
	return r, fakeClient
}

func readyComponent(name, namespace string) *v1alpha1.Component {
	return &v1alpha1.Component{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Status: v1alpha1.ComponentStatus{
			Component: v1alpha1.ComponentInfo{
				Component:      "ocm.software/test",
				Version:        "1.0.0",
				RepositorySpec: &apiextensionsv1.JSON{Raw: []byte(`{"type":"ctf/v1","filePath":"/tmp/nonexistent","accessMode":"readOnly"}`)},
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
}

func TestReconcile_NotFound(t *testing.T) {
	g := require.New(t)
	rec, _ := newReconciler(t)

	result, err := rec.Reconcile(t.Context(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "absent"}})
	g.NoError(err)
	g.Equal(ctrl.Result{}, result)
}

func TestReconcile_SuspendedExitsWithoutStatusAdvancement(t *testing.T) {
	g := require.New(t)

	discovery := &v1alpha1.Discovery{
		ObjectMeta: metav1.ObjectMeta{Name: "disco", Namespace: "default"},
		Spec: v1alpha1.DiscoverySpec{
			ComponentRef: corev1.LocalObjectReference{Name: "missing-component"},
			Suspend:      true,
		},
	}
	rec, c := newReconciler(t, discovery)

	result, err := rec.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(discovery)})
	g.NoError(err)
	g.Equal(ctrl.Result{}, result, "suspended discoveries schedule no work")

	fresh := &v1alpha1.Discovery{}
	g.NoError(c.Get(t.Context(), client.ObjectKeyFromObject(discovery), fresh))
	g.Empty(fresh.Status.Conditions)
	g.Zero(fresh.Status.ObservedGeneration)
}

func TestReconcile_DeletingExitsWithoutStatusAdvancement(t *testing.T) {
	g := require.New(t)

	discovery := &v1alpha1.Discovery{
		ObjectMeta: metav1.ObjectMeta{Name: "disco", Namespace: "default"},
		Spec: v1alpha1.DiscoverySpec{
			ComponentRef: corev1.LocalObjectReference{Name: "missing-component"},
		},
	}
	rec, c := newReconciler(t, discovery)

	// give the object a deletion timestamp by deleting an object with a finalizer
	latest := &v1alpha1.Discovery{}
	g.NoError(c.Get(t.Context(), client.ObjectKeyFromObject(discovery), latest))
	latest.Finalizers = []string{"test/finalizer"}
	g.NoError(c.Update(t.Context(), latest))
	g.NoError(c.Delete(t.Context(), latest))

	result, err := rec.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(discovery)})
	g.NoError(err)
	g.Equal(ctrl.Result{}, result, "deleting discoveries schedule no work")

	fresh := &v1alpha1.Discovery{}
	g.NoError(c.Get(t.Context(), client.ObjectKeyFromObject(discovery), fresh))
	g.Empty(fresh.Status.Conditions)
	g.Zero(fresh.Status.ObservedGeneration)
}

func TestReconcile_TerminalSelectorFailureIsStalledAndNotRequeued(t *testing.T) {
	g := require.New(t)

	discovery := &v1alpha1.Discovery{
		ObjectMeta: metav1.ObjectMeta{Name: "disco", Namespace: "default"},
		Spec: v1alpha1.DiscoverySpec{
			ComponentRef: corev1.LocalObjectReference{Name: "component"},
			ComponentSelector: &v1alpha1.Selector{
				Expression: "identity.name in [", // invalid CEL
			},
		},
	}
	rec, c := newReconciler(t, readyComponent("component", "default"), discovery)

	result, err := rec.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(discovery)})
	g.Error(err)
	g.True(errors.Is(err, reconcile.TerminalError(nil)), "selector compilation errors are terminal, got %v", err)
	g.Equal(ctrl.Result{}, result, "terminal failures do not schedule periodic work")

	fresh := &v1alpha1.Discovery{}
	g.NoError(c.Get(t.Context(), client.ObjectKeyFromObject(discovery), fresh))
	ready := status.FindCondition(fresh, v1alpha1.ReadyCondition)
	g.NotNil(ready)
	g.Equal(metav1.ConditionFalse, ready.Status)
	g.Equal(v1alpha1.SelectorFailedReason, ready.Reason)
	g.True(status.IsStalled(fresh))
	g.Zero(fresh.Status.ObservedGeneration, "failures do not advance the observed generation")
}

// drainEvents returns everything the reconciler recorded so far, joined.
func drainEvents(t *testing.T, rec *Reconciler) string {
	t.Helper()
	recorder, ok := rec.EventRecorder.(*record.FakeRecorder)
	require.True(t, ok, "the test reconciler must use a FakeRecorder")

	var recorded []string
	for {
		select {
		case e := <-recorder.Events:
			recorded = append(recorded, e)
		default:
			return strings.Join(recorded, "\n")
		}
	}
}

func TestReconcile_PublishesRawAndExtractedPayloads(t *testing.T) {
	r := require.New(t)

	root := &desc.Descriptor{}
	root.Meta.Version = "v2"
	root.Component.Name = "root"
	root.Component.Version = "1.0.0"
	root.Component.Provider.Name = "ocm.software"
	ref := desc.Reference{Component: "child"}
	ref.Name = "child"
	ref.Version = "1.0.0"
	root.Component.References = []desc.Reference{ref}
	child := &desc.Descriptor{}
	child.Meta.Version = "v2"
	child.Component.Name = "child"
	child.Component.Version = "1.0.0"
	child.Component.Provider.Name = "ocm.software"

	repoSpec := &ctf.Repository{
		Type:       ocmruntime.Type{Version: "v1", Name: "ctf"},
		FilePath:   t.TempDir(),
		AccessMode: ctf.AccessModeReadWrite,
	}
	repo, err := ocirepository.NewFromCTFRepoV1(t.Context(), repoSpec)
	r.NoError(err)
	r.NoError(repo.AddComponentVersion(t.Context(), root))
	r.NoError(repo.AddComponentVersion(t.Context(), child))
	repoSpec.AccessMode = ctf.AccessModeReadOnly
	rawSpec, err := json.Marshal(repoSpec)
	r.NoError(err)

	component := readyComponent("component", "default")
	component.Status.Component = v1alpha1.ComponentInfo{
		Component:      root.Component.Name,
		Version:        root.Component.Version,
		RepositorySpec: &apiextensionsv1.JSON{Raw: rawSpec},
	}
	discovery := &v1alpha1.Discovery{
		ObjectMeta: metav1.ObjectMeta{Name: "discovery", Namespace: "default"},
		Spec:       v1alpha1.DiscoverySpec{ComponentRef: corev1.LocalObjectReference{Name: component.Name}},
	}
	rec, c := newReconciler(t, component, discovery)
	rec.NewPluginManager = func(ctx context.Context, cfg *genericv1.Config) (*manager.PluginManager, error) {
		return setup.NewPluginManager(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}

	result, err := rec.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(discovery)})
	r.NoError(err)
	r.Equal(ctrl.Result{}, result)

	// Discovery does not use the shared UpdateBeforePatch defer, so it emits the
	// reconciliation-finished event itself; without it `kubectl describe` shows
	// less than it does for the other kinds.
	r.Contains(drainEvents(t, rec), "Reconciliation finished")

	current := &v1alpha1.Discovery{}
	r.NoError(c.Get(t.Context(), client.ObjectKeyFromObject(discovery), current))
	r.Len(current.Status.Components, 2)
	r.Nil(current.Status.Extracted)
	r.Equal(v1alpha1.SucceededReason, status.FindCondition(current, v1alpha1.ReadyCondition).Reason)

	current.Spec.Extract = &v1alpha1.Extract{ByComponents: map[string]string{"name": "component.name"}}
	r.NoError(c.Update(t.Context(), current))
	r.NoError(c.Get(t.Context(), client.ObjectKeyFromObject(discovery), current))

	result, err = rec.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(current)})
	r.NoError(err)
	r.Equal(ctrl.Result{}, result)
	r.NoError(c.Get(t.Context(), client.ObjectKeyFromObject(discovery), current))
	r.Nil(current.Status.Components)
	r.Len(current.Status.Extracted, 2)
	r.Contains(string(current.Status.Extracted[0]["name"].Raw), "child")
	r.Contains(string(current.Status.Extracted[1]["name"].Raw), "root")
}

func TestReconcile_UnreadyComponentIsTerminal(t *testing.T) {
	g := require.New(t)

	discovery := &v1alpha1.Discovery{
		ObjectMeta: metav1.ObjectMeta{Name: "disco", Namespace: "default"},
		Spec: v1alpha1.DiscoverySpec{
			ComponentRef: corev1.LocalObjectReference{Name: "missing-component"},
		},
	}
	rec, c := newReconciler(t, discovery)

	_, err := rec.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(discovery)})
	g.Error(err)
	g.True(errors.Is(err, reconcile.TerminalError(nil)), "nothing is retried; recovery comes from the Component watch")

	fresh := &v1alpha1.Discovery{}
	g.NoError(c.Get(t.Context(), client.ObjectKeyFromObject(discovery), fresh))
	ready := status.FindCondition(fresh, v1alpha1.ReadyCondition)
	g.NotNil(ready)
	g.Equal(metav1.ConditionFalse, ready.Status)
	g.Equal(v1alpha1.ResourceIsNotAvailable, ready.Reason)
}

func TestUpToDate(t *testing.T) {
	const key = "jsonNormalisation/v4alpha1:SHA-256:abc"
	info := func(value string) v1alpha1.ComponentInfo {
		if value == "" {
			return v1alpha1.ComponentInfo{}
		}
		return v1alpha1.ComponentInfo{Digest: &v2.Digest{
			HashAlgorithm: "SHA-256", NormalisationAlgorithm: "jsonNormalisation/v4alpha1", Value: value,
		}}
	}
	discovery := func(mutate func(*v1alpha1.Discovery)) *v1alpha1.Discovery {
		d := &v1alpha1.Discovery{ObjectMeta: metav1.ObjectMeta{Generation: 3}}
		d.Status.ObservedGeneration = 3
		d.Status.ObservedComponentDigest = key
		d.Status.Components = []apiextensionsv1.JSON{}
		if mutate != nil {
			mutate(d)
		}
		return d
	}

	for _, tc := range []struct {
		name     string
		discover *v1alpha1.Discovery
		info     v1alpha1.ComponentInfo
		want     bool
	}{
		{"unchanged", discovery(nil), info("abc"), true},
		{"unchanged with extracted payload", discovery(func(d *v1alpha1.Discovery) {
			d.Status.Components = nil
			d.Status.Extracted = []v1alpha1.ExtractedRecord{}
		}), info("abc"), true},
		{"no recorded digest", discovery(func(d *v1alpha1.Discovery) {
			d.Status.ObservedComponentDigest = ""
		}), info("abc"), false},
		{"spec changed", discovery(func(d *v1alpha1.Discovery) { d.Generation = 4 }), info("abc"), false},
		{"no payload published", discovery(func(d *v1alpha1.Discovery) {
			d.Status.Components = nil
		}), info("abc"), false},
		{"digest changed", discovery(nil), info("def"), false},
		{"component has no digest", discovery(nil), info(""), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, upToDate(tc.discover, tc.info))
		})
	}
}

func TestSetPayloadRejectsOversizedPayload(t *testing.T) {
	r := require.New(t)
	rec, _ := newReconciler(t)

	big := bytes.Repeat([]byte("a"), maxPayloadBytes/2)
	oversized := &internaldiscovery.Payload{Components: []json.RawMessage{
		json.RawMessage(fmt.Sprintf("%q", big)),
		json.RawMessage(fmt.Sprintf("%q", big)),
		json.RawMessage(fmt.Sprintf("%q", big)),
	}}

	discovery := &v1alpha1.Discovery{}
	discovery.Status.Components = []apiextensionsv1.JSON{{Raw: []byte(`{"kept":true}`)}}

	err := rec.setPayload(discovery, oversized)
	r.ErrorIs(err, errPayloadTooLarge)
	r.ErrorContains(err, "limit is")
	r.Equal([]apiextensionsv1.JSON{{Raw: []byte(`{"kept":true}`)}}, discovery.Status.Components,
		"the persisted payload must survive an oversized computation")

	// Extracted mode is guarded the same way.
	discovery = &v1alpha1.Discovery{}
	records := make([]map[string]any, 0, 3)
	for range 3 {
		records = append(records, map[string]any{"blob": string(big)})
	}
	r.ErrorIs(rec.setPayload(discovery, &internaldiscovery.Payload{Extracted: records}), errPayloadTooLarge)
	r.Nil(discovery.Status.Extracted)

	// A payload under the limit is written.
	small := &internaldiscovery.Payload{Components: []json.RawMessage{json.RawMessage(`{"ok":true}`)}}
	discovery = &v1alpha1.Discovery{}
	r.NoError(rec.setPayload(discovery, small))
	r.Len(discovery.Status.Components, 1)
}

// TestMapComponentToDiscoveries pins the root-reference routing: a Component
// change must reach exactly the same-namespace Discoveries that discover it.
// This is the only path that wakes a Discovery from upstream, so a miss here is
// a Discovery that silently stops updating.
func TestMapComponentToDiscoveries(t *testing.T) {
	r := require.New(t)

	discoveries := []client.Object{
		&v1alpha1.Discovery{
			ObjectMeta: metav1.ObjectMeta{Name: "root", Namespace: "default"},
			Spec:       v1alpha1.DiscoverySpec{ComponentRef: corev1.LocalObjectReference{Name: "component"}},
		},
		&v1alpha1.Discovery{
			ObjectMeta: metav1.ObjectMeta{Name: "other-namespace", Namespace: "other"},
			Spec:       v1alpha1.DiscoverySpec{ComponentRef: corev1.LocalObjectReference{Name: "component"}},
		},
		&v1alpha1.Discovery{
			ObjectMeta: metav1.ObjectMeta{Name: "other-root", Namespace: "default"},
			Spec:       v1alpha1.DiscoverySpec{ComponentRef: corev1.LocalObjectReference{Name: "other"}},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithObjects(discoveries...).
		WithIndex(&v1alpha1.Discovery{}, componentRefIndex, func(obj client.Object) []string {
			return []string{obj.(*v1alpha1.Discovery).Spec.ComponentRef.Name}
		}).
		Build()
	reconciler := &Reconciler{BaseReconciler: &ocm.BaseReconciler{Client: fakeClient}}

	requests := reconciler.mapComponentToDiscoveries(t.Context(), &v1alpha1.Component{
		ObjectMeta: metav1.ObjectMeta{Name: "component", Namespace: "default"},
	})

	r.Equal([]reconcile.Request{
		{NamespacedName: types.NamespacedName{Name: "root", Namespace: "default"}},
	}, requests, "only same-namespace references to this component match")

	r.Nil(reconciler.mapComponentToDiscoveries(t.Context(), &v1alpha1.Resource{}),
		"a wrong object type yields no requests")
}
