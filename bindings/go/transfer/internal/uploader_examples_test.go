package internal

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	ocispecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	wgetv1alpha1 "ocm.software/open-component-model/bindings/go/wget/transformation/spec/v1alpha1"
)

// examplesResources returns the fixture resources of the documented uploader selection
// examples (website/content/docs/reference/transfer-configuration.md, "Selection
// examples").
func examplesResources() []descriptor.Resource {
	app := ociImageResource("app", "1.0.0", "ghcr.io/acme/app:1.0.0")
	app.Labels = []descriptor.Label{{Name: "ocm.software/transfer", Value: json.RawMessage(`"oci"`)}}

	bundle := dockerManifestLocalBlobResource("bundle", "1.0.0")
	bundleAccess := bundle.Access.(*descriptorv2.LocalBlob)
	bundleAccess.MediaType = ocispecv1.MediaTypeImageManifest
	bundleAccess.ReferenceName = "acme/bundle:1.0.0"

	return []descriptor.Resource{
		app,
		ociImageResource("nginx", "1.0.0", "docker.io/library/nginx:1.25"),
		helmResource("chart", "1.0.0", "https://charts.acme.io/stable", "app"),
		bundle,
		localBlobResource("notes", "1.0.0"),
		wgetResource("docs", "1.0.0", "https://docs.acme.io/guide.tar"),
	}
}

// examplesResource returns the named fixture resource.
func examplesResource(t *testing.T, name string) descriptor.Resource {
	t.Helper()
	for _, res := range examplesResources() {
		if res.Name == name {
			return res
		}
	}
	t.Fatalf("no example resource %q", name)
	return descriptor.Resource{}
}

// customAccessResource returns a resource whose access type the transfer does not know.
func customAccessResource(name, version string) descriptor.Resource {
	return descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: name, Version: version}},
		Type:        "blob",
		Relation:    descriptor.ExternalRelation,
		Access:      &runtime.Raw{Type: runtime.NewVersionedType("Custom", "v1"), Data: []byte(`{"type":"Custom/v1"}`)},
	}
}

// completeExampleConfig is the complete config example of the transfer configuration
// reference ("Complete Example").
const completeExampleConfig = `
type: generic.config.ocm.software/v1
configurations:
  - type: transfer.config.ocm.software/v1alpha1
    recursive: -1
  # 1. Keep the large base image by reference (not copied at all).
  #    Declared first, so the catch-all below never sees it.
  - type: reference.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.name == "base-os-image"
  # 2. Stream wget-hosted documentation to an HTTP artifact store.
  - type: http.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("Wget/v1")
    targetURL: '${"https://artifacts.example.com/ocm" + url(resource.access.url).path}'
    method: PUT
  # 3. OCI images, Helm charts and OCI-manifest local blobs become separate OCI
  #    artifacts next to the component version (former uploadType: ociArtifact).
  #    Default match and imageReference.
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
  # 4. Everything the rules above did not select is embedded as a local blob
  #    (former copyMode: allResources). Default match.
  - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
`

// completeExampleResources returns the resources of the complete config example.
func completeExampleResources() []descriptor.Resource {
	config := localBlobResource("config", "1.0.0")
	config.Access.(*descriptorv2.LocalBlob).MediaType = "application/json"
	return []descriptor.Resource{
		ociImageResource("base-os-image", "1.0.0", "ghcr.io/acme/base-os:1.2"),
		wgetResource("docs", "1.0.0", "https://docs.example.com/demo/guide.tar"),
		ociImageResource("app-image", "1.0.0", "ghcr.io/acme/app:1.0.0"),
		helmResource("chart", "1.0.0", "https://charts.acme.io/stable", "app"),
		config,
		githubResource("sources", "1.0.0", "https://github.com/open-component-model/open-component-model", "f58349914e3c775747dc1ee9af1bc83db4652266"),
		{
			ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "models", Version: "1.0.0"}},
			Type:        "blob",
			Relation:    descriptor.ExternalRelation,
			Access:      &runtime.Raw{Type: runtime.NewVersionedType("S3", "v2"), Data: []byte(`{"type":"S3/v2","bucketName":"models","objectKey":"m.bin"}`)},
		},
	}
}

// indentBlock indents every line of s by n spaces, for embedding s in a YAML block scalar.
func indentBlock(s string, n int) string {
	pad := strings.Repeat(" ", n)
	return pad + strings.ReplaceAll(s, "\n", "\n"+pad)
}

// uploaderOutcomes classifies what the graph does with each resource: "oci <ref>" for an
// OCI artifact push (ref evaluated), "local blob" for an embedded local resource, "http"
// for an HTTP upload, and "by reference" when no transformation handles the resource.
func uploaderOutcomes(t *testing.T, tgd *transformv1alpha1.TransformationGraphDefinition, resources []descriptor.Resource) map[string]string {
	t.Helper()
	addOCIArtifact := runtime.NewVersionedType(ociv1alpha1.AddOCIArtifactType, ociv1alpha1.Version)
	got := make(map[string]string, len(resources))
	for _, res := range resources {
		resourceID := identityToTransformationID(res.ToIdentity())
		outcome := "by reference"
		for _, tr := range tgd.Transformations {
			if !strings.HasSuffix(tr.ID, resourceID) {
				continue
			}
			switch tr.Type {
			case ociv1alpha1.TransferOCIArtifactV1alpha1, addOCIArtifact:
				outcome = "oci " + evaluateTemplate(t, tgd, specImageReference(t, tr))
			case ociv1alpha1.OCIAddLocalResourceV1alpha1, ociv1alpha1.CTFAddLocalResourceV1alpha1:
				outcome = "local blob"
			case wgetv1alpha1.HTTPStreamingV1alpha1:
				outcome = "http"
			}
		}
		got[res.Name] = outcome
	}
	return got
}

// TestUploaderExamples executes the documented uploader selection examples: each config
// must produce exactly the documented outcome per resource, or the documented error.
func TestUploaderExamples(t *testing.T) {
	const ociTarget = "ghcr.io/target-org/ocm"
	ctf := func(t *testing.T) runtime.Typed { return testCTFRepo(t.TempDir()) }
	oci := func(*testing.T) runtime.Typed { return testOCIRepo(ociTarget) }

	e1 := map[string]string{
		"app":    "oci ghcr.io/target-org/ocm/acme/app:1.0.0",
		"nginx":  "oci ghcr.io/target-org/ocm/library/nginx:1.25",
		"chart":  "oci ghcr.io/target-org/ocm/stable/app:1.0.0",
		"bundle": "oci ghcr.io/target-org/ocm/acme/bundle:1.0.0",
		"notes":  "local blob",
		"docs":   "by reference",
	}

	tests := []struct {
		name       string
		configYAML string
		target     func(t *testing.T) runtime.Typed
		// resources selects fixture resources by name; nil means all of examplesResources().
		resources []string
		// fixture replaces examplesResources() entirely.
		fixture []descriptor.Resource
		want    map[string]string
		wantErr string
	}{
		{
			name: "E1 default OCI uploader",
			configYAML: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
`,
			target: oci,
			want:   e1,
		},
		{
			name: "E2 the same with every default spelled out",
			configYAML: fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    match: |-
%s
    imageReference: |-
%s
`, indentBlock(transferv1alpha1.DefaultOCIUploaderMatch, 6), indentBlock(transferv1alpha1.DefaultOCIImageReference, 6)),
			target: oci,
			want:   e1,
		},
		{
			name: "E3 Helm charts only",
			configYAML: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    match: target.type == "OCIRepository" && resource.access.isType("Helm")
`,
			target: oci,
			want: map[string]string{
				"app":    "by reference",
				"nginx":  "by reference",
				"chart":  "oci ghcr.io/target-org/ocm/stable/app:1.0.0",
				"bundle": "local blob",
				"notes":  "local blob",
				"docs":   "by reference",
			},
		},
		{
			name: "E4 OCI-manifest local blobs only",
			configYAML: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    match: >-
      target.type == "OCIRepository"
      && resource.access.isType("LocalBlob")
      && has(resource.access.mediaType) && isOCIManifest(resource.access.mediaType)
      && has(resource.access.referenceName)
`,
			target: oci,
			want: map[string]string{
				"app":    "by reference",
				"nginx":  "by reference",
				"chart":  "by reference",
				"bundle": "oci ghcr.io/target-org/ocm/acme/bundle:1.0.0",
				"notes":  "local blob",
				"docs":   "by reference",
			},
		},
		{
			name: "E5 only images from Docker Hub",
			configYAML: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    match: >-
      target.type == "OCIRepository"
      && resource.access.isType("OCIImage")
      && (resource.access.toOCI().host == "docker.io"
        || resource.access.toOCI().host.endsWith(".docker.io"))
`,
			target: oci,
			want: map[string]string{
				"app":    "by reference",
				"nginx":  "oci ghcr.io/target-org/ocm/library/nginx:1.25",
				"chart":  "by reference",
				"bundle": "local blob",
				"notes":  "local blob",
				"docs":   "by reference",
			},
		},
		{
			name: "E6 select by label",
			configYAML: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    match: >-
      target.type == "OCIRepository"
      && resource.access.isType("OCIImage")
      && has(resource.labels)
      && resource.labels.exists(l, l.name == "ocm.software/transfer" && l.value == "oci")
`,
			target: oci,
			want: map[string]string{
				"app":    "oci ghcr.io/target-org/ocm/acme/app:1.0.0",
				"nginx":  "by reference",
				"chart":  "by reference",
				"bundle": "local blob",
				"notes":  "local blob",
				"docs":   "by reference",
			},
		},
		{
			name: "E7 CTF target, images mirrored to a registry",
			configYAML: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("OCIImage")
    imageReference: '${"registry.example.com/mirror/" + resource.access.toOCI().repository + ":" + resource.access.toOCI().tag}'
`,
			target: ctf,
			want: map[string]string{
				"app":    "oci registry.example.com/mirror/acme/app:1.0.0",
				"nginx":  "oci registry.example.com/mirror/library/nginx:1.25",
				"chart":  "by reference",
				"bundle": "local blob",
				"notes":  "local blob",
				"docs":   "by reference",
			},
		},
		{
			name: "E8 relocate one resource, default for the rest",
			configYAML: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.name == "app"
    imageReference: ghcr.io/target-org/special/app:1.0.0
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
`,
			target: oci,
			want: map[string]string{
				"app":    "oci ghcr.io/target-org/special/app:1.0.0",
				"nginx":  e1["nginx"],
				"chart":  e1["chart"],
				"bundle": e1["bundle"],
				"notes":  e1["notes"],
				"docs":   e1["docs"],
			},
		},
		{
			name: "E9 selected access type the OCI uploader cannot upload",
			configYAML: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("Wget")
`,
			target:    oci,
			resources: []string{"docs"},
			wantErr:   "oci uploader cannot upload access type Wget/v1",
		},
		{
			name: "E9 selected local blob that is not an OCI manifest",
			configYAML: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("LocalBlob")
`,
			target:    oci,
			resources: []string{"notes"},
			wantErr:   "not an OCI manifest",
		},
		{
			name: "E9 match that does not compile",
			configYAML: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.name ==
`,
			target:    oci,
			resources: []string{"app"},
			wantErr:   "invalid match",
		},
		{
			name: "E9 match that is not a bool",
			configYAML: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    match: '"yes"'
`,
			target:    oci,
			resources: []string{"app"},
			wantErr:   "must evaluate to a bool",
		},
		{
			name: "E9 default imageReference on a CTF target",
			configYAML: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("OCIImage")
`,
			target:    ctf,
			resources: []string{"app"},
			wantErr:   "imageReference does not evaluate",
		},
		{
			name: "E10 copy everything as local blobs",
			configYAML: `
type: generic.config.ocm.software/v1
configurations:
  - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
`,
			target: oci,
			want: map[string]string{
				"app":    "local blob",
				"nginx":  "local blob",
				"chart":  "local blob",
				"bundle": "local blob",
				"notes":  "local blob",
				"docs":   "local blob",
			},
		},
		{
			name: "E11 OCI artifacts plus everything else copied",
			configYAML: `
type: generic.config.ocm.software/v1
configurations:
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
  - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
`,
			target: oci,
			want: map[string]string{
				"app":    e1["app"],
				"nginx":  e1["nginx"],
				"chart":  e1["chart"],
				"bundle": e1["bundle"],
				"notes":  "local blob",
				"docs":   "local blob",
			},
		},
		{
			name: "E12 exclude one resource from a catch-all",
			configYAML: `
type: generic.config.ocm.software/v1
configurations:
  - type: reference.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.name == "nginx"
  - type: oci.uploader.transfer.config.ocm.software/v1alpha1
  - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
`,
			target: oci,
			want: map[string]string{
				"app":    e1["app"],
				"nginx":  "by reference",
				"chart":  e1["chart"],
				"bundle": e1["bundle"],
				"notes":  "local blob",
				"docs":   "local blob",
			},
		},
		{
			name: "E13 keep Docker Hub images by reference, copy the rest",
			configYAML: `
type: generic.config.ocm.software/v1
configurations:
  - type: reference.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("OCIImage") && (resource.access.toOCI().host == "docker.io" || resource.access.toOCI().host.endsWith(".docker.io"))
  - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
`,
			target: oci,
			want: map[string]string{
				"app":    "local blob",
				"nginx":  "by reference",
				"chart":  "local blob",
				"bundle": "local blob",
				"notes":  "local blob",
				"docs":   "local blob",
			},
		},
		{
			name: "E14 copy only wget downloads",
			configYAML: `
type: generic.config.ocm.software/v1
configurations:
  - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
    match: resource.access.isType("Wget")
`,
			target: oci,
			want: map[string]string{
				"app":    "by reference",
				"nginx":  "by reference",
				"chart":  "by reference",
				"bundle": "local blob",
				"notes":  "local blob",
				"docs":   "local blob",
			},
		},
		{
			name: "E15 a local blob cannot be kept by reference",
			configYAML: `
type: generic.config.ocm.software/v1
configurations:
  - type: reference.uploader.transfer.config.ocm.software/v1alpha1
    match: "true"
`,
			target:    oci,
			resources: []string{"bundle"},
			wantErr:   "local blobs cannot be kept by reference",
		},
		{
			name: "E15 the local blob uploader cannot copy an unknown access type",
			configYAML: `
type: generic.config.ocm.software/v1
configurations:
  - type: localblob.uploader.transfer.config.ocm.software/v1alpha1
    match: "true"
`,
			target:  oci,
			fixture: []descriptor.Resource{customAccessResource("custom", "1.0.0")},
			wantErr: "local blob uploader cannot copy access type Custom/v1",
		},
		{
			name:       "E16 complete config to an OCI registry",
			configYAML: completeExampleConfig,
			target:     oci,
			fixture:    completeExampleResources(),
			want: map[string]string{
				"base-os-image": "by reference",
				"docs":          "http",
				"app-image":     "oci ghcr.io/target-org/ocm/acme/app:1.0.0",
				"chart":         "oci ghcr.io/target-org/ocm/stable/app:1.0.0",
				"config":        "local blob",
				"sources":       "local blob",
				"models":        "local blob",
			},
		},
		{
			name:       "E16 complete config to a CTF archive",
			configYAML: completeExampleConfig,
			target:     ctf,
			fixture:    completeExampleResources(),
			want: map[string]string{
				"base-os-image": "by reference",
				"docs":          "http",
				"app-image":     "local blob",
				"chart":         "local blob",
				"config":        "local blob",
				"sources":       "local blob",
				"models":        "local blob",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			var generic genericv1.Config
			r.NoError(genericv1.Scheme.Decode(strings.NewReader(tc.configYAML), &generic))
			cfg, err := transferv1alpha1.LookupConfig(&generic)
			r.NoError(err)
			if cfg == nil {
				cfg = &transferv1alpha1.Config{}
			}
			uploaders, err := transferv1alpha1.LookupUploaderConfigs(&generic)
			r.NoError(err)

			resources := examplesResources()
			if tc.fixture != nil {
				resources = tc.fixture
			}
			if tc.resources != nil {
				resources = nil
				for _, name := range tc.resources {
					resources = append(resources, examplesResource(t, name))
				}
			}

			desc := testDescriptor("ocm.software/demo", "1.0.0", resources, nil)
			resolver := testResolverFor("ocm.software/demo", "1.0.0", testOCIRepo("ghcr.io/source"), desc)
			roots := testTransferRoots("ocm.software/demo", "1.0.0", tc.target(t), resolver)

			tgd, err := BuildGraphDefinition(t.Context(), roots, *cfg, uploaders)
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(tc.want, uploaderOutcomes(t, tgd, resources))
		})
	}
}
