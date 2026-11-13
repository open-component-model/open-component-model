package v1alpha1

import (
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
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
		data, err := json.Marshal(&DiscoveryStatus{}, ocmruntime.JSONOptions)
		r.NoError(err)
		r.NotContains(string(data), "components")
		r.NotContains(string(data), "extracted")
	})

	t.Run("present when computed empty", func(t *testing.T) {
		data, err := json.Marshal(&DiscoveryStatus{
			Components: []apiextensionsv1.JSON{},
		}, ocmruntime.JSONOptions)
		r.NoError(err)
		r.Contains(string(data), `"components":[]`)
		r.NotContains(string(data), "extracted")

		data, err = json.Marshal(&DiscoveryStatus{
			Extracted: []ExtractedRecord{},
		}, ocmruntime.JSONOptions)
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
	data, err := json.Marshal(record, ocmruntime.JSONOptions)
	r.NoError(err)
	r.JSONEq(`{"nested":{"items":[1,true],"metadata":{"source":"test"}},"null":null}`, string(data))

	var decoded ExtractedRecord
	r.NoError(json.Unmarshal(data, &decoded))
	r.JSONEq(`{"items":[1,true],"metadata":{"source":"test"}}`, string(decoded["nested"].Raw))
	r.Nil(decoded["null"].Raw)
}

// TestExtractedRecordAPIMachineryRoundTrip pins the CRD serialization path:
// the apimachinery JSON serializer, not encoding/json/v2, is what writes and
// reads status.extracted against the API server. ExtractedRecord implements
// json.MarshalerTo/UnmarshalerFrom, so this verifies the apiextensionsv1.JSON
// members survive a full encode/decode through that codec unchanged.
func TestExtractedRecordAPIMachineryRoundTrip(t *testing.T) {
	r := require.New(t)

	scheme := runtime.NewScheme()
	r.NoError(AddToScheme(scheme))
	codec := serializer.NewCodecFactory(scheme).LegacyCodec(GroupVersion)

	discovery := &Discovery{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "test"},
		Status: DiscoveryStatus{
			Extracted: []ExtractedRecord{
				{
					"nested": {Raw: []byte(`{"items":[1,true],"metadata":{"source":"test"}}`)},
					"null":   {Raw: []byte(`null`)},
				},
			},
		},
	}

	data, err := runtime.Encode(codec, discovery)
	r.NoError(err)
	r.Contains(string(data), `"nested":{"items":[1,true],"metadata":{"source":"test"}}`)

	decoded := &Discovery{}
	_, _, err = codec.Decode(data, nil, decoded)
	r.NoError(err)
	r.Len(decoded.Status.Extracted, 1)
	r.JSONEq(`{"items":[1,true],"metadata":{"source":"test"}}`, string(decoded.Status.Extracted[0]["nested"].Raw))
	r.Nil(decoded.Status.Extracted[0]["null"].Raw, "a null member decodes to an empty apiextensionsv1.JSON, as it does with encoding/json/v2")
}

// TestExtractModePresence verifies that the extract CEL exclusivity rule
// sees exactly the modes a Go client serialized: nil maps are omitted,
// explicitly empty maps stay present.
func TestExtractModePresence(t *testing.T) {
	r := require.New(t)

	data, err := json.Marshal(&Extract{}, ocmruntime.JSONOptions)
	r.NoError(err)
	r.NotContains(string(data), "byResources")
	r.NotContains(string(data), "byComponents")
	r.NotContains(string(data), "expression")

	data, err = json.Marshal(&Extract{ByResources: map[string]string{}}, ocmruntime.JSONOptions)
	r.NoError(err)
	r.Contains(string(data), `"byResources":{}`)

	data, err = json.Marshal(&Extract{ByComponents: map[string]string{}}, ocmruntime.JSONOptions)
	r.NoError(err)
	r.Contains(string(data), `"byComponents":{}`)

	data, err = json.Marshal(&Extract{Expression: "components"}, ocmruntime.JSONOptions)
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
