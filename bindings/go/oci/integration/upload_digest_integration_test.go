package integration_test

import (
	"bytes"
	_ "crypto/sha512" // Register SHA-512 for the non-OCM OCI root regression.
	"encoding/json"
	"os"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	accessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ocistream "ocm.software/open-component-model/bindings/go/oci/stream"
	ocitar "ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Test_Integration_OCIUpload_ConflictingAccessDigest(t *testing.T) {
	for _, method := range []string{"resource", "stream", "source"} {
		for _, tagged := range []bool{true, false} {
			states := []string{"absent", "incomplete", "correct"}
			if method == "source" {
				states = []string{"absent"}
			}
			for _, state := range states {
				form := "digest-only"
				if tagged {
					form = "tag-and-digest"
				}
				t.Run(method+"/"+form+"/"+state, func(t *testing.T) {
					runDigestUpload(t, digestUploadCase{
						method: method, algorithm: digest.SHA256, digestState: state,
						pin: "conflicting", tagged: tagged, reject: true, errorContains: "target access digest mismatch",
					})
				})
			}
		}
	}
}

func Test_Integration_OCIUpload_DigestControls(t *testing.T) {
	for _, method := range []string{"resource", "stream", "source"} {
		for _, tc := range []struct {
			name   string
			pin    string
			tagged bool
		}{
			{name: "sha256-tag", tagged: true},
			{name: "matching-tag-and-digest", pin: "matching", tagged: true},
			{name: "matching-digest-only", pin: "matching"},
		} {
			t.Run(method+"/"+tc.name, func(t *testing.T) {
				runDigestUpload(t, digestUploadCase{
					method: method, algorithm: digest.SHA256, digestState: "absent",
					pin: tc.pin, tagged: tc.tagged,
				})
			})
		}
	}
}

func Test_Integration_OCIUpload_SHA512Root(t *testing.T) {
	for _, method := range []string{"resource", "stream"} {
		for _, state := range []string{"absent", "incomplete"} {
			t.Run(method+"/"+state, func(t *testing.T) {
				runDigestUpload(t, digestUploadCase{
					method: method, algorithm: digest.SHA512, digestState: state,
					tagged: true, reject: true, errorContains: "unknown algorithm",
				})
			})
		}
	}
	// Sources have no OCM digest to populate; SHA-512 is valid at the OCI boundary.
	t.Run("source/allowed", func(t *testing.T) {
		runDigestUpload(t, digestUploadCase{
			method: "source", algorithm: digest.SHA512, tagged: true,
		})
	})
}

type digestUploadCase struct {
	method        string
	algorithm     digest.Algorithm
	digestState   string
	pin           string
	tagged        bool
	reject        bool
	errorContains string
}

func runDigestUpload(t *testing.T, tc digestUploadCase) {
	t.Helper()
	r := require.New(t)
	ctx := t.Context()
	const repository = "example.org/digest-upload"
	const tag = "stable"

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	resolver := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(t.TempDir()))
	r.NoError(err)
	dst, err := resolver.StoreForReference(ctx, repository)
	r.NoError(err)

	oldBlob, oldRoot := digestUploadLayout(t, digest.SHA256, "existing")
	oldStore, err := ocitar.ReadOCILayout(ctx, oldBlob)
	r.NoError(err)
	t.Cleanup(func() { require.NoError(t, oldStore.Close()) })
	r.NoError(oras.CopyGraph(ctx, oldStore, dst, oldRoot, oras.DefaultCopyGraphOptions))
	r.NoError(dst.Tag(ctx, oldRoot, tag))

	input, root := digestUploadLayout(t, tc.algorithm, "replacement")
	exists, err := dst.Exists(ctx, root)
	r.NoError(err)
	r.False(exists, "fixture must start without the replacement root")
	ref := repository
	if tc.tagged {
		ref += ":" + tag
	}
	switch tc.pin {
	case "conflicting":
		ref += "@" + oldRoot.Digest.String()
	case "matching":
		ref += "@" + root.Digest.String()
	}
	access := &accessv1.OCIImage{
		Type: runtime.NewVersionedType(accessv1.OCIImageType, accessv1.Version), ImageReference: ref,
	}
	var resultAccess runtime.Typed
	switch tc.method {
	case "source":
		src := &descriptor.Source{
			ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "image", Version: "1.0.0"}},
			Type:        "ociImage", Access: access,
		}
		before := src.DeepCopy()
		var result *descriptor.Source
		result, err = repo.UploadSource(ctx, src, input)
		r.Equal(before, src, "upload must not mutate the caller's source")
		if result != nil {
			resultAccess = result.Access
		}
		if tc.reject {
			assert.Nil(t, result, "rejected upload must not return a source")
		}
	case "resource", "stream":
		res := &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "image", Version: "1.0.0"}},
			Relation:    descriptor.LocalRelation, Type: "ociImage", Access: access,
		}
		if tc.digestState == "incomplete" || tc.digestState == "correct" {
			res.Digest = &descriptor.Digest{HashAlgorithm: "SHA-256", Value: root.Digest.Encoded()}
			if tc.digestState == "correct" {
				res.Digest.NormalisationAlgorithm = "ociArtifactDigest/v1"
			}
		}
		before := res.DeepCopy()
		var result *descriptor.Resource
		if tc.method == "stream" {
			store, readErr := ocitar.ReadOCILayout(ctx, input)
			r.NoError(readErr)
			t.Cleanup(func() { require.NoError(t, store.Close()) })
			result, err = repo.UploadResourceStream(ctx, res, &ocistream.OCIResourceStream{
				ReadOnlyGraphStorage: store, Descriptor: root,
			})
		} else {
			result, err = repo.UploadResource(ctx, res, input)
		}
		r.Equal(before, res, "upload must not mutate the caller's resource")
		if result != nil {
			resultAccess = result.Access
		}
		if tc.reject {
			assert.Nil(t, result, "rejected upload must not return a resource")
		} else {
			r.NoError(err)
			r.NotNil(result)
			r.Equal(&descriptor.Digest{
				HashAlgorithm: "SHA-256", NormalisationAlgorithm: "ociArtifactDigest/v1", Value: root.Digest.Encoded(),
			}, result.Digest)
		}
	default:
		t.Fatalf("unknown upload method %q", tc.method)
	}

	if tc.reject {
		assert.Error(t, err, "conflicting or unsupported digest must be rejected before writes")
		if tc.errorContains != "" {
			assert.ErrorContains(t, err, tc.errorContains)
		}
	} else {
		r.NoError(err)
		r.NotNil(resultAccess)
		if tc.pin != "" {
			r.Equal(ref, resultAccess.(*accessv1.OCIImage).ImageReference, "matching pin must be preserved")
		}
	}
	resolved, err := dst.Resolve(ctx, tag)
	r.NoError(err)
	wantTag := oldRoot.Digest
	if !tc.reject && tc.tagged {
		wantTag = root.Digest
	}
	assert.Equal(t, wantTag, resolved.Digest, "existing destination tag must remain unchanged on rejection or untagged upload")
	exists, err = dst.Exists(ctx, root)
	r.NoError(err)
	assert.Equal(t, !tc.reject, exists, "rejected upload must not write the replacement root")
}

// Both upload paths consume the same real OCI layout, including a SHA-512 root
// when requested; only the root algorithm changes, not the config blob's digest.
func digestUploadLayout(t *testing.T, algorithm digest.Algorithm, marker string) (blob.ReadOnlyBlob, ocispec.Descriptor) {
	t.Helper()
	r := require.New(t)
	manifest, err := json.Marshal(ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2}, MediaType: ocispec.MediaTypeImageManifest,
		ArtifactType: "application/vnd.ocm.test.digest-upload",
		Config:       ocispec.DescriptorEmptyJSON, Layers: []ocispec.Descriptor{},
		Annotations: map[string]string{"test-content": marker},
	})
	r.NoError(err)
	root := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageManifest, Digest: algorithm.FromBytes(manifest), Size: int64(len(manifest)),
	}
	var buf bytes.Buffer
	w, err := ocitar.NewOCILayoutWriterWithTempFile(&buf, t.TempDir())
	r.NoError(err)
	t.Cleanup(func() { require.NoError(t, w.Close()) })
	r.NoError(w.Push(t.Context(), ocispec.DescriptorEmptyJSON, bytes.NewReader(ocispec.DescriptorEmptyJSON.Data)))
	r.NoError(w.Push(t.Context(), root, bytes.NewReader(manifest)))
	r.NoError(w.Close())
	return inmemory.New(bytes.NewReader(buf.Bytes())), root
}
