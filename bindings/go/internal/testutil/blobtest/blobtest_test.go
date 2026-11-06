package blobtest_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/internal/testutil/blobtest"
)

func TestReadAll(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
	}{
		{name: "empty", content: []byte{}},
		{name: "text", content: []byte("hello")},
		{name: "binary", content: []byte{0x00, 0xff, 0x10}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			got := blobtest.ReadAll(t, inmemory.New(bytes.NewReader(tt.content)))
			r.Equal(tt.content, got)
		})
	}
}

func TestFailing(t *testing.T) {
	r := require.New(t)
	want := errors.New("cannot open")

	b := blobtest.Failing(want)

	for range 2 {
		rc, err := b.ReadCloser()
		r.ErrorIs(err, want)
		r.Nil(rc)
	}
}
