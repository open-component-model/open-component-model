package internal

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
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
		Match:     transferv1alpha1.UploaderMatch{AccessType: accessType},
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

	uploaders := []*transferv1alpha1.HTTPUploaderConfig{wgetUploader(t, `${"https://target.example" + url(resource.access.url).path}`)}
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
	assert.Contains(t, targetURL, ".access.url", "resource alias must be rewritten to the node path")
	assert.NotContains(t, targetURL, "resource.access", "the bare resource alias must not survive the rewrite")

	// The injected environment node exposes the source resource for the CEL expression.
	uploads := tgd.Environment.Data["uploads"].(map[string]any)
	node := uploads[streaming.id].(map[string]any)
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
	uploaders := []*transferv1alpha1.HTTPUploaderConfig{
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
	uploaders := []*transferv1alpha1.HTTPUploaderConfig{u}

	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{CopyMode: transferv1alpha1.CopyModeLocalBlobResources}, uploaders)
	r.NoError(err)

	var header map[string]any
	var streamID string
	for i := range tgd.Transformations {
		if tgd.Transformations[i].Type == wgetv1alpha1.HTTPStreamingV1alpha1 {
			streamID = tgd.Transformations[i].ID
			tgt := tgd.Transformations[i].Spec.Data["targetResource"].(map[string]any)
			header, _ = tgt["access"].(map[string]any)["header"].(map[string]any)
		}
	}
	r.NotNil(header, "target access must carry the templated header map")

	reprDigest := header["Repr-Digest"].([]any)[0].(string)
	assert.True(t, strings.HasPrefix(reprDigest, "${") && strings.HasSuffix(reprDigest, "}"),
		"templated header value must be a CEL expression field, got %q", reprDigest)
	assert.Contains(t, reprDigest, "environment.uploads.", "the resource alias must be rewritten to the node path")
	assert.Contains(t, reprDigest, ".digest.value", "the digest field access must survive the rewrite")
	assert.NotContains(t, reprDigest, "resource.digest", "the bare resource alias must not survive the rewrite")

	contentDigest := header["Content-Digest"].([]any)[0].(string)
	assert.Contains(t, contentDigest, "contentDigestAlgorithm(",
		"the contentDigestAlgorithm() call must be preserved")
	assert.Contains(t, contentDigest, ".digest.hashAlgorithm", "the algorithm-name field access must survive the rewrite")
	assert.Contains(t, contentDigest, "base64.encode(hex.decode(", "the base64/hex conversion must be preserved")
	assert.Contains(t, contentDigest, ".digest.value", "the digest value field access must survive the rewrite")
	assert.Contains(t, contentDigest, "environment.uploads.", "the resource alias must be rewritten to the node path")

	xStatic := header["X-Static"].([]any)[0].(string)
	assert.Equal(t, "literal-value", xStatic, "a literal header value must pass through unchanged")

	// The injected node exposes the source digest for the header expression.
	node := tgd.Environment.Data["uploads"].(map[string]any)[streamID].(map[string]any)
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
	u.Match = transferv1alpha1.UploaderMatch{
		AccessType:    runtime.NewVersionedType("Wget", "v1"),
		ExtraIdentity: runtime.Identity{"tier": "public"},
	}
	uploaders := []*transferv1alpha1.HTTPUploaderConfig{u}

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
	assert.Contains(t, targetURL, "environment.uploads.", "the resource alias must be rewritten to the node path")
	assert.Contains(t, targetURL, `.labels.filter(l, l.name == "region")[0].value`,
		"the label filter-by-name access must survive the rewrite")
	assert.NotContains(t, targetURL, "resource.labels", "the bare resource alias must not survive the rewrite")

	// The injected node exposes labels as the schema-driven array, resolvable end-to-end.
	node := tgd.Environment.Data["uploads"].(map[string]any)[streamID].(map[string]any)
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
	uploaders := []*transferv1alpha1.HTTPUploaderConfig{
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

	// The generic access field is exposed on the injected node.
	uploads := tgd.Environment.Data["uploads"].(map[string]any)
	node := uploads[streamID].(map[string]any)
	access := node["access"].(map[string]any)
	assert.Equal(t, "ghcr.io/source/image:1.0.0", access["imageReference"],
		"the OCI access field must be exposed under resource.access")
	assert.Equal(t, "ociImage", node["type"], "the resource type must be exposed")

	// The target URL expression references the rewritten node path, not the bare alias.
	assert.Contains(t, targetURL, "environment.uploads.", "the resource alias must be rewritten to the node path")
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
	uploaders := []*transferv1alpha1.HTTPUploaderConfig{wgetUploader(t, `"https://target.example" + url(resource.access.url).path`)}
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

	uploaders := []*transferv1alpha1.HTTPUploaderConfig{wgetUploader(t, `${"https://target.example" + url(resource.access.url).path}`)}

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

// resourceWithIdentity builds a v2 Wget resource carrying extra identity attributes
// so the matcher's name/version/extraIdentity selection can be exercised.
func resourceWithIdentity(name, version string, extra map[string]string) descriptorv2.Resource {
	res := descriptorv2.Resource{
		ElementMeta: descriptorv2.ElementMeta{
			ObjectMeta: descriptorv2.ObjectMeta{Name: name, Version: version},
		},
		Type:     "blob",
		Relation: descriptorv2.ExternalRelation,
		Access: &runtime.Raw{
			Type: runtime.NewVersionedType("Wget", "v1"),
			Data: []byte(`{"type":"Wget/v1","url":"https://source.example/` + name + `"}`),
		},
	}
	if len(extra) > 0 {
		res.ExtraIdentity = runtime.Identity{}
		for k, v := range extra {
			res.ExtraIdentity[k] = v
		}
	}
	return res
}

func TestMatchUploader(t *testing.T) {
	wget := runtime.NewVersionedType("Wget", "v1")
	// Rules are named so assertions can identify which one won.
	rule := func(name string, m transferv1alpha1.UploaderMatch) *transferv1alpha1.HTTPUploaderConfig {
		u := uploaderFor(t, m.AccessType, "${\""+name+"\"}")
		u.Match = m
		return u
	}

	byAccess := rule("byAccess", transferv1alpha1.UploaderMatch{AccessType: wget})
	byName := rule("byName", transferv1alpha1.UploaderMatch{AccessType: wget, Name: "docs"})
	byVersion := rule("byVersion", transferv1alpha1.UploaderMatch{AccessType: wget, Version: "1.0.0"})
	byArch := rule("byArch", transferv1alpha1.UploaderMatch{AccessType: wget, ExtraIdentity: runtime.Identity{"architecture": "arm64"}})
	byNameAndArch := rule("byNameAndArch", transferv1alpha1.UploaderMatch{AccessType: wget, Name: "docs", ExtraIdentity: runtime.Identity{"architecture": "arm64"}})
	byVersionedAccess := rule("byVersionedAccess", transferv1alpha1.UploaderMatch{AccessType: runtime.NewVersionedType("Wget", "v2")})
	ociOnly := rule("ociOnly", transferv1alpha1.UploaderMatch{AccessType: runtime.NewVersionedType("OCIImage", "v1")})
	byUnversioned := rule("byUnversioned", transferv1alpha1.UploaderMatch{AccessType: runtime.NewUnversionedType("Wget")})

	// helper to name the winning rule via its (single) targetURL literal.
	won := func(t *testing.T, u *transferv1alpha1.HTTPUploaderConfig) string {
		t.Helper()
		if u == nil {
			return ""
		}
		return strings.Trim(u.TargetURL, "${\"}")
	}

	tests := []struct {
		name      string
		uploaders []*transferv1alpha1.HTTPUploaderConfig
		resource  descriptorv2.Resource
		want      string // winning rule name, "" for no match
	}{
		{
			name:      "access type only",
			uploaders: []*transferv1alpha1.HTTPUploaderConfig{byAccess},
			resource:  resourceWithIdentity("anything", "1.0.0", nil),
			want:      "byAccess",
		},
		{
			name:      "name constraint selects only the named resource",
			uploaders: []*transferv1alpha1.HTTPUploaderConfig{byName},
			resource:  resourceWithIdentity("other", "1.0.0", nil),
			want:      "",
		},
		{
			name:      "name constraint matches the named resource",
			uploaders: []*transferv1alpha1.HTTPUploaderConfig{byName},
			resource:  resourceWithIdentity("docs", "1.0.0", nil),
			want:      "byName",
		},
		{
			name:      "version constraint matches the versioned resource",
			uploaders: []*transferv1alpha1.HTTPUploaderConfig{byVersion},
			resource:  resourceWithIdentity("docs", "1.0.0", nil),
			want:      "byVersion",
		},
		{
			name:      "version constraint selects only the matching version",
			uploaders: []*transferv1alpha1.HTTPUploaderConfig{byVersion},
			resource:  resourceWithIdentity("docs", "2.0.0", nil),
			want:      "",
		},
		{
			name:      "extraIdentity must be present and equal",
			uploaders: []*transferv1alpha1.HTTPUploaderConfig{byArch},
			resource:  resourceWithIdentity("docs", "1.0.0", map[string]string{"architecture": "amd64"}),
			want:      "",
		},
		{
			name:      "extraIdentity matches",
			uploaders: []*transferv1alpha1.HTTPUploaderConfig{byArch},
			resource:  resourceWithIdentity("docs", "1.0.0", map[string]string{"architecture": "arm64"}),
			want:      "byArch",
		},
		{
			name:      "first match wins: specific before broad",
			uploaders: []*transferv1alpha1.HTTPUploaderConfig{byNameAndArch, byName, byAccess},
			resource:  resourceWithIdentity("docs", "1.0.0", map[string]string{"architecture": "arm64"}),
			want:      "byNameAndArch",
		},
		{
			name:      "broad rule wins when specific rules do not apply",
			uploaders: []*transferv1alpha1.HTTPUploaderConfig{byNameAndArch, byName, byAccess},
			resource:  resourceWithIdentity("other", "1.0.0", nil),
			want:      "byAccess",
		},
		{
			name:      "access version must match when specified",
			uploaders: []*transferv1alpha1.HTTPUploaderConfig{byVersionedAccess},
			resource:  resourceWithIdentity("docs", "1.0.0", nil),
			want:      "",
		},
		{
			name:      "unversioned access type matches any version",
			uploaders: []*transferv1alpha1.HTTPUploaderConfig{byUnversioned},
			resource:  resourceWithIdentity("docs", "1.0.0", nil),
			want:      "byUnversioned",
		},
		{
			name:      "non-matching access type is skipped",
			uploaders: []*transferv1alpha1.HTTPUploaderConfig{ociOnly, byAccess},
			resource:  resourceWithIdentity("docs", "1.0.0", nil),
			want:      "byAccess",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := matchUploader(tc.uploaders, tc.resource)
			assert.Equal(t, tc.want, won(t, got))
		})
	}
}
