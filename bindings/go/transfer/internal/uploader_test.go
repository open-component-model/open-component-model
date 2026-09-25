package internal

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	helmv1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	helmv1alpha1 "ocm.software/open-component-model/bindings/go/helm/transformation/spec/v1alpha1"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/transform/graph/env"
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
	assert.NotContains(t, streaming.spec, "opener", "the plain HTTP uploader must not select a source opener")

	// Source reference preserves the original wget URL.
	srcAccess := streaming.spec["resource"].(map[string]any)["access"].(map[string]any)
	assert.Equal(t, "https://source.example/artifacts/blob.tar", srcAccess["url"])

	// The upload request carries the HTTP verb and the templated target URL.
	request := streaming.spec["request"].(map[string]any)
	assert.Equal(t, "Wget/v1", request["type"])
	assert.Equal(t, "PUT", request["verb"], "the upload request must carry the configured write verb")
	requestURL := request["url"].(string)
	assert.True(t, strings.HasPrefix(requestURL, "${") && strings.HasSuffix(requestURL, "}"),
		"request url must be a CEL expression field, got %q", requestURL)
	assert.Contains(t, requestURL, ".component.resources[0].access.url", "resource alias must be rewritten to the selected node path")

	// The published (download) target access resolves to the same URL but must not carry
	// the upload write verb, so a later download does not re-issue the write request.
	tgtResource := streaming.spec["targetResource"].(map[string]any)
	tgtAccess := tgtResource["access"].(map[string]any)
	assert.Equal(t, "Wget/v1", tgtAccess["type"])
	assert.NotContains(t, tgtAccess, "verb", "the published download access must not carry the upload write verb")
	targetURL := tgtAccess["url"].(string)
	assert.Equal(t, requestURL, targetURL, "request and published access must resolve to the same target URL")
	assert.Contains(t, targetURL, ".component.resources[0]", "target url must select the resource by index from the descriptor node")
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

func TestBuildGraphDefinition_JFrogHelmUploader_EmitsHelmTarget(t *testing.T) {
	chartResource := helmResource("chart-resource", "1.0.0", "https://charts.example", "chart:1.0.0")
	build := func(t *testing.T, resource descriptor.Resource, u *transferv1alpha1.JFrogHelmUploaderConfig) (*transformv1alpha1.TransformationGraphDefinition, transformv1alpha1.GenericTransformation) {
		t.Helper()
		r := require.New(t)
		desc := testDescriptor("ocm.software/test", "1.0.0", []descriptor.Resource{resource}, nil)
		resolver := testResolverFor("ocm.software/test", "1.0.0", testOCIRepo("ghcr.io/source"), desc)
		roots := testTransferRoots("ocm.software/test", "1.0.0", testOCIRepo("ghcr.io/target"), resolver)

		tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{CopyMode: transferv1alpha1.CopyModeAllResources}, []transferv1alpha1.UploaderConfig{u})
		r.NoError(err)

		var streaming []transformv1alpha1.GenericTransformation
		for _, tr := range tgd.Transformations {
			r.NotEqual(helmv1alpha1.GetHelmChartV1alpha1, tr.Type, "the uploader path must not emit a GetHelmChart node")
			if tr.Type == wgetv1alpha1.HTTPStreamingV1alpha1 {
				streaming = append(streaming, tr)
			}
		}
		r.Len(streaming, 1)
		return tgd, streaming[0]
	}
	uploader := func(accessType runtime.Type) *transferv1alpha1.JFrogHelmUploaderConfig {
		return &transferv1alpha1.JFrogHelmUploaderConfig{
			Type:       runtime.NewVersionedType(transferv1alpha1.JFrogHelmUploaderConfigType, transferv1alpha1.Version),
			MatchSpec:  transferv1alpha1.UploaderMatch{AccessType: accessType},
			URL:        "https://artifactory.example",
			Repository: "helm-local",
		}
	}
	helmMatch := runtime.NewVersionedType(helmv1.LegacyType, helmv1.LegacyTypeVersion)
	requestURL := func(tr transformv1alpha1.GenericTransformation) string {
		return tr.Spec.Data["request"].(map[string]any)["url"].(string)
	}
	helmChart := func(tr transformv1alpha1.GenericTransformation) string {
		return tr.Spec.Data["targetResource"].(map[string]any)["access"].(map[string]any)["helmChart"].(string)
	}

	t.Run("publishes the source chart as Helm/v1 and reindexes", func(t *testing.T) {
		r := require.New(t)
		_, tr := build(t, chartResource, uploader(helmMatch))
		r.Equal("HelmChartArchive", tr.Spec.Data["opener"])
		r.NotContains(tr.Spec.Data, "sourceFile")

		request := tr.Spec.Data["request"].(map[string]any)
		r.Equal("PUT", request["verb"])
		r.Equal(`${"https://artifactory.example/artifactory/helm-local/" + "chart" + "-" + "1.0.0" + ".tgz"}`, request["url"],
			"the chart is deployed under the name and version of the source chart, not of the resource")

		access := tr.Spec.Data["targetResource"].(map[string]any)["access"].(map[string]any)
		r.Equal("Helm/v1", access["type"])
		r.Equal("https://artifactory.example/artifactory/api/helm/helm-local", access["helmRepository"])
		r.Equal(`${"chart" + ":" + "1.0.0"}`, access["helmChart"])

		r.Equal("test@1.0.0 [Stream chart-resource to artifactory.example]", tr.Label)
		afterUpload := tr.Spec.Data["afterUpload"].(map[string]any)
		r.Equal("POST", afterUpload["verb"])
		r.Equal("https://artifactory.example/artifactory/api/helm/helm-local/reindex", afterUpload["url"])
	})

	sourceDefaults := []struct {
		name      string
		resource  descriptor.Resource
		match     runtime.Type
		wantChart []string
	}{
		{
			name:      "Helm packaged file name",
			resource:  helmResource("r", "9.9.9", "https://charts.example", "my-chart-1.2.3-rc.1.tgz"),
			match:     helmMatch,
			wantChart: []string{`"my-chart"`, `"1.2.3-rc.1"`},
		},
		{
			name: "Helm chart without version falls back to the resource version",
			resource: func() descriptor.Resource {
				res := helmResource("r", "9.9.9", "https://charts.example", "chart")
				res.Access.(*helmv1.Helm).Version = ""
				return res
			}(),
			match:     helmMatch,
			wantChart: []string{`"chart"`, ".component.resources[0].version"},
		},
		{
			name:      "OCIImage repository and tag",
			resource:  ociImageResource("r", "9.9.9", "ghcr.io/org/charts/podinfo:6.5.0@sha256:0000000000000000000000000000000000000000000000000000000000000000"),
			match:     runtime.NewVersionedType(ociv1.LegacyType, ociv1.LegacyTypeVersion),
			wantChart: []string{`"podinfo"`, `"6.5.0"`},
		},
		{
			name:      "LocalBlob has no chart reference and uses the resource",
			resource:  localBlobResource("r", "9.9.9"),
			match:     runtime.NewVersionedType(descriptorv2.LocalBlobAccessType, descriptorv2.LocalBlobAccessTypeVersion),
			wantChart: []string{".component.resources[0].name", ".component.resources[0].version"},
		},
	}
	for _, tt := range sourceDefaults {
		t.Run("defaults from "+tt.name, func(t *testing.T) {
			r := require.New(t)
			_, tr := build(t, tt.resource, uploader(tt.match))
			for _, want := range tt.wantChart {
				r.Contains(requestURL(tr), want)
				r.Contains(helmChart(tr), want)
			}
		})
	}

	t.Run("LocalBlob streams from the source component version", func(t *testing.T) {
		r := require.New(t)
		tgd, tr := build(t, localBlobResource("chart", "1.0.0"), uploader(runtime.NewVersionedType(descriptorv2.LocalBlobAccessType, descriptorv2.LocalBlobAccessTypeVersion)))
		for _, other := range tgd.Transformations {
			r.NotEqual(ociv1alpha1.OCIGetLocalResourceV1alpha1, other.Type, "the local blob must not be buffered by a GetLocalResource transformation")
		}
		cv := tr.Spec.Data["componentVersion"].(map[string]any)
		r.Equal("ocm.software/test", cv["component"])
		r.Equal("1.0.0", cv["version"])
		r.Equal("OCIRepository/v1", cv["repository"].(map[string]any)["type"])
		r.Nil(findCleanupTransformation(tgd), "nothing is buffered, so there is nothing to clean up")
	})

	t.Run("literal chart name and expression chart version", func(t *testing.T) {
		r := require.New(t)
		u := uploader(helmMatch)
		u.ChartName = "renamed"
		u.ChartVersion = `${resource.version + "-ocm"}`
		_, tr := build(t, chartResource, u)
		r.Contains(requestURL(tr), `"renamed"`)
		r.Contains(requestURL(tr), `.component.resources[0].version + "-ocm")`)
	})

	t.Run("reindex disabled", func(t *testing.T) {
		r := require.New(t)
		u := uploader(helmMatch)
		u.Reindex = new(bool)
		_, tr := build(t, chartResource, u)
		r.NotContains(tr.Spec.Data, "afterUpload")
	})
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
	assert.Contains(t, targetURL, ".component.resources[", "the bare resource identifier must be rewritten")
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
			req := tgd.Transformations[i].Spec.Data["request"].(map[string]any)
			header, _ = req["header"].(map[string]any)
		}
	}
	r.NotNil(header, "the upload request must carry the templated header map")

	reprDigest := header["Repr-Digest"].([]any)[0].(string)
	assert.True(t, strings.HasPrefix(reprDigest, "${") && strings.HasSuffix(reprDigest, "}"),
		"templated header value must be a CEL expression field, got %q", reprDigest)
	assert.Contains(t, reprDigest, ".component.resources[", "the resource alias must be rewritten to the node path")
	assert.Contains(t, reprDigest, ".digest.value", "the digest field access must survive the rewrite")
	assert.NotContains(t, reprDigest, "resource.digest", "the bare resource alias must not survive the rewrite")

	contentDigest := header["Content-Digest"].([]any)[0].(string)
	assert.Contains(t, contentDigest, "contentDigestAlgorithm(",
		"the contentDigestAlgorithm() call must be preserved")
	assert.Contains(t, contentDigest, ".digest.hashAlgorithm", "the algorithm-name field access must survive the rewrite")
	assert.Contains(t, contentDigest, "base64.encode(hex.decode(", "the base64/hex conversion must be preserved")
	assert.Contains(t, contentDigest, ".digest.value", "the digest value field access must survive the rewrite")
	assert.Contains(t, contentDigest, ".component.resources[", "the resource alias must be rewritten to the node path")

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
	assert.Contains(t, targetURL, ".component.resources[", "the resource alias must be rewritten to the node path")
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
	assert.Contains(t, targetURL, ".component.resources[", "the resource alias must be rewritten to the node path")
	assert.Contains(t, targetURL, ".access.imageReference", "the access field access must survive the rewrite")
	assert.NotContains(t, targetURL, "resource.access", "the bare resource alias must not survive the rewrite")
}

func TestBuildGraphDefinition_UploaderLiteralTargetURL(t *testing.T) {
	r := require.New(t)
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{wgetResource("blob", "1.0.0", "https://source.example/artifacts/blob.tar")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	roots := testTransferRoots("ocm.software/test", "1.0.0", targetRepo, resolver)

	// A targetURL without ${...} is a literal, templated like any other string: it
	// passes through unchanged with no special-case handling.
	uploaders := []transferv1alpha1.UploaderConfig{wgetUploader(t, `https://target.example/uploads/blob.tar`)}
	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{CopyMode: transferv1alpha1.CopyModeLocalBlobResources}, uploaders)
	r.NoError(err)

	var targetURL string
	for i := range tgd.Transformations {
		if tgd.Transformations[i].Type == wgetv1alpha1.HTTPStreamingV1alpha1 {
			tgt := tgd.Transformations[i].Spec.Data["targetResource"].(map[string]any)
			targetURL = tgt["access"].(map[string]any)["url"].(string)
		}
	}
	assert.Equal(t, `https://target.example/uploads/blob.tar`, targetURL,
		"a literal targetURL must pass through unchanged")
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

// TestResourceNodePath_ExtraIdentitySelectorEvaluatesOverMixedResources reproduces the
// CLI finding where selecting a resource by extraIdentity fails with "undefined field
// 'extraIdentity'" during CEL type checking: the environment node's resource element
// type is inferred from the concrete JSON, so an optional field such as extraIdentity is
// absent from the inferred type when a sibling resource omits it. Index-based selection
// compiles and evaluates cleanly over such a mixed resource list.
func TestResourceNodePath_ExtraIdentitySelectorEvaluatesOverMixedResources(t *testing.T) {
	r := require.New(t)

	plain := wgetResource("plain", "1.0.0", "https://source.example/plain.tar")
	blob := wgetResource("blob", "1.0.0", "https://source.example/blob.tar")
	blob.ExtraIdentity = runtime.Identity{"tier": "public"}

	desc := testDescriptor("ocm.software/test", "1.0.0", []descriptor.Resource{plain, blob}, nil)
	v2desc, err := descriptor.ConvertToV2(runtime.NewScheme(runtime.WithAllowUnknown()), desc)
	r.NoError(err)

	raw, err := json.Marshal(v2desc)
	r.NoError(err)
	var node map[string]any
	r.NoError(json.Unmarshal(raw, &node))

	const baseID = "root"
	environment := map[string]any{baseID: node}

	// The selector the uploader rewrites the `resource` alias to for the blob resource
	// (index 1 in the descriptor's resource list). Index selection compiles even though
	// the sibling "plain" resource omits extraIdentity.
	selector := resourceNodePath(baseID, 1)
	r.Contains(selector, "resources[1]", "the selector must address the resource by index")

	builder, err := env.NewEnvBuilder(environment)
	r.NoError(err)
	celEnv, _, err := builder.CurrentEnv()
	r.NoError(err)
	ast, iss := celEnv.Compile(selector + ".name")
	r.NoError(iss.Err(), "selector must compile without an undefined-field error")
	prg, err := celEnv.Program(ast)
	r.NoError(err)
	out, _, err := prg.Eval(map[string]any{})
	r.NoError(err, "selector must evaluate without an undefined-field error over mixed resources")
	assert.Equal(t, "blob", out.Value(), "the index selector must resolve to the matching resource")
}
