package descriptortest_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/internal/testutil/descriptortest"
)

func TestNew(t *testing.T) {
	r := require.New(t)

	got := descriptortest.New("ocm.software/app", "1.0.0")

	want := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: "ocm.software/app", Version: "1.0.0"},
			},
			Provider: descriptor.Provider{Name: "ocm.software"},
		},
	}
	r.Equal(want, got)
	r.Nil(got.Component.Resources)
	r.Nil(got.Component.References)
	r.Nil(got.Component.Labels)
}

func TestOptions(t *testing.T) {
	res := descriptor.Resource{ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "res"}}}
	ref := descriptor.Reference{Component: "ocm.software/child"}
	label := descriptor.Label{Name: "sig", Value: []byte(`"v"`), Signing: true}

	tests := []struct {
		name  string
		opt   descriptortest.Option
		check func(r *require.Assertions, d *descriptor.Descriptor)
	}{
		{
			name: "WithMetaVersion",
			opt:  descriptortest.WithMetaVersion(""),
			check: func(r *require.Assertions, d *descriptor.Descriptor) {
				r.Empty(d.Meta.Version)
			},
		},
		{
			name: "WithProvider",
			opt:  descriptortest.WithProvider("test-provider"),
			check: func(r *require.Assertions, d *descriptor.Descriptor) {
				r.Equal("test-provider", d.Component.Provider.Name)
			},
		},
		{
			name: "WithResources",
			opt:  descriptortest.WithResources(res),
			check: func(r *require.Assertions, d *descriptor.Descriptor) {
				r.Equal([]descriptor.Resource{res}, d.Component.Resources)
			},
		},
		{
			name: "WithResources keeps an empty slice non-nil",
			opt:  descriptortest.WithResources([]descriptor.Resource{}...),
			check: func(r *require.Assertions, d *descriptor.Descriptor) {
				r.NotNil(d.Component.Resources)
				r.Empty(d.Component.Resources)
			},
		},
		{
			name: "WithReferences",
			opt:  descriptortest.WithReferences(ref),
			check: func(r *require.Assertions, d *descriptor.Descriptor) {
				r.Equal([]descriptor.Reference{ref}, d.Component.References)
			},
		},
		{
			name: "WithLabels",
			opt:  descriptortest.WithLabels(label),
			check: func(r *require.Assertions, d *descriptor.Descriptor) {
				r.Equal([]descriptor.Label{label}, d.Component.Labels)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			d := descriptortest.New("ocm.software/app", "1.0.0", tc.opt)
			tc.check(r, d)
			r.Equal("ocm.software/app", d.Component.Name)
			r.Equal("1.0.0", d.Component.Version)
		})
	}
}
