package integration_test

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	helminput "ocm.software/open-component-model/bindings/go/helm/input"
	helmresource "ocm.software/open-component-model/bindings/go/helm/repository/resource"
	helmaccess "ocm.software/open-component-model/bindings/go/helm/spec/access"
	helmaccessv1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	helminputv1 "ocm.software/open-component-model/bindings/go/helm/spec/input/v1"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ctfrepospec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/artifactory/artifactorytest"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

// Test_Integration_TransferLocalBlobHelmResource_ArtifactoryHelmUploaderDeploysChart verifies that a
// LocalBlob/v1 resource containing a Helm chart is correctly streamed to the fake Artifactory
// endpoint by the Artifactory helm uploader. Two media types are tested:
//   - packaged chart: the blob is the chart .tgz itself, uploaded unchanged.
//   - OCI layout: the blob is an OCI image layout (as produced by the helm input), and only
//     the chart layer is extracted and uploaded.
func Test_Integration_TransferLocalBlobHelmResource_ArtifactoryHelmUploaderDeploysChart(t *testing.T) {
	t.Parallel()

	// Read the expected chart .tgz bytes for comparison.
	chartTgzBytes, err := os.ReadFile("../../helm/testdata/mychart-0.1.0.tgz")
	require.NoError(t, err)
	require.NotEmpty(t, chartTgzBytes)

	// Build the OCI layout blob using the real helm input path. Using the packaged tgz (not the
	// chart directory) ensures the chart layer bytes inside the layout match chartTgzBytes exactly.
	ociLayoutBlob, _, err := helminput.GetV1HelmBlob(t.Context(), helminputv1.Helm{
		Path: "../../helm/testdata/mychart-0.1.0.tgz",
	}, t.TempDir())
	require.NoError(t, err)

	ociLayoutRC, err := ociLayoutBlob.ReadCloser()
	require.NoError(t, err)
	ociLayoutBytes, err := io.ReadAll(ociLayoutRC)
	require.NoError(t, err)
	require.NoError(t, ociLayoutRC.Close())

	tests := []struct {
		name      string
		mediaType string
		blobData  []byte
		// wantBody is what the PUT body should equal. For a packaged chart it is the chart
		// tgz; for an OCI layout it is the chart layer (which equals the chart tgz).
		wantBody []byte
	}{
		{
			name:      "packaged chart",
			mediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip",
			blobData:  chartTgzBytes,
			wantBody:  chartTgzBytes,
		},
		{
			name:      "OCI layout",
			mediaType: "application/vnd.ocm.software.oci.layout.v1+tar+gzip",
			blobData:  ociLayoutBytes,
			wantBody:  chartTgzBytes,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			// Target "Artifactory": records chart properties for the chart .tgz like Artifactory does.
			targetSrv := artifactorytest.New(t, map[string][2]string{digestOf(chartTgzBytes).Encoded(): {"mychart", "0.1.0"}})

			componentName := "ocm.software/jfrog-helm-local-blob-test"
			componentVersion := "1.0.0"
			sourceCTFPath := t.TempDir()
			ctfRepo := createCTFRepository(t, sourceCTFPath)

			// Build a LocalBlob resource with the given media type.
			res := descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{Name: "mychart", Version: "0.1.0"},
				},
				Type:     "helmChart",
				Relation: descriptor.LocalRelation,
				Access: &descriptorv2.LocalBlob{
					Type:      runtime.NewVersionedType(descriptorv2.LocalBlobAccessType, descriptorv2.LocalBlobAccessTypeVersion),
					MediaType: tt.mediaType,
				},
			}
			updatedResource, err := ctfRepo.AddLocalResource(t.Context(), componentName, componentVersion, &res, inmemory.New(bytes.NewReader(tt.blobData), inmemory.WithMediaType(tt.mediaType)))
			r.NoError(err)

			desc := &descriptor.Descriptor{
				Meta: descriptor.Meta{Version: "v2"},
				Component: descriptor.Component{
					ComponentMeta: descriptor.ComponentMeta{
						ObjectMeta: descriptor.ObjectMeta{Name: componentName, Version: componentVersion},
					},
					Provider:  descriptor.Provider{Name: "test-provider"},
					Resources: []descriptor.Resource{*updatedResource},
				},
			}
			r.NoError(ctfRepo.AddComponentVersion(t.Context(), desc))

			sourceSpec := &ctfrepospec.Repository{
				Type:     runtime.Type{Name: ctfrepospec.Type, Version: ctfrepospec.Version},
				FilePath: sourceCTFPath,
			}
			targetCTFPath := t.TempDir()
			targetSpec := &ctfrepospec.Repository{
				Type:       runtime.Type{Name: ctfrepospec.Type, Version: ctfrepospec.Version},
				FilePath:   targetCTFPath,
				AccessMode: "readwrite|create",
			}

			// The Artifactory uploader routes LocalBlob/v1 resources to the fake Artifactory server.
			uploaders := []transferv1alpha1.UploaderConfig{&transferv1alpha1.ArtifactoryUploaderConfig{
				Type:       runtime.NewVersionedType(transferv1alpha1.ArtifactoryUploaderConfigType, transferv1alpha1.Version),
				MatchSpec:  transferv1alpha1.UploaderMatch{AccessType: runtime.NewVersionedType(descriptorv2.LocalBlobAccessType, descriptorv2.LocalBlobAccessTypeVersion)},
				URL:        targetSrv.URL,
				Repository: "helm-local",
			}}

			tgd, err := transfer.BuildGraphDefinition(t.Context(),
				&transferv1alpha1.Config{CopyMode: transferv1alpha1.CopyModeAllResources},
				uploaders,
				transfer.Mapping{
					Components: []transfer.ComponentID{{Component: componentName, Version: componentVersion}},
					Target:     targetSpec,
					Resolver:   transfer.NewRepositoryResolver(ctfRepo, sourceSpec),
				},
			)
			r.NoError(err)
			r.NotNil(tgd)

			ctx := t.Context()
			repoProvider := provider.NewComponentVersionRepositoryProvider(provider.WithTempDir(t.TempDir()))
			resourceRepo := helmresource.NewResourceRepository(nil)
			b := transfer.NewDefaultBuilder(repoProvider, resourceRepo, nil)
			graph, err := b.BuildAndCheck(tgd)
			r.NoError(err)
			r.NoError(graph.Process(ctx))

			// The target server must have received the chart under the component version path.
			expectedPath := "/artifactory/helm-local/ocm.software/jfrog-helm-local-blob-test/1.0.0/mychart-0.1.0.tgz"
			var put *artifactorytest.Request
			for _, req := range targetSrv.Recorded() {
				if req.Method == http.MethodPut && !req.Deploy && req.Path == expectedPath {
					put = &req
				}
			}
			r.NotNil(put, "target server should have received a PUT at %s", expectedPath)
			r.Equal(tt.wantBody, put.Body, "uploaded bytes must equal the chart .tgz")
			r.Equal("application/gzip", put.ContentType, "Content-Type must be application/gzip")

			// Verify the transferred descriptor in the target CTF.
			gotDesc, err := createCTFRepository(t, targetCTFPath).GetComponentVersion(ctx, componentName, componentVersion)
			r.NoError(err)
			r.Len(gotDesc.Component.Resources, 1)
			gotResource := gotDesc.Component.Resources[0]

			// The published access must be Helm/v1.
			r.NotNil(gotResource.Access)
			r.Equal(helmaccessv1.Type, gotResource.Access.GetType().Name,
				"uploaded resource should carry a Helm access")

			var typedHelm helmaccessv1.Helm
			r.NoError(helmaccess.Scheme.Convert(gotResource.Access, &typedHelm))
			r.Equal(targetSrv.URL+"/artifactory/api/helm/helm-local", typedHelm.HelmRepository,
				"helmRepository should point at the Artifactory Helm API")
			r.Equal("mychart:0.1.0", typedHelm.HelmChart,
				"helmChart should be <name>:<version>")

			// The digest must be genericBlobDigest/v1 of the chart .tgz.
			r.NotNil(gotResource.Digest, "transferred resource should carry a digest")
			r.Equal("SHA-256", gotResource.Digest.HashAlgorithm)
			r.Equal("genericBlobDigest/v1", gotResource.Digest.NormalisationAlgorithm)
			r.Equal(digestOf(tt.wantBody).Encoded(), gotResource.Digest.Value,
				"digest value should be the sha256 of the chart .tgz")
		})
	}
}
