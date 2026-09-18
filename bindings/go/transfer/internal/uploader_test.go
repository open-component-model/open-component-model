package internal

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	wgetv1alpha1 "ocm.software/open-component-model/bindings/go/wget/transformation/spec/v1alpha1"
)

func wgetResource(name, version, url string) descriptor.Resource {
	return descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: name, Version: version},
		},
		Type:     "blob",
		Relation: descriptor.ExternalRelation,
		Access: &runtime.Raw{
			Type: runtime.NewVersionedType("Wget", "v1"),
			Data: []byte(`{"type":"Wget/v1","url":"` + url + `"}`),
		},
	}
}

func wgetUploader(t *testing.T, targetURL string) *transferv1alpha1.UploaderConfig {
	t.Helper()
	stream, err := runtime.UnstructuredFromMixedData(map[string]any{
		"type":      "HTTPStreaming/v1alpha1",
		"targetURL": targetURL,
		"method":    "PUT",
	})
	require.NoError(t, err)
	var raw runtime.Raw
	require.NoError(t, runtime.NewScheme(runtime.WithAllowUnknown()).Convert(stream, &raw))
	return &transferv1alpha1.UploaderConfig{
		Type:   runtime.NewVersionedType(transferv1alpha1.UploaderConfigType, transferv1alpha1.Version),
		Match:  transferv1alpha1.UploaderMatch{AccessType: runtime.NewVersionedType("Wget", "v1")},
		Stream: &raw,
	}
}

func TestBuildGraphDefinition_UploaderMatch_EmitsHTTPStreaming(t *testing.T) {
	r := require.New(t)
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{wgetResource("blob", "1.0.0", "https://source.example/artifacts/blob.tar")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	roots := testTransferRoots("ocm.software/test", "1.0.0", targetRepo, resolver)

	uploaders := []*transferv1alpha1.UploaderConfig{wgetUploader(t, `${"https://target.example" + resource.access.path}`)}
	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{CopyMode: transferv1alpha1.CopyModeLocalBlobResources}, uploaders)
	r.NoError(err)

	// Exactly one HTTPStreaming node for the resource, plus the component-version upload.
	var streaming *struct {
		spec  map[string]any
		id    string
		label string
	}
	var sawDownloadWget bool
	for i := range tgd.Transformations {
		tr := tgd.Transformations[i]
		switch tr.Type {
		case wgetv1alpha1.HTTPStreamingV1alpha1:
			streaming = &struct {
				spec  map[string]any
				id    string
				label string
			}{spec: tr.Spec.Data, id: tr.ID, label: tr.Label}
		case wgetv1alpha1.DownloadWgetResourceV1alpha1:
			sawDownloadWget = true
		}
	}
	r.NotNil(streaming, "expected an HTTPStreaming transformation")
	assert.Contains(t, streaming.id, "Upload")
	assert.False(t, sawDownloadWget, "uploader path must not emit a DownloadWgetResource node")

	// Source reference preserves the original wget URL.
	srcAccess := streaming.spec["resource"].(map[string]any)["access"].(map[string]any)
	assert.Equal(t, "https://source.example/artifacts/blob.tar", srcAccess["url"])

	// Target reference carries a CEL expression referencing the injected source node,
	// resolved to the concrete URL by the graph runtime at execution time.
	tgtResource := streaming.spec["targetResource"].(map[string]any)
	tgtAccess := tgtResource["access"].(map[string]any)
	assert.Equal(t, "Wget/v1", tgtAccess["type"])
	assert.Equal(t, "PUT", tgtAccess["verb"])
	targetURL := tgtAccess["url"].(string)
	assert.True(t, strings.HasPrefix(targetURL, "${") && strings.HasSuffix(targetURL, "}"),
		"target url must be a CEL expression field, got %q", targetURL)
	assert.Contains(t, targetURL, "environment."+"uploads"+".", "target url must reference the injected upload node")
	assert.Contains(t, targetURL, ".access.path", "resource alias must be rewritten to the node path")
	assert.NotContains(t, targetURL, "resource.access", "the bare resource alias must not survive the rewrite")

	// The injected environment node exposes the source resource for the CEL expression.
	uploads := tgd.Environment.Data["uploads"].(map[string]any)
	node := uploads[streaming.id].(map[string]any)
	nodeAccess := node["access"].(map[string]any)
	assert.Equal(t, "/artifacts/blob.tar", nodeAccess["path"])
	assert.Equal(t, "blob", node["name"])

	// ADR 28: the node carries a human-readable label; the host is parsed from the
	// leading string literal of the targetURL expression.
	assert.Equal(t, "test@1.0.0 [Stream blob to target.example]", streaming.label)
}

func TestBuildGraphDefinition_NoUploader_KeepsDownloadWgetPath(t *testing.T) {
	r := require.New(t)
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{wgetResource("blob", "1.0.0", "https://source.example/artifacts/blob.tar")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	roots := testTransferRoots("ocm.software/test", "1.0.0", targetRepo, resolver)

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{CopyMode: transferv1alpha1.CopyModeAllResources}, nil)
	r.NoError(err)

	var sawDownloadWget, sawStreaming bool
	for i := range tgd.Transformations {
		switch tgd.Transformations[i].Type {
		case wgetv1alpha1.DownloadWgetResourceV1alpha1:
			sawDownloadWget = true
		case wgetv1alpha1.HTTPStreamingV1alpha1:
			sawStreaming = true
		}
	}
	assert.True(t, sawDownloadWget, "without an uploader the wget resource must use the DownloadWgetResource path")
	assert.False(t, sawStreaming, "no HTTPStreaming node should be emitted without an uploader")
}

func TestBuildGraphDefinition_UploaderPreservesResourceInStringLiteral(t *testing.T) {
	r := require.New(t)
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{wgetResource("blob", "1.0.0", "https://source.example/artifacts/blob.tar")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	roots := testTransferRoots("ocm.software/test", "1.0.0", targetRepo, resolver)

	// The literal path segment "resource" must survive; only the bare identifier is rewritten.
	uploaders := []*transferv1alpha1.UploaderConfig{
		wgetUploader(t, `${"https://uploads.example/resource/" + resource.name}`),
	}
	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{CopyMode: transferv1alpha1.CopyModeLocalBlobResources}, uploaders)
	r.NoError(err)

	var targetURL string
	for i := range tgd.Transformations {
		if tgd.Transformations[i].Type == wgetv1alpha1.HTTPStreamingV1alpha1 {
			tgt := tgd.Transformations[i].Spec.Data["targetResource"].(map[string]any)
			targetURL = tgt["access"].(map[string]any)["url"].(string)
		}
	}
	r.NotEmpty(targetURL)
	// The string literal keeps the word "resource"; the identifier before ".name" is rewritten.
	assert.Contains(t, targetURL, `"https://uploads.example/resource/"`,
		"the literal path segment must not be rewritten")
	assert.Contains(t, targetURL, "environment.uploads.", "the bare resource identifier must be rewritten")
	assert.Contains(t, targetURL, ".name", "the rewritten node path must retain the field access")
}

func TestBuildGraphDefinition_UploaderRejectsUnwrappedTargetURL(t *testing.T) {
	r := require.New(t)
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{wgetResource("blob", "1.0.0", "https://source.example/artifacts/blob.tar")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	roots := testTransferRoots("ocm.software/test", "1.0.0", targetRepo, resolver)

	// A bare CEL expression without the ${...} delimiters must be rejected.
	uploaders := []*transferv1alpha1.UploaderConfig{wgetUploader(t, `"https://target.example" + resource.access.path`)}
	_, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{CopyMode: transferv1alpha1.CopyModeLocalBlobResources}, uploaders)
	r.Error(err)
	assert.Contains(t, err.Error(), "must be a single CEL expression wrapped in ${...}")
}

func TestBuildGraphDefinition_DeterministicOrder(t *testing.T) {
	r := require.New(t)
	targetRepo := testOCIRepo("ghcr.io/target")
	roots := map[string]TransferRoot{}
	names := []string{"ocm.software/a", "ocm.software/b", "ocm.software/c"}
	multiEntries := map[string]struct {
		spec runtime.Typed
		desc *descriptor.Descriptor
	}{}
	sourceRepo := testOCIRepo("ghcr.io/source")
	for _, n := range names {
		key := n + ":1.0.0"
		d := testDescriptor(n, "1.0.0",
			[]descriptor.Resource{wgetResource("blob", "1.0.0", "https://source.example/"+n+"/blob.tar")}, nil)
		multiEntries[key] = struct {
			spec runtime.Typed
			desc *descriptor.Descriptor
		}{spec: sourceRepo, desc: d}
	}
	res := testMultiResolver(multiEntries)
	for _, n := range names {
		key := n + ":1.0.0"
		roots[key] = TransferRoot{RootComponentKey: key, Targets: []runtime.Typed{targetRepo}, SourceResolver: res}
	}

	uploaders := []*transferv1alpha1.UploaderConfig{wgetUploader(t, `${"https://target.example" + resource.access.path}`)}

	first, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{CopyMode: transferv1alpha1.CopyModeAllResources}, uploaders)
	r.NoError(err)
	for i := 0; i < 20; i++ {
		next, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{CopyMode: transferv1alpha1.CopyModeAllResources}, uploaders)
		r.NoError(err)
		r.Equal(len(first.Transformations), len(next.Transformations))
		for j := range first.Transformations {
			assert.Equal(t, first.Transformations[j].ID, next.Transformations[j].ID,
				"transformation order must be deterministic across runs (index %d, run %d)", j, i)
		}
	}
}
