package internal

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
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

func ociResource(name, version, imageRef string) descriptor.Resource {
	return descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: name, Version: version},
		},
		Type:     "ociImage",
		Relation: descriptor.ExternalRelation,
		Access: &runtime.Raw{
			Type: runtime.NewVersionedType("OCIImage", "v1"),
			Data: []byte(`{"type":"OCIImage/v1","imageReference":"` + imageRef + `"}`),
		},
	}
}

func uploaderFor(t *testing.T, accessType runtime.Type, targetURL string) *transferv1alpha1.HTTPUploaderConfig {
	t.Helper()
	return &transferv1alpha1.HTTPUploaderConfig{
		Type:      runtime.NewVersionedType(transferv1alpha1.HTTPUploaderConfigType, transferv1alpha1.Version),
		MatchSpec: transferv1alpha1.UploaderMatch{AccessType: accessType},
		TargetURL: targetURL,
		Method:    "PUT",
	}
}

func wgetUploader(t *testing.T, targetURL string) *transferv1alpha1.HTTPUploaderConfig {
	t.Helper()
	return uploaderFor(t, runtime.NewVersionedType("Wget", "v1"), targetURL)
}

func TestBuildGraphDefinition_UploaderMatch_EmitsHTTPStreaming(t *testing.T) {
	r := require.New(t)
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{wgetResource("blob", "1.0.0", "https://source.example/artifacts/blob.tar")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	roots := testTransferRoots("ocm.software/test", "1.0.0", targetRepo, resolver)

	uploaders := []transferv1alpha1.UploaderConfig{wgetUploader(t, `${"https://target.example" + url(resource.access.url).path}`)}
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

	// Target reference carries a CEL expression referencing the source resource inside
	// the shared descriptor environment node (selected by identity), resolved to the
	// concrete URL by the graph runtime at execution time.
	tgtResource := streaming.spec["targetResource"].(map[string]any)
	tgtAccess := tgtResource["access"].(map[string]any)
	assert.Equal(t, "Wget/v1", tgtAccess["type"])
	assert.Equal(t, "PUT", tgtAccess["verb"])
	targetURL := tgtAccess["url"].(string)
	assert.True(t, strings.HasPrefix(targetURL, "${") && strings.HasSuffix(targetURL, "}"),
		"target url must be a CEL expression field, got %q", targetURL)
	assert.Contains(t, targetURL, ".component.resources.filter(", "target url must select the resource from the descriptor node")
	assert.Contains(t, targetURL, `.name == "blob"`, "the filter must select by resource identity")
	assert.Contains(t, targetURL, ")[0].access.url", "resource alias must be rewritten to the selected node path")
	assert.NotContains(t, targetURL, "resource.access", "the bare resource alias must not survive the rewrite")

	// No second copy of the resource is injected; the descriptor environment node the
	// selector targets already carries it under component.resources.
	assert.Nil(t, tgd.Environment.Data["uploads"], "uploader must not inject a separate uploads node")
	node := findResourceInDescriptorEnv(t, tgd, "blob")
	nodeAccess := node["access"].(map[string]any)
	assert.Equal(t, "https://source.example/artifacts/blob.tar", nodeAccess["url"])
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
	uploaders := []transferv1alpha1.UploaderConfig{
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
	assert.Contains(t, targetURL, ".component.resources.filter(", "the bare resource identifier must be rewritten")
	assert.Contains(t, targetURL, ".name", "the rewritten node path must retain the field access")
}

func TestBuildGraphDefinition_UploaderTemplatesHeaders(t *testing.T) {
	r := require.New(t)
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	res := wgetResource("blob", "1.0.0", "https://source.example/artifacts/blob.tar")
	res.Digest = &descriptor.Digest{
		HashAlgorithm:          "SHA-256",
		NormalisationAlgorithm: "genericBlobDigest/v1",
		Value:                  "abc123",
	}
	desc := testDescriptor("ocm.software/test", "1.0.0", []descriptor.Resource{res}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	roots := testTransferRoots("ocm.software/test", "1.0.0", targetRepo, resolver)

	u := wgetUploader(t, `${"https://target.example" + url(resource.access.url).path}`)
	u.Header = map[string][]string{
		// A templated checksum header referencing the source digest.
		"Repr-Digest": {`${"sha-256=:" + resource.digest.value + ":"}`},
		// RFC 9530 Content-Digest: key from the OCM algorithm, value as base64(hex-decoded digest).
		"Content-Digest": {`${contentDigestAlgorithm(resource.digest.hashAlgorithm) + "=:" + base64.encode(hex.decode(resource.digest.value)) + ":"}`},
		// A static literal header passes through unchanged.
		"X-Static": {"literal-value"},
	}
	uploaders := []transferv1alpha1.UploaderConfig{u}

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{CopyMode: transferv1alpha1.CopyModeLocalBlobResources}, uploaders)
	r.NoError(err)

	var header map[string]any
	for i := range tgd.Transformations {
		if tgd.Transformations[i].Type == wgetv1alpha1.HTTPStreamingV1alpha1 {
			tgt := tgd.Transformations[i].Spec.Data["targetResource"].(map[string]any)
			header, _ = tgt["access"].(map[string]any)["header"].(map[string]any)
		}
	}
	r.NotNil(header, "target access must carry the templated header map")

	reprDigest := header["Repr-Digest"].([]any)[0].(string)
	assert.True(t, strings.HasPrefix(reprDigest, "${") && strings.HasSuffix(reprDigest, "}"),
		"templated header value must be a CEL expression field, got %q", reprDigest)
	assert.Contains(t, reprDigest, ".component.resources.filter(", "the resource alias must be rewritten to the node path")
	assert.Contains(t, reprDigest, ".digest.value", "the digest field access must survive the rewrite")
	assert.NotContains(t, reprDigest, "resource.digest", "the bare resource alias must not survive the rewrite")

	contentDigest := header["Content-Digest"].([]any)[0].(string)
	assert.Contains(t, contentDigest, "contentDigestAlgorithm(",
		"the contentDigestAlgorithm() call must be preserved")
	assert.Contains(t, contentDigest, ".digest.hashAlgorithm", "the algorithm-name field access must survive the rewrite")
	assert.Contains(t, contentDigest, "base64.encode(hex.decode(", "the base64/hex conversion must be preserved")
	assert.Contains(t, contentDigest, ".digest.value", "the digest value field access must survive the rewrite")
	assert.Contains(t, contentDigest, ".component.resources.filter(", "the resource alias must be rewritten to the node path")

	xStatic := header["X-Static"].([]any)[0].(string)
	assert.Equal(t, "literal-value", xStatic, "a literal header value must pass through unchanged")

	// The descriptor environment node the selector targets exposes the source digest.
	node := findResourceInDescriptorEnv(t, tgd, "blob")
	digest := node["digest"].(map[string]any)
	assert.Equal(t, "abc123", digest["value"])
	assert.Equal(t, "SHA-256", digest["hashAlgorithm"])
}

func TestBuildGraphDefinition_UploaderUsesLabelValueAndIdentityMatch(t *testing.T) {
	r := require.New(t)
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")

	// The resource carries a "region" label (used as a value in the target URL) and a
	// "tier" extra-identity attribute (used as the matching criterion).
	res := wgetResource("blob", "1.0.0", "https://source.example/artifacts/blob.tar")
	res.Labels = []descriptor.Label{{Name: "region", Value: []byte(`"eu"`)}}
	res.ExtraIdentity = runtime.Identity{"tier": "public"}
	desc := testDescriptor("ocm.software/test", "1.0.0", []descriptor.Resource{res}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	roots := testTransferRoots("ocm.software/test", "1.0.0", targetRepo, resolver)

	// Match on the extra identity; build the target host from the label value, selected
	// by name via a CEL filter (order-independent).
	u := wgetUploader(t, `${"https://" + resource.labels.filter(l, l.name == "region")[0].value + ".example.com" + url(resource.access.url).path}`)
	u.MatchSpec = transferv1alpha1.UploaderMatch{
		AccessType:    runtime.NewVersionedType("Wget", "v1"),
		ExtraIdentity: runtime.Identity{"tier": "public"},
	}
	uploaders := []transferv1alpha1.UploaderConfig{u}

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{CopyMode: transferv1alpha1.CopyModeLocalBlobResources}, uploaders)
	r.NoError(err)

	// The uploader matched (via the extra-identity criterion) → an HTTPStreaming node exists.
	var streamID, targetURL string
	for i := range tgd.Transformations {
		if tgd.Transformations[i].Type == wgetv1alpha1.HTTPStreamingV1alpha1 {
			streamID = tgd.Transformations[i].ID
			tgt := tgd.Transformations[i].Spec.Data["targetResource"].(map[string]any)
			targetURL = tgt["access"].(map[string]any)["url"].(string)
		}
	}
	r.NotEmpty(streamID, "expected an HTTPStreaming transformation (extra-identity match must select the resource)")

	// The label-value expression survived the alias rewrite to the node path.
	assert.Contains(t, targetURL, ".component.resources.filter(", "the resource alias must be rewritten to the node path")
	assert.Contains(t, targetURL, `.labels.filter(l, l.name == "region")[0].value`,
		"the label filter-by-name access must survive the rewrite")
	assert.NotContains(t, targetURL, "resource.labels", "the bare resource alias must not survive the rewrite")

	// The descriptor environment node the selector targets exposes labels as the
	// schema-driven array, resolvable end-to-end.
	node := findResourceInDescriptorEnv(t, tgd, "blob")
	labels := node["labels"].([]any)
	label0 := labels[0].(map[string]any)
	assert.Equal(t, "region", label0["name"])
	assert.Equal(t, "eu", label0["value"])
}

func TestBuildGraphDefinition_UploaderMatchesNonWgetSource(t *testing.T) {
	r := require.New(t)
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{ociResource("image", "1.0.0", "ghcr.io/source/image:1.0.0")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	roots := testTransferRoots("ocm.software/test", "1.0.0", targetRepo, resolver)

	// An OCI source with no URL: the expression references an access-specific field
	// (imageReference) exposed generically under resource.access.
	uploaders := []transferv1alpha1.UploaderConfig{
		uploaderFor(t, runtime.NewVersionedType("OCIImage", "v1"),
			`${"https://mirror.example/" + resource.access.imageReference}`),
	}
	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{CopyMode: transferv1alpha1.CopyModeAllResources}, uploaders)
	r.NoError(err)

	var streamID string
	var targetURL string
	for i := range tgd.Transformations {
		if tgd.Transformations[i].Type == wgetv1alpha1.HTTPStreamingV1alpha1 {
			streamID = tgd.Transformations[i].ID
			tgt := tgd.Transformations[i].Spec.Data["targetResource"].(map[string]any)
			targetURL = tgt["access"].(map[string]any)["url"].(string)
		}
	}
	r.NotEmpty(streamID, "expected an HTTPStreaming transformation for the OCI source")

	// The descriptor environment node the selector targets exposes the generic access field.
	node := findResourceInDescriptorEnv(t, tgd, "image")
	access := node["access"].(map[string]any)
	assert.Equal(t, "ghcr.io/source/image:1.0.0", access["imageReference"],
		"the OCI access field must be exposed under resource.access")
	assert.Equal(t, "ociImage", node["type"], "the resource type must be exposed")

	// The target URL expression references the rewritten node path, not the bare alias.
	assert.Contains(t, targetURL, ".component.resources.filter(", "the resource alias must be rewritten to the node path")
	assert.Contains(t, targetURL, ".access.imageReference", "the access field access must survive the rewrite")
	assert.NotContains(t, targetURL, "resource.access", "the bare resource alias must not survive the rewrite")
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
	uploaders := []transferv1alpha1.UploaderConfig{wgetUploader(t, `"https://target.example" + url(resource.access.url).path`)}
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

	uploaders := []transferv1alpha1.UploaderConfig{wgetUploader(t, `${"https://target.example" + url(resource.access.url).path}`)}

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

// findResourceInDescriptorEnv locates the source resource by name inside the shared
// descriptor environment node (environment.<baseID>.component.resources) that the
// uploader selector targets. It fails the test if no such resource is present.
func findResourceInDescriptorEnv(t *testing.T, tgd *transformv1alpha1.TransformationGraphDefinition, name string) map[string]any {
	t.Helper()
	for _, entry := range tgd.Environment.Data {
		node, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		component, ok := node["component"].(map[string]any)
		if !ok {
			continue
		}
		resources, ok := component["resources"].([]any)
		if !ok {
			continue
		}
		for _, r := range resources {
			res, ok := r.(map[string]any)
			if ok && res["name"] == name {
				return res
			}
		}
	}
	t.Fatalf("resource %q not found in any descriptor environment node", name)
	return nil
}

