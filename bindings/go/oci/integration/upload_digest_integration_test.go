package integration_test

import (
	"bytes"
	"crypto"
	_ "crypto/sha512" // Make SHA-384 and SHA-512 available for OCI root controls.
	"encoding/json"
	"os"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"

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
		for _, state := range []string{"absent", "incomplete", "correct"} {
			t.Run(method+"/"+state, func(t *testing.T) {
				runDigestUpload(t, digestUploadCase{
					method: method, algorithm: digest.SHA512, digestState: state,
					tagged: true,
				})
			})
		}
	}

	t.Run("source/allowed", func(t *testing.T) {
		runDigestUpload(t, digestUploadCase{
			method: "source", algorithm: digest.SHA512, tagged: true,
		})
	})
}

func Test_Integration_OCIUpload_UnsupportedOCMRootAlgorithm(t *testing.T) {
	// SHA-384 is opt-in in go-digest and deliberately absent from OCM's mapping.
	digest.RegisterAlgorithm(digest.SHA384, crypto.SHA384)
	for _, method := range []string{"resource", "stream"} {
		for _, state := range []string{"absent", "incomplete", "correct"} {
			t.Run(method+"/"+state, func(t *testing.T) {
				runDigestUpload(t, digestUploadCase{
					method: method, algorithm: digest.SHA384, digestState: state,
					tagged: true, reject: true, errorContains: "unknown algorithm",
				})
			})
		}
	}
	// The transport accepts SHA-384, but OCM resource digest conversion does not.
	// Sources have no OCM digest, so they isolate that conversion from graph copying.
	t.Run("source/allowed", func(t *testing.T) {
		runDigestUpload(t, digestUploadCase{
			method: "source", algorithm: digest.SHA384, tagged: true,
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

	oldBlob, oldRoot, oldConfig := digestUploadLayout(t, digest.SHA256, "existing")
	oldStore, err := ocitar.ReadOCILayout(ctx, oldBlob)
	r.NoError(err)
	t.Cleanup(func() { require.NoError(t, oldStore.Close()) })
	r.NoError(oras.CopyGraph(ctx, oldStore, dst, oldRoot, oras.DefaultCopyGraphOptions))
	r.NoError(dst.Tag(ctx, oldRoot, tag))

	oldContent := make(map[digest.Digest][]byte)
	for _, desc := range []ocispec.Descriptor{oldRoot, oldConfig} {
		oldContent[desc.Digest], err = content.FetchAll(ctx, dst, desc)
		r.NoError(err)
	}

	input, root, config := digestUploadLayout(t, tc.algorithm, "replacement")
	for _, desc := range []ocispec.Descriptor{root, config} {
		exists, existsErr := dst.Exists(ctx, desc)
		r.NoError(existsErr)
		r.False(exists, "fixture must start without replacement content: %s", desc.Digest)
	}
	hashAlgorithm, ok := map[digest.Algorithm]string{
		digest.SHA256: "SHA-256", digest.SHA512: "SHA-512", digest.SHA384: "SHA-384",
	}[tc.algorithm]
	r.True(ok, "fixture must specify an OCM hash algorithm name")
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
			res.Digest = &descriptor.Digest{HashAlgorithm: hashAlgorithm, Value: root.Digest.Encoded()}
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
				HashAlgorithm: hashAlgorithm, NormalisationAlgorithm: "ociArtifactDigest/v1", Value: root.Digest.Encoded(),
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
	for _, desc := range []ocispec.Descriptor{root, config} {
		exists, existsErr := dst.Exists(ctx, desc)
		r.NoError(existsErr)
		assert.Equal(t, !tc.reject, exists, "rejected upload must not write replacement content: %s", desc.Digest)
	}
	for _, desc := range []ocispec.Descriptor{oldRoot, oldConfig} {
		data, fetchErr := content.FetchAll(ctx, dst, desc)
		r.NoError(fetchErr)
		assert.Equal(t, oldContent[desc.Digest], data, "existing destination content must remain intact")
	}
}

// All upload paths consume the same real OCI layout. Only the root algorithm
// varies; a unique SHA-256 config detects child writes before root rejection.
func digestUploadLayout(t *testing.T, algorithm digest.Algorithm, marker string) (blob.ReadOnlyBlob, ocispec.Descriptor, ocispec.Descriptor) {
	t.Helper()
	r := require.New(t)
	r.True(algorithm.Available(), "fixture root algorithm must be available")
	configData, err := json.Marshal(map[string]string{"test-content": marker})
	r.NoError(err)
	config := content.NewDescriptorFromBytes(ocispec.MediaTypeImageConfig, configData)
	manifest, err := json.Marshal(ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2}, MediaType: ocispec.MediaTypeImageManifest,
		ArtifactType: "application/vnd.ocm.test.digest-upload",
		Config:       config, Layers: []ocispec.Descriptor{},
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
	r.NoError(w.Push(t.Context(), config, bytes.NewReader(configData)))
	r.NoError(w.Push(t.Context(), root, bytes.NewReader(manifest)))
	r.NoError(w.Close())
	return inmemory.New(bytes.NewReader(buf.Bytes())), root, config
}
