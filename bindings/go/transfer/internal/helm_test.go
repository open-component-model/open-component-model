package internal

import (
	"crypto"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"cel.dev/cel-go/cel"
	"github.com/stretchr/testify/require"

	v4alpha1 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	helmv1alpha1 "ocm.software/open-component-model/bindings/go/helm/transformation/spec/v1alpha1"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/transform/graph/env"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

const (
	helmDigestComponent = "ocm.software/test"
	helmDigestVersion   = "1.0.0"
)

func chartDigest() *descriptor.Digest {
	return &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: descriptor.GenericBlobDigestV1, Value: strings.Repeat("a", 64)}
}

// helmDigestDescriptor returns a component version with one Helm chart that records dig.
func helmDigestDescriptor(dig *descriptor.Digest, signed bool, chartRepo string) *descriptor.Descriptor {
	chart := helmResource("chart", "1.0.0", chartRepo, "chart")
	chart.Digest = dig
	desc := testDescriptor(helmDigestComponent, helmDigestVersion, []descriptor.Resource{chart}, nil)
	if signed {
		desc.Signatures = []descriptor.Signature{{Name: "default"}}
	}
	return desc
}

// helmDigestGraph builds the transfer graph of desc into the targets.
func helmDigestGraph(t *testing.T, desc *descriptor.Descriptor, uploaders []transferv1alpha1.UploaderConfig, targets ...runtime.Typed) (*transformv1alpha1.TransformationGraphDefinition, error) {
	t.Helper()
	key := helmDigestComponent + ":" + helmDigestVersion
	resolver := testResolverFor(helmDigestComponent, helmDigestVersion, testOCIRepo("ghcr.io/source"), desc)
	roots := map[string]TransferRoot{key: {RootComponentKey: key, Targets: targets, SourceResolver: resolver}}
	return BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, uploaders)
}

func nodesOfType(tgd *transformv1alpha1.TransformationGraphDefinition, typ runtime.Type) []transformv1alpha1.GenericTransformation {
	var nodes []transformv1alpha1.GenericTransformation
	for _, tr := range tgd.Transformations {
		if tr.Type == typ {
			nodes = append(nodes, tr)
		}
	}
	return nodes
}

func nodeTypes(tgd *transformv1alpha1.TransformationGraphDefinition) []runtime.Type {
	types := make([]runtime.Type, len(tgd.Transformations))
	for i, tr := range tgd.Transformations {
		types[i] = tr.Type
	}
	return types
}

// oneNodeOfType requires exactly one node of typ and returns it.
func oneNodeOfType(r *require.Assertions, tgd *transformv1alpha1.TransformationGraphDefinition, typ runtime.Type) transformv1alpha1.GenericTransformation {
	nodes := nodesOfType(tgd, typ)
	r.Len(nodes, 1, "nodes of type %s", typ)
	return nodes[0]
}

// firstDescriptorResource returns the expression the upload node takes resource 0 from.
func firstDescriptorResource(r *require.Assertions, upload transformv1alpha1.GenericTransformation) any {
	desc, ok := upload.Spec.Data["descriptor"].(map[string]any)
	r.True(ok, "the descriptor spec must be composed")
	return desc["component"].(map[string]any)["resources"].([]any)[0]
}

func chartResourceID() string {
	chart := helmResource("chart", "1.0.0", "", "chart")
	return identityToTransformationID(chart.ToIdentity())
}

func helmDigestComponentID() string {
	return identityToTransformationID(runtime.Identity{
		descriptor.IdentityAttributeName:    helmDigestComponent,
		descriptor.IdentityAttributeVersion: helmDigestVersion,
	})
}

// requireLocalBlobChain checks the chain that stores the chart archive as a local blob.
func requireLocalBlobChain(t *testing.T, tgd *transformv1alpha1.TransformationGraphDefinition, addType, uploadType runtime.Type) {
	t.Helper()
	r := require.New(t)
	r.Equal([]runtime.Type{helmv1alpha1.GetHelmChartV1alpha1, addType, uploadType, FileCleanupVersionedType}, nodeTypes(tgd))

	get := tgd.Transformations[0]
	add := tgd.Transformations[1]
	r.Equal(fmt.Sprintf("${%s.output.chartFile}", get.ID), add.Spec.Data["file"])
	access := add.Spec.Data["resource"].(map[string]any)["access"].(map[string]any)
	r.NotContains(access, "referenceName", "the chart archive is not an OCI manifest")

	r.Equal([]string{
		fmt.Sprintf("${%s.spec.file}", add.ID),
		fmt.Sprintf("${%s.output.?provFile}", get.ID),
	}, cleanupFileExpressions(t, findCleanupTransformation(tgd)))
	r.Equal(fmt.Sprintf("${%s.output.resource}", add.ID), firstDescriptorResource(r, tgd.Transformations[2]))
}

// requireLayerChain checks the chain that pushes the chart as a Helm OCI image and points the
// access at its chart layer.
func requireLayerChain(t *testing.T, tgd *transformv1alpha1.TransformationGraphDefinition) {
	t.Helper()
	r := require.New(t)
	r.Equal([]runtime.Type{
		helmv1alpha1.GetHelmChartV1alpha1,
		helmv1alpha1.ConvertHelmToOCIV1alpha1,
		ociv1alpha1.AddOCIArtifactV1alpha1,
		helmv1alpha1.SetHelmChartLayerAccessV1alpha1,
		ociv1alpha1.OCIAddComponentVersionV1alpha1,
		FileCleanupVersionedType,
	}, nodeTypes(tgd))

	convert := tgd.Transformations[1]
	add := tgd.Transformations[2]
	setAccess := tgd.Transformations[3]
	r.Equal(helmDigestComponentID()+"SetAccess"+chartResourceID(), setAccess.ID)
	r.Equal("test@1.0.0 [Reference chart layer of chart]", setAccess.Label)
	r.Equal(fmt.Sprintf(`${%[1]s.output.resource.access.imageReference.contains("@") ? %[1]s.output.resource.access.imageReference : `+
		`%[1]s.output.resource.access.imageReference + "@" + (%[1]s.output.resource.digest.hashAlgorithm == "SHA-512" ? "sha512:" : "sha256:") + `+
		`%[1]s.output.resource.digest.value}`, add.ID), setAccess.Spec.Data["imageReference"])
	r.Equal(fmt.Sprintf("${%s.output.chartLayer}", convert.ID), setAccess.Spec.Data["chartLayer"])
	resource, ok := setAccess.Spec.Data["resource"].(map[string]any)
	r.True(ok, "the resource must be passed as a literal")
	r.Equal("chart", resource["name"])
	r.Equal(map[string]any{
		"hashAlgorithm":          "SHA-256",
		"normalisationAlgorithm": descriptor.GenericBlobDigestV1,
		"value":                  strings.Repeat("a", 64),
	}, resource["digest"], "the source digest must reach the new access node")

	r.Equal(fmt.Sprintf("${%s.output.resource}", setAccess.ID), firstDescriptorResource(r, tgd.Transformations[4]))
	r.Equal(helmFileExpressions(helmDigestComponentID(), chartResourceID()), cleanupFileExpressions(t, findCleanupTransformation(tgd)))
}

// requireMainChain checks the chain that converts the chart to an OCI artifact and embeds it.
func requireMainChain(t *testing.T, tgd *transformv1alpha1.TransformationGraphDefinition) {
	t.Helper()
	r := require.New(t)
	r.Empty(nodesOfType(tgd, helmv1alpha1.SetHelmChartLayerAccessV1alpha1))
	oneNodeOfType(r, tgd, helmv1alpha1.ConvertHelmToOCIV1alpha1)
	add := oneNodeOfType(r, tgd, ociv1alpha1.OCIAddLocalResourceV1alpha1)
	upload := oneNodeOfType(r, tgd, ociv1alpha1.OCIAddComponentVersionV1alpha1)
	r.Equal(fmt.Sprintf("${%s.output.resource}", add.ID), firstDescriptorResource(r, upload))
	r.Equal(helmFileExpressions(helmDigestComponentID(), chartResourceID()), cleanupFileExpressions(t, findCleanupTransformation(tgd)))
}

func TestBuildGraphDefinition_HelmChartDigest(t *testing.T) {
	const httpRepo = "https://charts.example.com"
	signedPrefix := "cannot copy the Helm chart of signed component version ocm.software/test:1.0.0"
	ociArtifactDigest := &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: descriptor.OCIArtifactDigestV1, Value: strings.Repeat("b", 64)}
	sha512Digest := &descriptor.Digest{HashAlgorithm: "SHA-512", NormalisationAlgorithm: descriptor.GenericBlobDigestV1, Value: strings.Repeat("c", 128)}

	localOnRegistry := func(t *testing.T, tgd *transformv1alpha1.TransformationGraphDefinition) {
		t.Helper()
		requireLocalBlobChain(t, tgd, ociv1alpha1.OCIAddLocalResourceV1alpha1, ociv1alpha1.OCIAddComponentVersionV1alpha1)
	}

	tests := []struct {
		name      string
		digest    *descriptor.Digest
		signed    bool
		chartRepo string
		target    runtime.Typed
		uploaders []transferv1alpha1.UploaderConfig
		check     func(t *testing.T, tgd *transformv1alpha1.TransformationGraphDefinition)
		wantErr   []string
	}{
		{
			name: "chart digest, unsigned, registry target", digest: chartDigest(),
			chartRepo: httpRepo, target: testOCIRepo("ghcr.io/target"), uploaders: withLocalBlobUploader(),
			check: requireMainChain,
		},
		{
			name: "chart digest, signed, registry target", digest: chartDigest(), signed: true,
			chartRepo: httpRepo, target: testOCIRepo("ghcr.io/target"), uploaders: withLocalBlobUploader(),
			check: localOnRegistry,
		},
		{
			name: "chart digest, signed, CTF target", digest: chartDigest(), signed: true,
			chartRepo: httpRepo, target: testCTFRepo("./out"), uploaders: withLocalBlobUploader(),
			check: func(t *testing.T, tgd *transformv1alpha1.TransformationGraphDefinition) {
				t.Helper()
				requireLocalBlobChain(t, tgd, ociv1alpha1.CTFAddLocalResourceV1alpha1, ociv1alpha1.CTFAddComponentVersionV1alpha1)
			},
		},
		{
			name: "chart digest, signed, OCI uploader", digest: chartDigest(), signed: true,
			chartRepo: httpRepo, target: testOCIRepo("ghcr.io/target"), uploaders: withLocalBlobUploader(ociUploaders()...),
			check: requireLayerChain,
		},
		{
			name: "exclusion marker, signed", digest: descriptor.NewExcludeFromSignatureDigest(), signed: true,
			chartRepo: httpRepo, target: testOCIRepo("ghcr.io/target"), uploaders: withLocalBlobUploader(),
			check: localOnRegistry,
		},
		{
			name: "chart digest, unsigned, oci chart repository", digest: chartDigest(),
			chartRepo: "oci://registry.example/charts", target: testOCIRepo("ghcr.io/target"), uploaders: withLocalBlobUploader(),
			check: requireMainChain,
		},
		{
			name: "ociArtifactDigest, unsigned", digest: ociArtifactDigest,
			chartRepo: httpRepo, target: testOCIRepo("ghcr.io/target"), uploaders: withLocalBlobUploader(),
			check: requireMainChain,
		},
		{
			name: "SHA-512 chart digest, unsigned", digest: sha512Digest,
			chartRepo: httpRepo, target: testOCIRepo("ghcr.io/target"), uploaders: withLocalBlobUploader(),
			check: requireMainChain,
		},
		{
			name: "no digest, signed", signed: true,
			chartRepo: httpRepo, target: testOCIRepo("ghcr.io/target"), uploaders: withLocalBlobUploader(),
			wantErr: []string{signedPrefix, "no recorded digest"},
		},
		{
			name: "ociArtifactDigest, signed", digest: ociArtifactDigest, signed: true,
			chartRepo: httpRepo, target: testOCIRepo("ghcr.io/target"), uploaders: withLocalBlobUploader(),
			wantErr: []string{signedPrefix, "ociArtifactDigest/v1"},
		},
		{
			name: "SHA-512 chart digest, signed, OCI uploader", digest: sha512Digest, signed: true,
			chartRepo: httpRepo, target: testOCIRepo("ghcr.io/target"), uploaders: withLocalBlobUploader(ociUploaders()...),
			wantErr: []string{signedPrefix, "SHA-512"},
		},
		{
			name: "no digest, signed, no uploader", signed: true,
			chartRepo: httpRepo, target: testOCIRepo("ghcr.io/target"),
			check: func(t *testing.T, tgd *transformv1alpha1.TransformationGraphDefinition) {
				t.Helper()
				require.Empty(t, nodesOfType(tgd, helmv1alpha1.GetHelmChartV1alpha1), "a chart no uploader selects stays by reference")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			tgd, err := helmDigestGraph(t, helmDigestDescriptor(tc.digest, tc.signed, tc.chartRepo), tc.uploaders, tc.target)
			if len(tc.wantErr) > 0 {
				r.Error(err)
				for _, fragment := range tc.wantErr {
					r.ErrorContains(err, fragment)
				}
				return
			}
			r.NoError(err)
			tc.check(t, tgd)
		})
	}
}

// TestBuildGraphDefinition_HelmChartDigest_TwoTargets covers one transfer into a CTF and a
// registry with the OCI uploader: the OCI uploader selects the chart only for the registry, so
// each target gets its own chain and the cleanup node lists the files of both.
func TestBuildGraphDefinition_HelmChartDigest_TwoTargets(t *testing.T) {
	r := require.New(t)
	tgd, err := helmDigestGraph(t, helmDigestDescriptor(chartDigest(), true, "https://charts.example.com"),
		withLocalBlobUploader(ociUploaders()...), testCTFRepo("./out"), testOCIRepo("ghcr.io/target"))
	r.NoError(err)

	r.Len(nodesOfType(tgd, helmv1alpha1.GetHelmChartV1alpha1), 2)
	convert := oneNodeOfType(r, tgd, helmv1alpha1.ConvertHelmToOCIV1alpha1)
	oneNodeOfType(r, tgd, helmv1alpha1.SetHelmChartLayerAccessV1alpha1)
	localAdd := oneNodeOfType(r, tgd, ociv1alpha1.CTFAddLocalResourceV1alpha1)

	resourceID := chartResourceID()
	localID := strings.TrimSuffix(localAdd.ID, "Add"+resourceID)
	layerID := strings.TrimSuffix(convert.ID, "Convert"+resourceID)
	r.NotEqual(localID, layerID, "each target must get its own node IDs")

	exprs := cleanupFileExpressions(t, findCleanupTransformation(tgd))
	r.Subset(exprs, []string{
		fmt.Sprintf("${%s.spec.file}", localAdd.ID),
		fmt.Sprintf("${%sGet%s.output.?provFile}", localID, resourceID),
	})
	r.Subset(exprs, helmFileExpressions(layerID, resourceID))
	r.Len(exprs, 5)
}

// TestBuildGraphDefinition_HelmChartDigest_ReferencedChild covers an unsigned chart whose
// component version a parent references by digest: converting the chart would change the child
// descriptor and break that digest, so the chart is kept as a local blob.
func TestBuildGraphDefinition_HelmChartDigest_ReferencedChild(t *testing.T) {
	const parentName = "ocm.software/parent"
	tests := []struct {
		name            string
		referenceDigest bool
		check           func(t *testing.T, tgd *transformv1alpha1.TransformationGraphDefinition)
	}{
		{
			name: "reference digest", referenceDigest: true,
			check: func(t *testing.T, tgd *transformv1alpha1.TransformationGraphDefinition) {
				t.Helper()
				requireLocalBlobChain(t, tgd, ociv1alpha1.OCIAddLocalResourceV1alpha1, ociv1alpha1.OCIAddComponentVersionV1alpha1)
			},
		},
		{name: "no reference digest", check: requireMainChain},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			child := helmDigestDescriptor(chartDigest(), false, "https://charts.example.com")
			ref := descriptor.Reference{
				ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "child", Version: helmDigestVersion}},
				Component:   helmDigestComponent,
			}
			if tc.referenceDigest {
				dig, err := signing.GenerateDigest(t.Context(), child, slog.New(slog.DiscardHandler), v4alpha1.Algorithm, crypto.SHA256.String())
				r.NoError(err)
				ref.Digest = *dig
			}
			parent := testDescriptor(parentName, helmDigestVersion, nil, []descriptor.Reference{ref})
			source := testOCIRepo("ghcr.io/source")
			resolver := testMultiResolver(map[string]struct {
				spec runtime.Typed
				desc *descriptor.Descriptor
			}{
				parentName + ":" + helmDigestVersion:          {spec: source, desc: parent},
				helmDigestComponent + ":" + helmDigestVersion: {spec: source, desc: child},
			})
			roots := testTransferRoots(parentName, helmDigestVersion, testOCIRepo("ghcr.io/target"), resolver)
			tgd, err := BuildGraphDefinition(t.Context(), roots,
				transferv1alpha1.Config{Recursive: transferv1alpha1.RecursiveInfinite}, withLocalBlobUploader())
			r.NoError(err)

			// The chain helpers check the child's nodes by position, so drop the parent's upload.
			parentID := identityToTransformationID(runtime.Identity{
				descriptor.IdentityAttributeName:    parentName,
				descriptor.IdentityAttributeVersion: helmDigestVersion,
			})
			tgd.Transformations = slices.DeleteFunc(tgd.Transformations, func(tr transformv1alpha1.GenericTransformation) bool {
				return strings.HasPrefix(tr.ID, parentID)
			})
			tc.check(t, tgd)
		})
	}
}

// TestPinnedImageReference evaluates the expression in the CEL environment of the
// transformation graph, against the output an AddOCIArtifact node records.
func TestPinnedImageReference(t *testing.T) {
	const id = "add"
	sha256Value := strings.Repeat("b", 64)
	sha512Value := strings.Repeat("c", 128)
	tests := []struct {
		name      string
		reference string
		digest    map[string]any
		want      string
	}{
		{
			name:      "SHA-256 digest",
			reference: "ghcr.io/acme/charts/chart:1.0.0",
			digest:    map[string]any{"hashAlgorithm": "SHA-256", "normalisationAlgorithm": "genericBlobDigest/v1", "value": sha256Value},
			want:      "ghcr.io/acme/charts/chart:1.0.0@sha256:" + sha256Value,
		},
		{
			name:      "SHA-512 digest",
			reference: "ghcr.io/acme/charts/chart:1.0.0",
			digest:    map[string]any{"hashAlgorithm": "SHA-512", "normalisationAlgorithm": "genericBlobDigest/v1", "value": sha512Value},
			want:      "ghcr.io/acme/charts/chart:1.0.0@sha512:" + sha512Value,
		},
		{
			name:      "reference already pinned",
			reference: "ghcr.io/acme/charts/chart:1.0.0@sha256:" + sha256Value,
			digest:    map[string]any{"hashAlgorithm": "SHA-256", "normalisationAlgorithm": "genericBlobDigest/v1", "value": strings.Repeat("d", 64)},
			want:      "ghcr.io/acme/charts/chart:1.0.0@sha256:" + sha256Value,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			builder, err := env.NewEnvBuilder(map[string]any{})
			r.NoError(err)
			graphEnv, _, err := builder.CurrentEnv()
			r.NoError(err)
			celEnv, err := graphEnv.Extend(cel.Variable(id, cel.DynType))
			r.NoError(err)

			expr := pinnedImageReference(id)
			r.True(strings.HasPrefix(expr, "${") && strings.HasSuffix(expr, "}"), "the expression must be a single ${...} reference")
			ast, iss := celEnv.Compile(strings.TrimSuffix(strings.TrimPrefix(expr, "${"), "}"))
			r.NoError(iss.Err())
			prg, err := celEnv.Program(ast)
			r.NoError(err)

			out, _, err := prg.Eval(map[string]any{id: map[string]any{
				"output": map[string]any{"resource": map[string]any{
					"access": map[string]any{"type": "OCIImage/v1", "imageReference": tc.reference},
					"digest": tc.digest,
				}},
			}})
			r.NoError(err)
			r.Equal(tc.want, out.Value())
		})
	}
}

func TestKeepsChartDigest(t *testing.T) {
	generic := func(hash, value string) *descriptor.Digest {
		return &descriptor.Digest{HashAlgorithm: hash, NormalisationAlgorithm: descriptor.GenericBlobDigestV1, Value: value}
	}
	notChartDigest := func(norm, hash string) string {
		return "its recorded digest " + norm + " (" + hash + ") is not a genericBlobDigest/v1 digest of the chart archive with hash algorithm SHA-256 and a lower-case hex value, and the copy would replace it"
	}
	tests := []struct {
		name       string
		digest     *descriptor.Digest
		signed     bool
		referenced bool
		pushed     bool
		typ        string
		want       bool
		wantErr    string
	}{
		{name: "chart digest, unsigned", digest: chartDigest()},
		{name: "chart digest, signed", digest: chartDigest(), signed: true, want: true},
		{name: "chart digest, referenced by digest", digest: chartDigest(), referenced: true, want: true},
		{name: "exclusion marker, unsigned", digest: descriptor.NewExcludeFromSignatureDigest()},
		{name: "exclusion marker, signed", digest: descriptor.NewExcludeFromSignatureDigest(), signed: true, want: true},
		{name: "exclusion marker, signed, pushed", digest: descriptor.NewExcludeFromSignatureDigest(), signed: true, pushed: true, want: true},
		{name: "exclusion marker, signed, pushed, other resource type", digest: descriptor.NewExcludeFromSignatureDigest(), signed: true, pushed: true, typ: "helm", want: true},
		{name: "half exclusion marker, signed, pushed", digest: generic("no-digest", descriptor.NoDigest), signed: true, pushed: true, want: true},
		{
			name:    "half exclusion marker, signed",
			digest:  generic(descriptor.NoDigest, descriptor.NoDigest),
			signed:  true,
			wantErr: `the target cannot store its digest with hash algorithm NO-DIGEST and normalisation algorithm "genericBlobDigest/v1"; exclude it with normalisation algorithm EXCLUDE-FROM-SIGNATURE`,
		},
		{name: "no digest, unsigned"},
		{name: "no digest, referenced by digest", referenced: true},
		{name: "no digest, signed", signed: true, wantErr: "it has no recorded digest, and the copy would add one"},
		{
			name:    "ociArtifactDigest, signed",
			digest:  &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: descriptor.OCIArtifactDigestV1, Value: "x"},
			signed:  true,
			wantErr: notChartDigest("ociArtifactDigest/v1", "SHA-256"),
		},
		{
			name:    "hash algorithm spelled sha256, signed",
			digest:  generic("sha256", strings.Repeat("a", 64)),
			signed:  true,
			wantErr: notChartDigest("genericBlobDigest/v1", "sha256"),
		},
		{
			name:    "upper-case value, signed",
			digest:  generic("SHA-256", strings.Repeat("A", 64)),
			signed:  true,
			wantErr: notChartDigest("genericBlobDigest/v1", "SHA-256"),
		},
		{name: "upper-case value, referenced by digest", digest: generic("SHA-256", strings.Repeat("A", 64)), referenced: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			desc := helmDigestDescriptor(tc.digest, tc.signed, "https://charts.example.com")
			if tc.typ != "" {
				desc.Component.Resources[0].Type = tc.typ
			}
			v2desc, err := descriptor.ConvertToV2(runtime.NewScheme(runtime.WithAllowUnknown()), desc)
			r.NoError(err)

			keep, err := keepsChartDigest(&discoveryValue{Descriptor: desc, ReferenceDigest: tc.referenced}, v2desc.Component.Resources[0], tc.pushed)
			if tc.wantErr != "" {
				r.EqualError(err, "cannot copy the Helm chart of signed component version ocm.software/test:1.0.0: "+tc.wantErr+
					"; keep the resource by reference (a reference.uploader entry that matches it) or transfer without copying it")
				return
			}
			r.NoError(err)
			r.Equal(tc.want, keep)
		})
	}
}
