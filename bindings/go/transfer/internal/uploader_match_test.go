package internal

import (
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

func TestIsType(t *testing.T) {
	s3v2 := descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "models", Version: "1.0.0"}},
		Type:        "blob",
		Relation:    descriptor.ExternalRelation,
		Access:      &runtime.Raw{Type: runtime.NewVersionedType("s3", "v2"), Data: []byte(`{"type":"s3/v2","bucketName":"b","objectKey":"k"}`)},
	}
	ociArtifact := ociImageResource("image", "1.0.0", "ghcr.io/org/image:v1") // ociArtifact/v1
	wget := wgetResource("docs", "1.0.0", "https://source.example/docs.tar")

	for _, tc := range []struct {
		name     string
		resource descriptor.Resource
		arg      string
		want     bool
		wantErr  string
	}{
		{"canonical name matches a legacy alias", ociArtifact, `"OCIImage"`, true, ""},
		{"versioned alias matches a legacy alias", ociArtifact, `"ociImage/v1"`, true, ""},
		{"local blob by canonical name", localBlobResource("notes", "1.0.0"), `"LocalBlob"`, true, ""},
		{"list matches any element", helmResource("chart", "1.0.0", "https://charts.example/stable", "app"), `["OCIImage", "Helm"]`, true, ""},
		{"list matches a later element", helmResource("chart", "1.0.0", "https://charts.example/stable", "app"), `["Wget", "Helm"]`, true, ""},
		{"versioned argument requires the same version", s3v2, `"S3/v1"`, false, ""},
		{"unversioned argument matches any version", s3v2, `"S3"`, true, ""},
		{"lower-case alias", wget, `"wget"`, true, ""},
		{"unregistered type matches by name", customAccessResource("custom", "1.0.0"), `"Custom"`, true, ""},
		{"unregistered type does not match another type", customAccessResource("custom", "1.0.0"), `"LocalBlob"`, false, ""},
		{"empty type is an error", wget, `""`, false, "empty type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			desc := testDescriptor("ocm.software/test", "1.0.0", []descriptor.Resource{tc.resource}, nil)
			v2desc, err := descriptor.ConvertToV2(runtime.NewScheme(runtime.WithAllowUnknown()), desc)
			r.NoError(err)
			tgd := &transformv1alpha1.TransformationGraphDefinition{Environment: &runtime.Unstructured{Data: map[string]any{}}}
			r.NoError(addDescriptorToEnvironment(v2desc, "base", tgd))
			env := &uploaderEnv{baseID: "base", node: tgd.Environment.Data["base"]}
			aliases, err := uploaderAliases(env, 0, testOCIRepo("ghcr.io/target"))
			r.NoError(err)

			got, err := matches("resource.access.isType("+tc.arg+")", aliases, env)
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(tc.want, got)
		})
	}
}
