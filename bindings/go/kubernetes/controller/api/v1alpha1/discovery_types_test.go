package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestDiscoverySchemeRegistration(t *testing.T) {
	r := require.New(t)

	scheme := runtime.NewScheme()
	r.NoError(AddToScheme(scheme))

	discovery := &Discovery{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "test", Generation: 3},
		Status: DiscoveryStatus{
			ObservedGeneration: 3,
			Extracted: []ExtractedRecord{
				{"nested": {Raw: []byte(`{"value":"original"}`)}},
			},
		},
	}
	gvks, unversioned, err := scheme.ObjectKinds(discovery)
	r.NoError(err)
	r.False(unversioned)
	r.Contains(gvks, GroupVersion.WithKind(KindDiscovery))

	list := &DiscoveryList{}
	gvks, unversioned, err = scheme.ObjectKinds(list)
	r.NoError(err)
	r.False(unversioned)
	r.Contains(gvks, GroupVersion.WithKind(KindDiscovery+"List"))

	copied := discovery.DeepCopyObject()
	r.Equal(discovery, copied)
	r.NotSame(discovery, copied)

	copyDiscovery := copied.(*Discovery)
	copyDiscovery.Status.Extracted[0]["nested"].Raw[0] = '['
	r.NotEqual(discovery.Status.Extracted[0]["nested"].Raw, copyDiscovery.Status.Extracted[0]["nested"].Raw)
}

// TestDiscoveryStatusPayloadPresence verifies the nil-vs-allocated-empty
// serialization contract of the optional status payload fields: absent when
// never computed, present-but-empty when computed with an empty result.
func TestDiscoveryStatusPayloadPresence(t *testing.T) {
	r := require.New(t)

	t.Run("absent when not computed", func(t *testing.T) {
		data, err := json.Marshal(&DiscoveryStatus{})
		r.NoError(err)
		r.NotContains(string(data), "components")
		r.NotContains(string(data), "extracted")
	})

	t.Run("present when computed empty", func(t *testing.T) {
		data, err := json.Marshal(&DiscoveryStatus{
			Components: []apiextensionsv1.JSON{},
		})
		r.NoError(err)
		r.Contains(string(data), `"components":[]`)
		r.NotContains(string(data), "extracted")

		data, err = json.Marshal(&DiscoveryStatus{
			Extracted: []ExtractedRecord{},
		})
		r.NoError(err)
		r.Contains(string(data), `"extracted":[]`)
		r.NotContains(string(data), "components")
	})
}

func TestExtractedRecordSerialization(t *testing.T) {
	r := require.New(t)

	record := ExtractedRecord{
		"nested": {Raw: []byte(`{"items":[1,true],"metadata":{"source":"test"}}`)},
		"null":   {Raw: []byte(`null`)},
	}
	data, err := json.Marshal(record)
	r.NoError(err)
	r.JSONEq(`{"nested":{"items":[1,true],"metadata":{"source":"test"}},"null":null}`, string(data))

	var decoded ExtractedRecord
	r.NoError(json.Unmarshal(data, &decoded))
	r.JSONEq(`{"items":[1,true],"metadata":{"source":"test"}}`, string(decoded["nested"].Raw))
	r.Nil(decoded["null"].Raw)
}

// TestExtractModePresence verifies that the extract CEL exclusivity rule
// sees exactly the modes a Go client serialized: nil maps are omitted,
// explicitly empty maps stay present.
func TestExtractModePresence(t *testing.T) {
	r := require.New(t)

	data, err := json.Marshal(&Extract{})
	r.NoError(err)
	r.NotContains(string(data), "byResources")
	r.NotContains(string(data), "byComponents")
	r.NotContains(string(data), "expression")

	data, err = json.Marshal(&Extract{ByResources: map[string]string{}})
	r.NoError(err)
	r.Contains(string(data), `"byResources":{}`)

	data, err = json.Marshal(&Extract{ByComponents: map[string]string{}})
	r.NoError(err)
	r.Contains(string(data), `"byComponents":{}`)

	data, err = json.Marshal(&Extract{Expression: "components"})
	r.NoError(err)
	r.Contains(string(data), `"expression":"components"`)
	r.NotContains(string(data), "byResources")
}

func TestDiscoveryAccessors(t *testing.T) {
	r := require.New(t)

	config := []OCMConfiguration{
		{
			NamespacedObjectKindReference: NamespacedObjectKindReference{
				Kind: "Secret",
				Name: "test",
			},
			Policy: ConfigurationPolicyPropagate,
		},
	}
	discovery := &Discovery{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "test"},
		Spec: DiscoverySpec{
			OCMConfig: config,
		},
		Status: DiscoveryStatus{
			EffectiveOCMConfig: config,
			Conditions: []metav1.Condition{
				{Type: ReadyCondition, Status: metav1.ConditionTrue, Reason: SucceededReason, Message: "done"},
			},
		},
	}

	r.Equal(KindDiscovery, discovery.GetKind())
	r.Equal(config, discovery.GetSpecifiedOCMConfig())
	r.Equal(config, discovery.GetEffectiveOCMConfig())
	r.Equal("default:test", discovery.GetVID()[GroupVersion.Group+"/discovery_version"])
	r.Len(discovery.GetConditions(), 1)

	discovery.SetObservedGeneration(7)
	r.Equal(int64(7), discovery.Status.ObservedGeneration)

	discovery.SetConditions([]metav1.Condition{})
	r.Empty(discovery.Status.Conditions)

	r.Same(&discovery.ObjectMeta, discovery.GetObjectMeta())
}
