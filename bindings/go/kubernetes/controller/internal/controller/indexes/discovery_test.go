package indexes

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
)

type recordingFieldIndexer struct {
	calls []indexCall
	err   error
}

type indexCall struct {
	ctx       context.Context
	obj       client.Object
	field     string
	extractor client.IndexerFunc
}

func (r *recordingFieldIndexer) IndexField(
	ctx context.Context,
	obj client.Object,
	field string,
	extractor client.IndexerFunc,
) error {
	r.calls = append(r.calls, indexCall{ctx: ctx, obj: obj, field: field, extractor: extractor})
	return r.err
}

func TestRegisterDiscoveryComponentRef(t *testing.T) {
	r := require.New(t)
	type contextKey string
	ctx := context.WithValue(t.Context(), contextKey("test"), "value")

	first := &recordingFieldIndexer{}
	second := &recordingFieldIndexer{}

	r.NoError(RegisterDiscoveryComponentRef(ctx, first))
	r.NoError(RegisterDiscoveryComponentRef(ctx, second))

	for _, indexer := range []*recordingFieldIndexer{first, second} {
		r.Len(indexer.calls, 1)
		call := indexer.calls[0]
		r.Same(ctx, call.ctx)
		r.IsType(&v1alpha1.Discovery{}, call.obj)
		r.Equal(DiscoveryComponentRef, call.field)
		r.Equal([]string{"component"}, call.extractor(&v1alpha1.Discovery{
			Spec: v1alpha1.DiscoverySpec{
				ComponentRef: corev1.LocalObjectReference{Name: "component"},
			},
		}))
		r.Nil(call.extractor(&v1alpha1.Component{}))
	}
}

func TestRegisterDiscoveryComponentRefWrapsError(t *testing.T) {
	r := require.New(t)
	registrationErr := errors.New("registration failed")
	indexer := &recordingFieldIndexer{err: registrationErr}

	err := RegisterDiscoveryComponentRef(t.Context(), indexer)

	r.ErrorIs(err, registrationErr)
	r.ErrorContains(err, "failed setting discovery component reference index")
	r.Len(indexer.calls, 1)
}
