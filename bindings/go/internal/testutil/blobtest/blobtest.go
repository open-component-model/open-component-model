// Package blobtest reads blobs in tests.
//
// [ReadAll] opens a [blob.ReadOnlyBlob], reads it to the end and closes it. It fails the test
// on any of the three errors, so the test checks only the content:
//
//	data := blobtest.ReadAll(t, b)
//	r.Equal("hello", string(data))
//
// [Failing] returns a blob that cannot be opened, to test how code handles that error:
//
//	_, err := compression.Compress(blobtest.Failing(errBoom)).ReadCloser()
//	r.ErrorIs(err, errBoom)
package blobtest

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
)

// ReadAll returns the content of b. It fails the test when the blob cannot be opened, read,
// or closed.
func ReadAll(t *testing.T, b blob.ReadOnlyBlob) []byte {
	t.Helper()
	r := require.New(t)

	rc, err := b.ReadCloser()
	r.NoError(err)

	data, err := io.ReadAll(rc)
	r.NoError(err)
	r.NoError(rc.Close())

	return data
}

// Failing returns a blob whose ReadCloser always returns err.
func Failing(err error) blob.ReadOnlyBlob {
	return failingBlob{err: err}
}

type failingBlob struct{ err error }

func (b failingBlob) ReadCloser() (io.ReadCloser, error) {
	return nil, b.err
}
