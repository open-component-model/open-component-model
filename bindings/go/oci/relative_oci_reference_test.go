package oci_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"sync"
	"testing"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// recordingResolver wraps a CTF store resolver. It can override ComponentVersionReference
// with a fixed value (to simulate a URL registry with a subPath and scheme) and records
// every reference StoreForReference is asked for, so a test can assert the absolute image
// reference derived from a relative access.
type recordingResolver struct {
	oci.Resolver
	fixedCVRef string
	mu         sync.Mutex
	refs       []string
}

func (r *recordingResolver) ComponentVersionReference(ctx context.Context, component, version string) string {
	if r.fixedCVRef != "" {
		return r.fixedCVRef
	}
	return r.Resolver.ComponentVersionReference(ctx, component, version)
}

func (r *recordingResolver) StoreForReference(ctx context.Context, reference string) (spec.Store, error) {
	r.mu.Lock()
	r.refs = append(r.refs, reference)
	r.mu.Unlock()
	return r.Resolver.StoreForReference(ctx, reference)
}

func (r *recordingResolver) requested(reference string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ref := range r.refs {
		if ref == reference {
			return true
		}
	}
	return false
}

// stageOCIImage pushes a single-layer OCI image into store at reference and tags it,
// returning the manifest descriptor.
func stageOCIImage(t *testing.T, ctx context.Context, store *ocictf.Store, reference, tag string, data []byte) ociImageSpecV1.Descriptor {
	t.Helper()
	r := require.New(t)
	imgStore, err := store.StoreForReference(ctx, reference)
	r.NoError(err)

	layer := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageLayer, data)
	r.NoError(imgStore.Push(ctx, layer, bytes.NewReader(data)))

	manifest, err := oras.PackManifest(ctx, imgStore, oras.PackManifestVersion1_1, ociImageSpecV1.MediaTypeImageManifest, oras.PackManifestOptions{
		Layers: []ociImageSpecV1.Descriptor{layer},
	})
	r.NoError(err)
	if tag != "" {
		r.NoError(imgStore.Tag(ctx, manifest, tag))
	}
	return manifest
}

// addComponentWithAccess stores a component version carrying one resource with the given
// access, bypassing AddLocalResource (the access is not a local blob).
func addComponentWithAccess(t *testing.T, ctx context.Context, repo *oci.Repository, component, version string, access runtime.Typed) {
	t.Helper()
	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			Provider: descriptor.Provider{Name: "test-provider"},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: component, Version: version},
			},
			Resources: []descriptor.Resource{{
				Relation:    descriptor.LocalRelation,
				ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "image", Version: version}},
				Type:        "ociImage",
				Access:      access,
			}},
		},
	}
	require.NoError(t, repo.AddComponentVersion(ctx, desc))
}

func TestRepository_GetLocalResource_RelativeOCIReference(t *testing.T) {
	const (
		component = "acme.org/compo"
		version   = "v1.0.0"
	)

	// A fixed ComponentVersionReference with a scheme and a subPath proves the resolution
	// base is the registry root: neither the subPath (ocm-prefix) nor the
	// component-descriptors path is prepended, and the scheme is preserved.
	const fixedCVRef = "http://registry.example/ocm-prefix/component-descriptors/acme.org/compo:v1.0.0"

	payload := []byte("relative artifact payload")

	for _, tc := range []struct {
		name         string
		accessType   runtime.Type
		reference    string
		stageRef     string // repository[:tag] staged in the CTF
		stageTag     string
		expectAbsRef string // reference StoreForReference must be asked for
		withDigest   bool   // append @<manifestDigest> to reference (and expectAbsRef)
		dropTag      bool   // digest-only: reference carries no tag
	}{
		{
			name:         "unversioned spelling, tag only",
			accessType:   runtime.NewUnversionedType(v1.RelativeOCIReferenceType),
			reference:    "ocm/value:v2.0",
			stageRef:     "ocm/value:v2.0",
			stageTag:     "v2.0",
			expectAbsRef: "http://registry.example/ocm/value:v2.0",
		},
		{
			name:         "versioned spelling, tag only",
			accessType:   runtime.NewVersionedType(v1.RelativeOCIReferenceType, v1.Version),
			reference:    "ocm/value:v2.0",
			stageRef:     "ocm/value:v2.0",
			stageTag:     "v2.0",
			expectAbsRef: "http://registry.example/ocm/value:v2.0",
		},
		{
			name:         "multi-segment repository",
			accessType:   runtime.NewUnversionedType(v1.RelativeOCIReferenceType),
			reference:    "ocm-prefix/images/app:v1",
			stageRef:     "ocm-prefix/images/app:v1",
			stageTag:     "v1",
			expectAbsRef: "http://registry.example/ocm-prefix/images/app:v1",
		},
		{
			name:         "dotted first segment is a path, not a host",
			accessType:   runtime.NewUnversionedType(v1.RelativeOCIReferenceType),
			reference:    "acme.org/value:v2.0",
			stageRef:     "acme.org/value:v2.0",
			stageTag:     "v2.0",
			expectAbsRef: "http://registry.example/acme.org/value:v2.0",
		},
		{
			name:         "tag and digest preserved",
			accessType:   runtime.NewUnversionedType(v1.RelativeOCIReferenceType),
			reference:    "ocm/value:v2.0",
			stageRef:     "ocm/value:v2.0",
			stageTag:     "v2.0",
			expectAbsRef: "http://registry.example/ocm/value:v2.0",
			withDigest:   true,
		},
		{
			name:         "digest only",
			accessType:   runtime.NewUnversionedType(v1.RelativeOCIReferenceType),
			reference:    "ocm/value",
			stageRef:     "ocm/value",
			stageTag:     "",
			expectAbsRef: "http://registry.example/ocm/value",
			withDigest:   true,
			dropTag:      true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			ctx := t.Context()

			fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
			r.NoError(err)
			store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
			resolver := &recordingResolver{Resolver: store, fixedCVRef: fixedCVRef}
			repo := Repository(t, oci.WithResolver(resolver))

			manifest := stageOCIImage(t, ctx, store, "http://registry.example/"+tc.stageRef, tc.stageTag, payload)

			reference := tc.reference
			expectAbsRef := tc.expectAbsRef
			if tc.withDigest {
				reference = reference + "@" + manifest.Digest.String()
				expectAbsRef = expectAbsRef + "@" + manifest.Digest.String()
			}

			access := &v1.RelativeOCIReference{Type: tc.accessType, Reference: reference}
			addComponentWithAccess(t, ctx, repo, component, version, access)

			blb, res, err := repo.GetLocalResource(ctx, component, version, runtime.Identity{"name": "image"})
			r.NoError(err)
			r.NotNil(blb)

			// Resolution base is the registry root: subPath and component-descriptors are
			// not prepended, the scheme is preserved, the relative reference rides along.
			r.True(resolver.requested(expectAbsRef),
				"expected StoreForReference(%q); got %v", expectAbsRef, resolver.refs)

			// The descriptor is not mutated: the returned resource keeps its (unconverted)
			// relative access and never surfaces the registry host or the CTF sentinel.
			r.True(ociaccess.IsRelativeOCIReference(res.Access),
				"returned access must still be a relativeOciReference, got %T", res.Access)
			var rel v1.RelativeOCIReference
			r.NoError(ociaccess.Scheme.Convert(res.Access, &rel))
			r.Equal(reference, rel.Reference)
			r.NotContains(rel.Reference, "ctf.ocm.software")
			r.NotContains(rel.Reference, "registry.example")

			// The materialized blob is a valid OCI layout tar holding the staged manifest.
			rc, err := blb.ReadCloser()
			r.NoError(err)
			defer func() { r.NoError(rc.Close()) }()
			buf, err := io.ReadAll(rc)
			r.NoError(err)
			layout, err := tar.ReadOCILayout(ctx, inmemory.New(bytes.NewReader(buf)))
			r.NoError(err)
			t.Cleanup(func() { r.NoError(layout.Close()) })
			r.NotEmpty(layout.Index.Manifests)
			found := false
			for _, m := range layout.Index.Manifests {
				if m.Digest == manifest.Digest {
					found = true
				}
			}
			r.True(found, "materialized layout must contain the staged manifest %s", manifest.Digest)
		})
	}
}

// TestRepository_GetLocalResource_RelativeOCIReference_CTF proves the best-effort CTF read
// edge case: an archive that genuinely holds the tagged artifact resolves through the same
// arm, and the CTF sentinel registry never surfaces in the returned access.
func TestRepository_GetLocalResource_RelativeOCIReference_CTF(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()
	const (
		component = "acme.org/compo"
		version   = "v1.0.0"
	)

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo := Repository(t, ocictf.WithCTF(store))

	manifest := stageOCIImage(t, ctx, store, "ctf.ocm.software/ocm/value:v2.0", "v2.0", []byte("ctf relative payload"))

	access := &v1.RelativeOCIReference{
		Type:      runtime.NewUnversionedType(v1.RelativeOCIReferenceType),
		Reference: "ocm/value:v2.0",
	}
	addComponentWithAccess(t, ctx, repo, component, version, access)

	blb, res, err := repo.GetLocalResource(ctx, component, version, runtime.Identity{"name": "image"})
	r.NoError(err)
	r.NotNil(blb)

	r.True(ociaccess.IsRelativeOCIReference(res.Access))
	var rel v1.RelativeOCIReference
	r.NoError(ociaccess.Scheme.Convert(res.Access, &rel))
	r.Equal("ocm/value:v2.0", rel.Reference)
	r.NotContains(rel.Reference, "ctf.ocm.software")

	rc, err := blb.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(rc.Close()) }()
	buf, err := io.ReadAll(rc)
	r.NoError(err)
	layout, err := tar.ReadOCILayout(ctx, inmemory.New(bytes.NewReader(buf)))
	r.NoError(err)
	t.Cleanup(func() { r.NoError(layout.Close()) })
	found := false
	for _, m := range layout.Index.Manifests {
		if m.Digest == manifest.Digest {
			found = true
		}
	}
	r.True(found)
}
