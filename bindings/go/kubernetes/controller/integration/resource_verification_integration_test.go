package integration_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/ctf"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/setup"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	componentVersion = "v1.0.0"
	resourceName     = "data"
)

// verificationCase is a component version with one resource, named resourceName, and what
// the Resource controller reports for it.
type verificationCase struct {
	name     string
	resource descruntime.Resource
	// wantDigest is the digest the Resource reports once Ready; without it, the Resource
	// must fail with a message containing all of wantMessages.
	wantDigest   *v2.Digest
	wantMessages []string
}

// Test_Integration_ResourceVerification runs the Resource controller, with the plugins it
// registers in production, against real backends of every access type it verifies: each
// resource becomes Ready if the content matches the digest its descriptor records, and
// fails otherwise. GitHub access is not covered: its digest processor only talks to the
// GitHub API, which needs the internet.
func Test_Integration_ResourceVerification(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)

	k8sClient := startResourceController(t)

	registryAddress := startRegistry(t, ctx)
	ociDigest := pushArtifact(t, ctx, registryAddress, "verification/artifact", "v1.0.0", []byte("oci artifact content"))

	s3Endpoint := startRustFS(t, ctx)
	s3Content := []byte("s3 object content")
	const s3Bucket, s3Key = "verification", "path/to/object.txt"
	putS3Object(t, ctx, s3Endpoint, s3Bucket, s3Key, s3Content)

	wgetContent := []byte("wget file content")
	chart, err := os.ReadFile(filepath.Join("..", "..", "..", "helm", "testdata", "mychart-0.1.0.tgz"))
	r.NoError(err)
	chartDigest := godigest.FromBytes(chart)
	// Chart URLs in the indexes are relative to the repository.
	fileServer := startFileServer(t, map[string][]byte{
		"/file.txt":                         wgetContent,
		"/with-digest/index.yaml":           helmIndex("mychart-0.1.0.tgz", chartDigest),
		"/with-digest/mychart-0.1.0.tgz":    chart,
		"/without-digest/index.yaml":        helmIndex("mychart-0.1.0.tgz", ""),
		"/without-digest/mychart-0.1.0.tgz": chart,
	})
	trustedCAs := certificatePEM(fileServer)

	gitPath, gitCommit := newGitRepository(t, "verification\n")
	gitURL, gitServer := startGitServer(t, gitPath)
	if gitServer != nil {
		trustedCAs += certificatePEM(gitServer)
	}

	// The servers are only trusted through the controller's HTTP configuration, and the S3
	// server only accepts the credentials the controller resolves for it.
	s3URL, err := url.Parse(s3Endpoint)
	r.NoError(err)
	ocmConfig := mustJSON(t, map[string]any{
		"type": "generic.config.ocm.software/v1",
		"configurations": []any{
			map[string]any{
				"type":       "http.config.ocm.software/v1alpha1",
				"rootCAsPEM": trustedCAs,
			},
			map[string]any{
				"type": "credentials.config.ocm.software",
				"consumers": []any{map[string]any{
					"identity": map[string]any{
						"type":     "S3",
						"scheme":   s3URL.Scheme,
						"hostname": s3URL.Hostname(),
						"port":     s3URL.Port(),
					},
					"credentials": []any{map[string]any{
						"type":            "S3Credentials/v1",
						"accessKeyId":     rustfsAccessKey,
						"secretAccessKey": rustfsSecretKey,
					}},
				}},
			},
		},
	})

	ctfPath := t.TempDir()
	fs, err := filesystem.NewFS(ctfPath, os.O_RDWR)
	r.NoError(err)
	repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))), oci.WithTempDir(t.TempDir()))
	r.NoError(err)

	ociAccess := rawAccess(t, map[string]any{
		"type":           "ociArtifact/v1",
		"imageReference": "http://" + registryAddress + "/verification/artifact:v1.0.0",
	})
	s3Access := rawAccess(t, map[string]any{
		"type":         "S3/v2",
		"region":       s3Region,
		"endpoint":     s3Endpoint,
		"usePathStyle": true,
		"bucketName":   s3Bucket,
		"objectKey":    s3Key,
	})
	wgetAccess := rawAccess(t, map[string]any{
		"type": "wget/v1",
		"url":  fileServer.URL + "/file.txt",
	})
	helmAccess := func(repository string) *runtime.Raw {
		return rawAccess(t, map[string]any{
			"type":           "helm/v1",
			"helmRepository": fileServer.URL + "/" + repository,
			"helmChart":      "mychart:0.1.0",
		})
	}

	ociArtifactDigest := &v2.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: descruntime.OCIArtifactDigestV1, Value: ociDigest.Encoded()}
	var cases []verificationCase
	cases = append(cases, externalCases("oci", "ociArtifact", ociAccess, ociArtifactDigest)...)
	cases = append(cases, externalCases("s3", "blob", s3Access, blobDigest(godigest.FromBytes(s3Content)))...)
	cases = append(cases, externalCases("wget", "blob", wgetAccess, blobDigest(godigest.FromBytes(wgetContent)))...)
	cases = append(cases, externalCases("helm-index-digest", "helmChart", helmAccess("with-digest"), blobDigest(chartDigest))...)
	cases = append(cases, externalCases("helm-chart-download", "helmChart", helmAccess("without-digest"), blobDigest(chartDigest))...)
	if gitServer != nil {
		gitAccess := rawAccess(t, map[string]any{
			"type":       "Git/v1",
			"repository": gitURL,
			"commit":     gitCommit.String(),
		})
		cases = append(cases, externalCases("git", "directoryTree", gitAccess, gitArchiveDigest(t, ocmConfig, gitAccess))...)
	} else {
		t.Log("no git binary installed to serve the git repository, skipping the git cases")
	}
	cases = append(cases, localBlobCases(t, repo)...)
	cases = append(cases, verificationCase{
		name: "unsupported-access",
		resource: descruntime.Resource{
			Type:     "blob",
			Relation: descruntime.ExternalRelation,
			Access:   rawAccess(t, map[string]any{"type": "unknownAccess/v1"}),
		},
		wantMessages: []string{"cannot be verified"},
	})

	for i := range cases {
		res := &cases[i].resource
		res.Name, res.Version = resourceName, componentVersion
		r.NoError(repo.AddComponentVersion(ctx, &descruntime.Descriptor{
			Meta: descruntime.Meta{Version: "v2"},
			Component: descruntime.Component{
				ComponentMeta: descruntime.ComponentMeta{ObjectMeta: descruntime.ObjectMeta{Name: componentName(cases[i].name), Version: componentVersion}},
				Provider:      descruntime.Provider{Name: "ocm.software"},
				Resources:     []descruntime.Resource{*res},
			},
		}))
	}
	repositorySpec := mustJSON(t, &ctfv1.Repository{
		Type:       runtime.NewVersionedType(ctfv1.Type, ctfv1.Version),
		FilePath:   ctfPath,
		AccessMode: ctfv1.AccessModeReadOnly,
	})

	namespace := "resource-verification"
	r.NoError(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}))
	configRef := createOCMConfigSecret(t, ctx, k8sClient, namespace, "ocm-config", ocmConfig)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			component := createReadyComponent(t, ctx, k8sClient, namespace, tc.name, componentName(tc.name), componentVersion, repositorySpec)
			resource := &v1alpha1.Resource{
				ObjectMeta: metav1.ObjectMeta{Name: tc.name, Namespace: namespace},
				Spec: v1alpha1.ResourceSpec{
					ComponentRef: corev1.LocalObjectReference{Name: component.GetName()},
					Resource: v1alpha1.ResourceID{
						ByReference: v1alpha1.ResourceReference{Resource: runtime.Identity{"name": resourceName}},
					},
					OCMConfig: []v1alpha1.OCMConfiguration{configRef},
				},
			}
			r.NoError(k8sClient.Create(ctx, resource))

			if tc.wantDigest == nil {
				waitForNotReady(t, ctx, k8sClient, resource, v1alpha1.GetOCMResourceFailedReason, tc.wantMessages...)
				return
			}
			waitForReady(t, ctx, k8sClient, resource)
			r.NotNil(resource.Status.Resource)
			r.Equal(tc.wantDigest, resource.Status.Resource.Digest)
		})
	}
}

// externalCases are the cases of a resource of access, recording the digest of its content
// (verified) and another digest (failed).
func externalCases(name, typ string, access runtime.Typed, digest *v2.Digest) []verificationCase {
	other := *digest
	other.Value = godigest.FromString("other").Encoded()
	resource := func(digest *v2.Digest) descruntime.Resource {
		return descruntime.Resource{
			Type:     typ,
			Relation: descruntime.ExternalRelation,
			Access:   access,
			Digest: &descruntime.Digest{
				HashAlgorithm:          digest.HashAlgorithm,
				NormalisationAlgorithm: digest.NormalisationAlgorithm,
				Value:                  digest.Value,
			},
		}
	}
	return []verificationCase{
		{name: name, resource: resource(digest), wantDigest: digest},
		{name: name + "-other-digest", resource: resource(&other), wantMessages: []string{"failed processing resource digest", "mismatch", other.Value}},
	}
}

// localBlobCases are the cases of a resource stored with its component version, recording
// the digest the repository stores it under (verified) and another digest (failed).
func localBlobCases(t *testing.T, repo *oci.Repository) []verificationCase {
	t.Helper()
	r := require.New(t)

	var cases []verificationCase
	for _, tc := range []struct {
		name        string
		otherDigest bool
	}{
		{name: "local-blob"},
		{name: "local-blob-other-digest", otherDigest: true},
	} {
		stored, err := repo.AddLocalResource(t.Context(), componentName(tc.name), componentVersion, &descruntime.Resource{
			ElementMeta: descruntime.ElementMeta{ObjectMeta: descruntime.ObjectMeta{Name: resourceName, Version: componentVersion}},
			Type:        "plainText",
			Relation:    descruntime.LocalRelation,
			Access: &v2.LocalBlob{
				Type:      runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
				MediaType: "text/plain",
			},
		}, inmemory.New(bytes.NewReader([]byte("local blob content"))))
		r.NoError(err)
		r.Equal(godigest.FromString("local blob content").Encoded(), stored.Digest.Value)

		c := verificationCase{name: tc.name, resource: *stored}
		if tc.otherDigest {
			c.resource.Digest.Value = godigest.FromString("other").Encoded()
			c.wantMessages = []string{"failed to verify local resource", "mismatch", c.resource.Digest.Value}
		} else {
			c.wantDigest = descruntime.ConvertToV2Digest(stored.Digest)
		}
		cases = append(cases, c)
	}
	return cases
}

// gitArchiveDigest is the digest of the archive the git plugin builds of access. The
// archive format is the plugin's, so the digest is taken from the plugin the controller
// registers, configured as in the controller.
func gitArchiveDigest(t *testing.T, ocmConfig []byte, access runtime.Typed) *v2.Digest {
	t.Helper()
	r := require.New(t)

	cfg := &genericv1.Config{}
	r.NoError(json.Unmarshal(ocmConfig, cfg))
	pm, err := setup.NewPluginManager(t.Context(), cfg, slog.Default(), setup.WithTempDir(t.TempDir()))
	r.NoError(err)
	t.Cleanup(func() { r.NoError(pm.Shutdown(t.Context())) })

	res := &descruntime.Resource{Access: access}
	processor, err := pm.DigestProcessorRegistry.GetPlugin(t.Context(), access)
	r.NoError(err)
	processed, err := processor.ProcessResourceDigest(t.Context(), res, nil)
	r.NoError(err)
	return descruntime.ConvertToV2Digest(processed.Digest)
}

func blobDigest(digest godigest.Digest) *v2.Digest {
	return &v2.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: descruntime.GenericBlobDigestV1, Value: digest.Encoded()}
}

func componentName(caseName string) string {
	return "ocm.software/verification/" + caseName
}

// rawAccess is the access spec of fields, as a descriptor read from a repository holds it.
func rawAccess(t *testing.T, fields map[string]any) *runtime.Raw {
	t.Helper()
	raw := &runtime.Raw{}
	require.NoError(t, json.Unmarshal(mustJSON(t, fields), raw))
	return raw
}
