package ocm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestIsLocalBlob(t *testing.T) {
	r := require.New(t)
	r.True(IsLocalBlob(&descriptor.LocalBlob{}))
	r.True(IsLocalBlob(&runtime.Raw{Type: runtime.NewUnversionedType(descriptor.LocalBlobAccessType)}))
	r.True(IsLocalBlob(&runtime.Raw{Type: runtime.NewUnversionedType(descriptor.LegacyLocalBlobAccessType)}))
	r.False(IsLocalBlob(&runtime.Raw{Type: runtime.NewUnversionedType("OCIImage")}))
	r.False(IsLocalBlob(nil))
}

func TestIsExcludedFromSignature(t *testing.T) {
	r := require.New(t)
	r.True(IsExcludedFromSignature(descriptor.NewExcludeFromSignatureDigest()))
	r.False(IsExcludedFromSignature(&descriptor.Digest{
		HashAlgorithm:          "SHA-256",
		NormalisationAlgorithm: "genericBlobDigest/v1",
		Value:                  "abc",
	}))
	r.False(IsExcludedFromSignature(nil))
}

type fakeLocalProvider struct {
	content []byte
	err     error
}

func (f fakeLocalProvider) GetLocalResource(_ context.Context, _, _ string, _ runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	if f.err != nil {
		return nil, nil, f.err
	}
	return inmemory.New(bytes.NewReader(f.content)), nil, nil
}

func sha256Digest(content []byte) *descriptor.Digest {
	sum := sha256.Sum256(content)
	return &descriptor.Digest{
		HashAlgorithm:          "SHA-256",
		NormalisationAlgorithm: "genericBlobDigest/v1",
		Value:                  hex.EncodeToString(sum[:]),
	}
}

func TestVerifyLocalBlob(t *testing.T) {
	content := []byte("hello ocm local blob")

	t.Run("matching digest verifies", func(t *testing.T) {
		r := require.New(t)
		res := &descriptor.Resource{Digest: sha256Digest(content)}
		res.Name = "res"
		err := VerifyLocalBlob(context.Background(), fakeLocalProvider{content: content}, "comp", "1.0.0", res)
		r.NoError(err)
	})

	t.Run("tampered digest fails", func(t *testing.T) {
		r := require.New(t)
		res := &descriptor.Resource{Digest: sha256Digest([]byte("different content"))}
		res.Name = "res"
		err := VerifyLocalBlob(context.Background(), fakeLocalProvider{content: content}, "comp", "1.0.0", res)
		r.Error(err)
	})

	t.Run("missing digest fails", func(t *testing.T) {
		r := require.New(t)
		res := &descriptor.Resource{}
		res.Name = "res"
		err := VerifyLocalBlob(context.Background(), fakeLocalProvider{content: content}, "comp", "1.0.0", res)
		r.ErrorContains(err, "no digest")
	})

	t.Run("provider error propagates", func(t *testing.T) {
		r := require.New(t)
		res := &descriptor.Resource{Digest: sha256Digest(content)}
		res.Name = "res"
		err := VerifyLocalBlob(context.Background(), fakeLocalProvider{err: errors.New("boom")}, "comp", "1.0.0", res)
		r.Error(err)
	})
}
