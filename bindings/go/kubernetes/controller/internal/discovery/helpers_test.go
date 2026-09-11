package discovery

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
)

// mustConvertV2 converts a runtime descriptor to its v2 form for JSON snapshot
// comparisons in input-preservation tests.
func mustConvertV2(t *testing.T, d *descriptor.Descriptor) *v2.Descriptor {
	t.Helper()
	v2desc, err := descriptor.ConvertToV2(runtime.NewScheme(runtime.WithAllowUnknown()), d)
	require.NoError(t, err)
	return v2desc
}

func stringLabel(name, value string) descriptor.Label {
	return descriptor.Label{Name: name, Value: json.RawMessage(strconv.Quote(value))}
}

func structuredLabel(name string, value any) descriptor.Label {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return descriptor.Label{Name: name, Value: raw}
}

type referenceOption func(*descriptor.Reference)

func withRefLabels(labels ...descriptor.Label) referenceOption {
	return func(r *descriptor.Reference) { r.Labels = labels }
}

func withRefExtras(extras map[string]string) referenceOption {
	return func(r *descriptor.Reference) { r.ExtraIdentity = extras }
}

func newReference(name, component, version string, opts ...referenceOption) descriptor.Reference {
	ref := descriptor.Reference{Component: component}
	ref.Name = name
	ref.Version = version
	for _, opt := range opts {
		opt(&ref)
	}
	return ref
}

type resourceOption func(*descriptor.Resource)

func withResourceLabels(labels ...descriptor.Label) resourceOption {
	return func(r *descriptor.Resource) { r.Labels = labels }
}

func withResourceExtras(extras map[string]string) resourceOption {
	return func(r *descriptor.Resource) { r.ExtraIdentity = extras }
}

func newResource(name string, opts ...resourceOption) descriptor.Resource {
	res := descriptor.Resource{}
	res.Name = name
	res.Version = "1.0.0"
	res.Type = "ociImage"
	res.Relation = descriptor.LocalRelation
	res.Access = &runtime.Raw{
		Type: runtime.NewVersionedType("OCIImage", "v1"),
		Data: []byte(`{"type":"OCIImage/v1","imageReference":"ghcr.io/ocm/` + name + `:1.0.0"}`),
	}
	for _, opt := range opts {
		opt(&res)
	}
	return res
}

type descriptorOption func(*descriptor.Descriptor)

func withReferences(refs ...descriptor.Reference) descriptorOption {
	return func(d *descriptor.Descriptor) { d.Component.References = refs }
}

func withResources(resources ...descriptor.Resource) descriptorOption {
	return func(d *descriptor.Descriptor) { d.Component.Resources = resources }
}

func withComponentLabels(labels ...descriptor.Label) descriptorOption {
	return func(d *descriptor.Descriptor) { d.Component.Labels = labels }
}

func newDescriptor(name, version string, opts ...descriptorOption) *descriptor.Descriptor {
	d := &descriptor.Descriptor{}
	d.Meta.Version = "v2"
	d.Component.Name = name
	d.Component.Version = version
	d.Component.Provider = descriptor.Provider{Name: "ocm.software"}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// extractSpec builds a DiscoverySpec carrying the given extract config.
func specWithExtract(extract *v1alpha1.Extract) *v1alpha1.DiscoverySpec {
	return &v1alpha1.DiscoverySpec{Extract: extract}
}
