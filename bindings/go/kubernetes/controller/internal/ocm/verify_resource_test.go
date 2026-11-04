package ocm_test

import (
	"bytes"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/ocm"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/setup"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// content is what every fake source serves.
var content = []byte("verified content")

// The controller's plugin manager verifies every built-in access type against the digest a
// resource records: the digest it computes is accepted, any other digest is rejected.
var _ = Describe("VerifyResource", Ordered, func() {
	var (
		pm      *manager.PluginManager
		servers map[string]string
		gitRepo string
		commit  string
	)

	BeforeAll(func(ctx SpecContext) {
		mux := http.NewServeMux()
		srv := httptest.NewTLSServer(mux)
		DeferCleanup(srv.Close)
		host := strings.TrimPrefix(srv.URL, "https://")

		// wget
		mux.HandleFunc("/files/content", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(content) })
		// S3, addressed path-style
		mux.HandleFunc("/bucket/object", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(content) })
		// GitHub (enterprise API on the same host): the tarball endpoint redirects to the archive.
		mux.HandleFunc("/api/v3/repos/acme/app/tarball/", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://"+r.Host+"/codeload/acme/app", http.StatusFound)
		})
		mux.HandleFunc("/codeload/acme/app", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(content) })
		// Helm HTTP repository whose index lists the chart digest
		mux.HandleFunc("/charts/index.yaml", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprintf(w, "apiVersion: v1\nentries:\n  app:\n    - urls: [%s/charts/app-1.0.0.tgz]\n      name: app\n      version: 1.0.0\n      apiVersion: v2\n      digest: %s\n",
				srv.URL, digest.FromBytes(content).Encoded())
		})
		// OCI registry with one image, test/app:1.0.0
		serveOCIImage(mux, "test/app", "1.0.0")

		servers = map[string]string{"https": srv.URL, "host": host}

		gitRepo, commit = newGitRepository()

		cfg := &genericv1.Config{
			Type: runtime.NewVersionedType(genericv1.ConfigType, genericv1.ConfigTypeV1),
			Configurations: []*runtime.Raw{{
				Type: runtime.NewVersionedType("http.config.ocm.software", "v1alpha1"),
				Data: mustJSON(map[string]any{
					"type":       "http.config.ocm.software/v1alpha1",
					"rootCAsPEM": string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})),
				}),
			}},
		}
		var err error
		pm, err = setup.NewPluginManager(ctx, cfg, slog.Default(), setup.WithTempDir(GinkgoT().TempDir()))
		Expect(err).NotTo(HaveOccurred())

		// The S3 SDK otherwise walks the default credential chain, including instance metadata.
		GinkgoT().Setenv("AWS_ACCESS_KEY_ID", "test")
		GinkgoT().Setenv("AWS_SECRET_ACCESS_KEY", "test")
		GinkgoT().Setenv("AWS_REGION", "us-east-1")
	})

	DescribeTable("accepts the digest of the content and rejects any other",
		func(ctx SpecContext, access func() map[string]any) {
			res := &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "data", Version: "1.0.0"}},
				Type:        "blob",
				Relation:    descriptor.ExternalRelation,
				Access:      &runtime.Raw{Type: runtime.Type{}, Data: mustJSON(access())},
			}
			var typ runtime.Raw
			Expect(json.Unmarshal(res.Access.(*runtime.Raw).Data, &typ)).To(Succeed())
			res.Access.(*runtime.Raw).Type = typ.Type

			By("recording the digest of the content")
			recorded, err := ocm.VerifyResource(ctx, pm, res, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(recorded.Digest).NotTo(BeNil())

			By("verifying against the recorded digest")
			_, err = ocm.VerifyResource(ctx, pm, recorded, nil)
			Expect(err).NotTo(HaveOccurred())

			By("rejecting another digest")
			other := recorded.DeepCopy()
			other.Digest.Value = digest.FromString("other").Encoded()
			_, err = ocm.VerifyResource(ctx, pm, other, nil)
			Expect(err).To(HaveOccurred())
		},
		Entry("OCI image", func() map[string]any {
			return map[string]any{"type": "ociArtifact/v1", "imageReference": servers["host"] + "/test/app:1.0.0"}
		}),
		Entry("Helm chart in an OCI registry", func() map[string]any {
			return map[string]any{"type": "helm/v1", "helmRepository": "oci://" + servers["host"] + "/test", "helmChart": "app:1.0.0"}
		}),
		Entry("Helm chart in an HTTP repository", func() map[string]any {
			return map[string]any{"type": "helm/v1", "helmRepository": servers["https"] + "/charts", "helmChart": "app:1.0.0"}
		}),
		Entry("wget", func() map[string]any {
			return map[string]any{"type": "wget/v1", "url": servers["https"] + "/files/content"}
		}),
		Entry("S3", func() map[string]any {
			return map[string]any{"type": "S3/v2", "endpoint": servers["https"], "usePathStyle": true, "region": "us-east-1", "bucketName": "bucket", "objectKey": "object"}
		}),
		Entry("GitHub", func() map[string]any {
			return map[string]any{"type": "gitHub/v1", "repoUrl": servers["https"] + "/acme/app", "commit": strings.Repeat("a", 40)}
		}),
		Entry("Git", func() map[string]any {
			return map[string]any{"type": "Git/v1", "repository": gitRepo, "commit": commit}
		}),
	)

	It("fails a resource whose access type no digest processor supports", func(ctx SpecContext) {
		res := &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "data"}},
			Access:      &runtime.Raw{Type: runtime.NewVersionedType("unknown", "v1"), Data: []byte(`{"type":"unknown/v1"}`)},
		}
		_, err := ocm.VerifyResource(ctx, pm, res, nil)
		Expect(err).To(MatchError(ContainSubstring("cannot be verified")))
	})

	It("fails a resource without access", func(ctx SpecContext) {
		_, err := ocm.VerifyResource(ctx, pm, &descriptor.Resource{ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "data"}}}, nil)
		Expect(err).To(MatchError(ContainSubstring("has no access")))
	})
})

// serveOCIImage serves one image manifest of repo under tag, with its config blob.
func serveOCIImage(mux *http.ServeMux, repo, tag string) {
	config := []byte(`{}`)
	manifest := mustJSON(ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config:    ociImageSpecV1.Descriptor{MediaType: ociImageSpecV1.MediaTypeEmptyJSON, Digest: digest.FromBytes(config), Size: int64(len(config))},
		Layers:    []ociImageSpecV1.Descriptor{{MediaType: "application/octet-stream", Digest: digest.FromBytes(content), Size: int64(len(content))}},
	})
	objects := map[string][]byte{
		"manifests/" + tag: manifest,
		"manifests/" + digest.FromBytes(manifest).String(): manifest,
		"blobs/" + digest.FromBytes(config).String():       config,
		"blobs/" + digest.FromBytes(content).String():      content,
	}
	mux.HandleFunc("/v2/", func(w http.ResponseWriter, req *http.Request) {
		rest, ok := strings.CutPrefix(req.URL.Path, "/v2/"+repo+"/")
		if req.URL.Path == "/v2/" {
			return
		}
		data := objects[rest]
		if !ok || data == nil {
			http.NotFound(w, req)
			return
		}
		mediaType := "application/octet-stream"
		if strings.HasPrefix(rest, "manifests/") {
			mediaType = ociImageSpecV1.MediaTypeImageManifest
		}
		w.Header().Set("Content-Type", mediaType)
		w.Header().Set("Docker-Content-Digest", digest.FromBytes(data).String())
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		if req.Method != http.MethodHead {
			_, _ = w.Write(data)
		}
	})
}

// newGitRepository creates a bare repository with one commit on main and returns its path
// and commit. Objects are stored directly, so no git configuration (e.g. signing) applies.
func newGitRepository() (string, string) {
	dir := filepath.Join(GinkgoT().TempDir(), "repo.git")
	repo, err := git.PlainInit(dir, true)
	Expect(err).NotTo(HaveOccurred())
	store := func(obj interface {
		Encode(plumbing.EncodedObject) error
	},
	) plumbing.Hash {
		encoded := repo.Storer.NewEncodedObject()
		Expect(obj.Encode(encoded)).To(Succeed())
		hash, err := repo.Storer.SetEncodedObject(encoded)
		Expect(err).NotTo(HaveOccurred())
		return hash
	}
	blob := repo.Storer.NewEncodedObject()
	blob.SetType(plumbing.BlobObject)
	w, err := blob.Writer()
	Expect(err).NotTo(HaveOccurred())
	_, err = w.Write(content)
	Expect(err).NotTo(HaveOccurred())
	Expect(w.Close()).To(Succeed())
	blobHash, err := repo.Storer.SetEncodedObject(blob)
	Expect(err).NotTo(HaveOccurred())
	signature := object.Signature{Name: "test", Email: "test@example.invalid", When: time.Unix(1700000000, 0).UTC()}
	commit := store(&object.Commit{
		Author:    signature,
		Committer: signature,
		Message:   "init\n",
		TreeHash:  store(&object.Tree{Entries: []object.TreeEntry{{Name: "README.md", Mode: filemode.Regular, Hash: blobHash}}}),
	})
	Expect(repo.Storer.SetReference(plumbing.NewHashReference("refs/heads/main", commit))).To(Succeed())
	return dir, commit.String()
}

func mustJSON(v any) []byte {
	data, err := json.Marshal(v)
	Expect(err).NotTo(HaveOccurred())
	return bytes.TrimSpace(data)
}
