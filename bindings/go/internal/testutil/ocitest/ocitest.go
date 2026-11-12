// Package ocitest builds OCI images and inspects OCI stores in tests.
//
// [SingleLayerImage] returns an OCI image layout tar holding one image with a single layer,
// tagged with every given reference, and an [v1.OCIImage] access for the first one:
//
//	layoutTar, access := ocitest.SingleLayerImage(t, []byte("payload"), "ghcr.io/acme/app:1.0.0")
//
// [OwnershipReferrers] lists the ownership referrers of an image in a store:
//
//	referrers := ocitest.OwnershipReferrers(t, ctx, resolver, access.ImageReference)
//
// The package imports oci/tar, so the in-package tests of oci/tar cannot use it. They build
// their layouts with the ocilayouttest package, which has no OCM imports.
package ocitest

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"
	orasregistry "oras.land/oras-go/v2/registry"

	"ocm.software/open-component-model/bindings/go/oci/looseref"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	"ocm.software/open-component-model/bindings/go/oci/tar"
)

// SingleLayerImage returns an OCI image layout tar with one image manifest. The manifest has
// data as its only layer and an empty JSON config, and is tagged with every ref. The access
// points to the first ref; it is nil when refs is empty.
func SingleLayerImage(t *testing.T, data []byte, refs ...string) ([]byte, *v1.OCIImage) {
	t.Helper()
	r := require.New(t)

	var buf bytes.Buffer
	w, err := tar.NewOCILayoutWriterWithTempFile(&buf, t.TempDir())
	r.NoError(err)

	layer := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageLayer,
		Digest:    digest.FromBytes(data),
		Size:      int64(len(data)),
	}
	r.NoError(w.Push(t.Context(), layer, bytes.NewReader(data)))

	configRaw, err := json.Marshal(map[string]string{})
	r.NoError(err)
	config := ociImageSpecV1.Descriptor{
		MediaType: "application/json",
		Digest:    digest.FromBytes(configRaw),
		Size:      int64(len(configRaw)),
	}
	r.NoError(w.Push(t.Context(), config, bytes.NewReader(configRaw)))

	manifestRaw, err := json.Marshal(ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config:    config,
		Layers:    []ociImageSpecV1.Descriptor{layer},
	})
	r.NoError(err)
	manifest := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromBytes(manifestRaw),
		Size:      int64(len(manifestRaw)),
	}
	r.NoError(w.Push(t.Context(), manifest, bytes.NewReader(manifestRaw)))

	for _, ref := range refs {
		r.NoError(w.Tag(t.Context(), manifest, ref))
	}
	r.NoError(w.Close())

	if len(refs) == 0 {
		return buf.Bytes(), nil
	}
	return buf.Bytes(), &v1.OCIImage{ImageReference: refs[0]}
}

// StoreResolver resolves a reference to the store that holds it. oci.Resolver satisfies it.
// The package declares its own interface because importing the oci package would stop the
// oci package's in-package tests from using ocitest.
type StoreResolver interface {
	StoreForReference(ctx context.Context, reference string) (spec.Store, error)
}

// OwnershipReferrers returns every referrer with artifact type
// [annotations.OwnershipArtifactType] of the subject that reference (a full OCI reference,
// by tag or digest) names. The store must support the OCI Referrers API.
func OwnershipReferrers(t *testing.T, ctx context.Context, resolver StoreResolver, reference string) []ociImageSpecV1.Descriptor {
	t.Helper()
	r := require.New(t)

	store, err := resolver.StoreForReference(ctx, reference)
	r.NoError(err)
	graphStore, ok := store.(content.ReadOnlyGraphStorage)
	r.Truef(ok, "store %T must implement content.ReadOnlyGraphStorage for referrers discovery", store)
	ref, err := looseref.ParseReference(reference)
	r.NoError(err)
	subject, err := store.Resolve(ctx, ref.ReferenceOrTag())
	r.NoError(err)
	referrers, err := orasregistry.Referrers(ctx, graphStore, subject, annotations.OwnershipArtifactType)
	r.NoError(err)
	return referrers
}
