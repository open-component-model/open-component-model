package internal

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	celparser "ocm.software/open-component-model/bindings/go/cel/expression/parser"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	helmv1alpha1 "ocm.software/open-component-model/bindings/go/helm/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/transform/graph/env"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	wgetv1alpha1 "ocm.software/open-component-model/bindings/go/wget/transformation/spec/v1alpha1"
)

// specImageReference returns the target image reference a TransferOCIArtifact
// (targetResource) or AddOCIArtifact (resource) transformation pushes to.
func specImageReference(t *testing.T, tr transformv1alpha1.GenericTransformation) string {
	t.Helper()
	data, err := json.Marshal(tr.Spec.Data)
	require.NoError(t, err)
	var spec struct {
		Resource struct {
			Access struct {
				ImageReference string `json:"imageReference"`
			} `json:"access"`
		} `json:"resource"`
		TargetResource struct {
			Access struct {
				ImageReference string `json:"imageReference"`
			} `json:"access"`
		} `json:"targetResource"`
	}
	require.NoError(t, json.Unmarshal(data, &spec))
	if tr.Type == ociv1alpha1.TransferOCIArtifactV1alpha1 {
		return spec.TargetResource.Access.ImageReference
	}
	return spec.Resource.Access.ImageReference
}

// evaluateTemplate evaluates every ${...} expression of value against the graph's
// environment, with the same CEL functions the transfer builder registers, and returns
// the resulting string. Compiling here also proves the template type-checks.
func evaluateTemplate(t *testing.T, tgd *transformv1alpha1.TransformationGraphDefinition, value string) string {
	t.Helper()
	r := require.New(t)
	fields, err := celparser.ParseSchemaless(map[string]any{"value": value})
	r.NoError(err)
	builder, err := env.NewEnvBuilder(tgd.GetEnvironmentData())
	r.NoError(err)
	builder.RegisterEnvOption(ToOCIEnvOption())
	celEnv, _, err := builder.CurrentEnv()
	r.NoError(err)

	out := value
	for _, field := range fields {
		for _, expr := range field.Expressions {
			ast, issues := celEnv.Compile(expr.Value)
			r.NoError(issues.Err(), "expression %q must compile", expr.Value)
			prg, err := celEnv.Program(ast)
			r.NoError(err)
			val, _, err := prg.Eval(map[string]any{})
			r.NoError(err, "expression %q must evaluate", expr.Value)
			out = strings.ReplaceAll(out, "${"+expr.Value+"}", fmt.Sprint(val.Value()))
		}
	}
	return out
}

func transformationTypes(tgd *transformv1alpha1.TransformationGraphDefinition) []runtime.Type {
	types := make([]runtime.Type, 0, len(tgd.Transformations))
	for _, tr := range tgd.Transformations {
		types = append(types, tr.Type)
	}
	return types
}

func TestBuildGraphDefinition_OCIUploader(t *testing.T) {
	addOCIArtifact := runtime.NewVersionedType(ociv1alpha1.AddOCIArtifactType, ociv1alpha1.Version)

	manifestBlobWithoutReferenceName := dockerManifestLocalBlobResource("my-image", "1.0.0")
	manifestBlobWithoutReferenceName.Access.(*descriptorv2.LocalBlob).ReferenceName = ""

	manifestBlobWithRegistrylessName := dockerManifestLocalBlobResource("my-image", "1.0.0")
	manifestBlobWithRegistrylessName.Access.(*descriptorv2.LocalBlob).ReferenceName = "stefanprodan/podinfo:6.5.0"

	manifestBlobWithDottedName := dockerManifestLocalBlobResource("my-image", "1.0.0")
	manifestBlobWithDottedName.Access.(*descriptorv2.LocalBlob).ReferenceName = "ocm.software/podinfo@sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"

	tests := []struct {
		name      string
		target    runtime.Typed
		resource  descriptor.Resource
		copyMode  transferv1alpha1.CopyMode
		uploaders []transferv1alpha1.UploaderConfig
		wantTypes []runtime.Type
		// wantImageRef is the evaluated image reference of the node at wantImageRefAt.
		wantImageRef   string
		wantImageRefAt int
		wantCleanup    int
		wantErr        string
	}{
		{
			name:           "OCI image streams to target repository regardless of copy mode",
			target:         testOCIRepo("ghcr.io/target"),
			resource:       ociImageResource("my-image", "1.0.0", "oci://ghcr.io/org/image:v1"),
			uploaders:      ociUploaders(),
			wantTypes:      []runtime.Type{ociv1alpha1.TransferOCIArtifactV1alpha1, ociv1alpha1.OCIAddComponentVersionV1alpha1},
			wantImageRef:   "ghcr.io/target/org/image:v1",
			wantImageRefAt: 0,
		},
		{
			name:           "target sub path is part of the default reference",
			target:         &oci.Repository{Type: runtime.Type{Name: oci.Type, Version: "v1"}, BaseUrl: "ghcr.io/target", SubPath: "sub"},
			resource:       ociImageResource("my-image", "1.0.0", "oci://ghcr.io/org/image:v1"),
			uploaders:      ociUploaders(),
			wantTypes:      []runtime.Type{ociv1alpha1.TransferOCIArtifactV1alpha1, ociv1alpha1.OCIAddComponentVersionV1alpha1},
			wantImageRef:   "ghcr.io/target/sub/org/image:v1",
			wantImageRefAt: 0,
		},
		{
			name:           "untagged digest reference keeps only the repository",
			target:         testOCIRepo("ghcr.io/target"),
			resource:       ociImageResource("my-image", "1.0.0", "ghcr.io/org/image@sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"),
			uploaders:      ociUploaders(),
			wantTypes:      []runtime.Type{ociv1alpha1.TransferOCIArtifactV1alpha1, ociv1alpha1.OCIAddComponentVersionV1alpha1},
			wantImageRef:   "ghcr.io/target/org/image",
			wantImageRefAt: 0,
		},
		{
			name:           "Helm chart is converted and added as OCI artifact",
			target:         testOCIRepo("ghcr.io/target"),
			resource:       helmResource("my-chart", "1.0.0", "https://charts.example.com", "my-chart"),
			uploaders:      ociUploaders(),
			wantTypes:      []runtime.Type{helmv1alpha1.GetHelmChartV1alpha1, helmv1alpha1.ConvertHelmToOCIV1alpha1, addOCIArtifact, ociv1alpha1.OCIAddComponentVersionV1alpha1, FileCleanupVersionedType},
			wantImageRef:   "ghcr.io/target/my-chart:1.0.0",
			wantImageRefAt: 2,
			wantCleanup:    3,
		},
		{
			name:           "reference name is relative to the target: a host-like first component stays in the repository",
			target:         testOCIRepo("ghcr.io/target"),
			resource:       dockerManifestLocalBlobResource("my-image", "1.0.0"),
			uploaders:      ociUploaders(),
			wantTypes:      []runtime.Type{ociv1alpha1.OCIGetLocalResourceV1alpha1, addOCIArtifact, ociv1alpha1.OCIAddComponentVersionV1alpha1, FileCleanupVersionedType},
			wantImageRef:   "ghcr.io/target/ghcr.io/org/image:v1",
			wantImageRefAt: 1,
			wantCleanup:    1,
		},
		{
			name:           "reference name without host-like component keeps its full repository",
			target:         testOCIRepo("ghcr.io/target"),
			resource:       manifestBlobWithRegistrylessName,
			uploaders:      ociUploaders(),
			wantTypes:      []runtime.Type{ociv1alpha1.OCIGetLocalResourceV1alpha1, addOCIArtifact, ociv1alpha1.OCIAddComponentVersionV1alpha1, FileCleanupVersionedType},
			wantImageRef:   "ghcr.io/target/stefanprodan/podinfo:6.5.0",
			wantImageRefAt: 1,
		},
		{
			name:           "dotted reference name without tag keeps its full repository",
			target:         testOCIRepo("ghcr.io/target"),
			resource:       manifestBlobWithDottedName,
			uploaders:      ociUploaders(),
			wantTypes:      []runtime.Type{ociv1alpha1.OCIGetLocalResourceV1alpha1, addOCIArtifact, ociv1alpha1.OCIAddComponentVersionV1alpha1, FileCleanupVersionedType},
			wantImageRef:   "ghcr.io/target/ocm.software/podinfo",
			wantImageRefAt: 1,
		},
		{
			name:      "non-manifest local blob falls through to local blob",
			target:    testOCIRepo("ghcr.io/target"),
			resource:  localBlobResource("my-resource", "1.0.0"),
			uploaders: ociUploaders(),
			wantTypes: []runtime.Type{ociv1alpha1.OCIGetLocalResourceV1alpha1, ociv1alpha1.OCIAddLocalResourceV1alpha1, ociv1alpha1.OCIAddComponentVersionV1alpha1, FileCleanupVersionedType},
		},
		{
			name:      "manifest local blob without reference name falls through to local blob",
			target:    testOCIRepo("ghcr.io/target"),
			resource:  manifestBlobWithoutReferenceName,
			uploaders: ociUploaders(),
			wantTypes: []runtime.Type{ociv1alpha1.OCIGetLocalResourceV1alpha1, ociv1alpha1.OCIAddLocalResourceV1alpha1, ociv1alpha1.OCIAddComponentVersionV1alpha1, FileCleanupVersionedType},
		},
		{
			name:      "CTF target with the default template falls through to local blob",
			target:    testCTFRepo("/tmp/target"),
			resource:  ociImageResource("my-image", "1.0.0", "oci://ghcr.io/org/image:v1"),
			copyMode:  transferv1alpha1.CopyModeAllResources,
			uploaders: ociUploaders(),
			wantTypes: []runtime.Type{ociv1alpha1.GetOCIArtifactV1alpha1, ociv1alpha1.CTFAddLocalResourceV1alpha1, ociv1alpha1.CTFAddComponentVersionV1alpha1, FileCleanupVersionedType},
		},
		{
			name:     "CTF target with an absolute template streams to the templated reference",
			target:   testCTFRepo("/tmp/target"),
			resource: ociImageResource("my-image", "1.0.0", "oci://ghcr.io/org/image:v1"),
			uploaders: []transferv1alpha1.UploaderConfig{&transferv1alpha1.OCIUploaderConfig{
				ImageReference: `${"ghcr.io/mirror/" + resource.access.toOCI().repository + ":" + resource.access.toOCI().tag}`,
			}},
			wantTypes:      []runtime.Type{ociv1alpha1.TransferOCIArtifactV1alpha1, ociv1alpha1.CTFAddComponentVersionV1alpha1},
			wantImageRef:   "ghcr.io/mirror/org/image:v1",
			wantImageRefAt: 0,
		},
		{
			name:     "embedded template over resource and target",
			target:   testOCIRepo("ghcr.io/target"),
			resource: ociImageResource("my-image", "1.0.0", "oci://ghcr.io/org/image:v1"),
			uploaders: []transferv1alpha1.UploaderConfig{&transferv1alpha1.OCIUploaderConfig{
				ImageReference: `${target.baseUrl + "/images/" + resource.name}:${resource.version}`,
			}},
			wantTypes:      []runtime.Type{ociv1alpha1.TransferOCIArtifactV1alpha1, ociv1alpha1.OCIAddComponentVersionV1alpha1},
			wantImageRef:   "ghcr.io/target/images/my-image:1.0.0",
			wantImageRefAt: 0,
		},
		{
			name:     "explicit default template equals the omitted default",
			target:   testOCIRepo("ghcr.io/target"),
			resource: ociImageResource("my-image", "1.0.0", "oci://ghcr.io/org/image:v1"),
			uploaders: []transferv1alpha1.UploaderConfig{&transferv1alpha1.OCIUploaderConfig{
				ImageReference: transferv1alpha1.DefaultOCIImageReference,
			}},
			wantTypes:      []runtime.Type{ociv1alpha1.TransferOCIArtifactV1alpha1, ociv1alpha1.OCIAddComponentVersionV1alpha1},
			wantImageRef:   "ghcr.io/target/org/image:v1",
			wantImageRefAt: 0,
		},
		{
			name:     "toOCI() for a local blob without reference name falls through",
			target:   testOCIRepo("ghcr.io/target"),
			resource: manifestBlobWithoutReferenceName,
			uploaders: []transferv1alpha1.UploaderConfig{&transferv1alpha1.OCIUploaderConfig{
				ImageReference: `${"ghcr.io/mirror/" + resource.access.toOCI().repository}`,
			}},
			wantTypes: []runtime.Type{ociv1alpha1.OCIGetLocalResourceV1alpha1, ociv1alpha1.OCIAddLocalResourceV1alpha1, ociv1alpha1.OCIAddComponentVersionV1alpha1, FileCleanupVersionedType},
		},
		{
			name:     "a template without toOCI() still applies to a resource without reference name",
			target:   testOCIRepo("ghcr.io/target"),
			resource: manifestBlobWithoutReferenceName,
			uploaders: []transferv1alpha1.UploaderConfig{&transferv1alpha1.OCIUploaderConfig{
				ImageReference: `${target.baseUrl + "/" + resource.name + ":" + resource.version}`,
			}},
			wantTypes:      []runtime.Type{ociv1alpha1.OCIGetLocalResourceV1alpha1, addOCIArtifact, ociv1alpha1.OCIAddComponentVersionV1alpha1, FileCleanupVersionedType},
			wantImageRef:   "ghcr.io/target/my-image:1.0.0",
			wantImageRefAt: 1,
		},
		{
			name:      "an unparsable image reference fails the build",
			target:    testOCIRepo("ghcr.io/target"),
			resource:  ociImageResource("my-image", "1.0.0", "oci://"),
			uploaders: ociUploaders(),
			wantErr:   "cannot parse imageReference",
		},
		{
			name:     "an invalid template fails the build",
			target:   testOCIRepo("ghcr.io/target"),
			resource: ociImageResource("my-image", "1.0.0", "oci://ghcr.io/org/image:v1"),
			uploaders: []transferv1alpha1.UploaderConfig{&transferv1alpha1.OCIUploaderConfig{
				ImageReference: `${target.baseUrl +}`,
			}},
			wantErr: "invalid imageReference",
		},
		{
			name:     "non-applicable OCI uploader falls through to the next uploader",
			target:   testOCIRepo("ghcr.io/target"),
			resource: wgetResource("blob", "1.0.0", "https://source.example/blob.tar"),
			uploaders: []transferv1alpha1.UploaderConfig{
				&transferv1alpha1.OCIUploaderConfig{},
				wgetUploader(t, `${"https://target.example" + url(resource.access.url).path}`),
			},
			wantTypes: []runtime.Type{wgetv1alpha1.HTTPStreamingV1alpha1, ociv1alpha1.OCIAddComponentVersionV1alpha1},
		},
		{
			name:     "OCI uploader scoped by match leaves other resources to the default handling",
			target:   testOCIRepo("ghcr.io/target"),
			resource: ociImageResource("my-image", "1.0.0", "oci://ghcr.io/org/image:v1"),
			uploaders: []transferv1alpha1.UploaderConfig{&transferv1alpha1.OCIUploaderConfig{
				MatchSpec: &transferv1alpha1.UploaderMatch{Name: "other"},
			}},
			wantTypes: []runtime.Type{ociv1alpha1.OCIAddComponentVersionV1alpha1},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			desc := testDescriptor("ocm.software/test", "1.0.0", []descriptor.Resource{tc.resource}, nil)
			resolver := testResolverFor("ocm.software/test", "1.0.0", testOCIRepo("ghcr.io/source"), desc)
			roots := testTransferRoots("ocm.software/test", "1.0.0", tc.target, resolver)
			copyMode := tc.copyMode
			if copyMode == "" {
				copyMode = transferv1alpha1.CopyModeLocalBlobResources
			}

			tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{CopyMode: copyMode}, tc.uploaders)
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(tc.wantTypes, transformationTypes(tgd))
			if tc.wantImageRef != "" {
				r.Equal(tc.wantImageRef, evaluateTemplate(t, tgd, specImageReference(t, tgd.Transformations[tc.wantImageRefAt])))
			}
			if tc.wantCleanup > 0 {
				r.Len(cleanupFileExpressions(t, findCleanupTransformation(tgd)), tc.wantCleanup)
			}
		})
	}
}
