package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"
	orasregistry "oras.land/oras-go/v2/registry"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/looseref"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/cmd"
	"ocm.software/open-component-model/cli/integration/internal"
)

// Test_Integration_Ownership verifies ownership (ADR 0016) end-to-end against
// live containerised registries. "ocm add cv" runs the real `ocm add
// component-version` on resources that differ only in options.ownershipPolicy:
// opted-in (Always) resources — by-value and by-reference — gain ownership
// referrers, an opted-out (Never) resource gets none. "ocm transfer" then copies
// the component version to a fresh registry, with and without --copy-resources,
// and asserts which referrers reach the target. Verification goes through the
// OCI Referrers API.
func Test_Integration_Ownership(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	const (
		version   = "v1.0.0"
		component = "ocm.software/test-asset"
	)

	// One component version, built once via the real CLI and shared by both halves
	// of the test. Its three resources differ only in options.ownershipPolicy:
	// backend-image-local (by-value, Always), backend-image-always (by-reference,
	// Always), backend-image-external (by-reference, Never). The by-reference
	// resources point at distinct images hosted in the source registry.
	srcResolver, srcReg := ownershipRegistry(t)
	srcAddr := srcReg.RegistryAddress
	srcRepo := newOwnershipRepository(t, srcResolver)

	constructorDir := t.TempDir()
	writeOCILayoutTarball(t, constructorDir, "hello-ocm.tar.gz", []byte("backend-image-local-payload"))

	ownedImageRef := pushByReferenceImage(t, ctx, srcRepo, "backend-image-always", version,
		fmt.Sprintf("%s/test-asset/backend-image-always:%s", srcAddr, version), []byte("backend-image-payload"))
	externalImageRef := pushByReferenceImage(t, ctx, srcRepo, "backend-image-external", version,
		fmt.Sprintf("%s/test-asset/backend-image-external:%s", srcAddr, version), []byte("backend-image-external-payload"))

	// The http:// scheme makes the CLI talk plain HTTP to the test registry.
	constructorYAML := fmt.Sprintf(`
components:
  - name: %[1]s
    version: %[2]s
    provider:
      name: ocm.software
    resources:
      - name: backend-image-local
        version: %[2]s
        type: ociArtifact
        options:
          ownershipPolicy: Always
        input:
          type: file/v1
          path: hello-ocm.tar.gz
          mediaType: application/vnd.ocm.software.oci.layout.v1+tar+gzip
      - name: backend-image-always
        version: %[2]s
        type: ociArtifact
        options:
          ownershipPolicy: Always
        access:
          type: OCIImage/v1
          imageReference: http://%[3]s
      - name: backend-image-external
        version: %[2]s
        type: ociArtifact
        options:
          ownershipPolicy: Never
        access:
          type: OCIImage/v1
          imageReference: http://%[4]s
`, component, version, ownedImageRef, externalImageRef)

	addComponentVersionViaCLI(t, ctx, srcReg, constructorDir, constructorYAML)

	t.Run("ocm add cv", func(t *testing.T) {
		t.Run("input resource (ownershipPolicy=Always) — ownership referrer is created", func(t *testing.T) {
			subjectRef := resourceSubjectReference(t, ctx, srcResolver, srcRepo, component, version, "backend-image-local")
			assertOwnershipReferrer(t, ctx, srcResolver, subjectRef, component, version, "backend-image-local", true)
		})

		t.Run("imageReference access (ownershipPolicy=Always) — ownership referrer is created", func(t *testing.T) {
			assertOwnershipReferrer(t, ctx, srcResolver, ownedImageRef, component, version, "backend-image-always", true)
		})

		t.Run("imageReference access (ownershipPolicy=Never) — ownership referrer is not created", func(t *testing.T) {
			assertOwnershipReferrer(t, ctx, srcResolver, externalImageRef, component, version, "backend-image-external", false)
		})

		t.Run("by-value create is idempotent (single referrer on re-add)", func(t *testing.T) {
			const (
				component    = "ocm.software/ownership/idempotent"
				resourceName = "backend-image"
			)
			resolver, _ := ownershipRegistry(t)
			repo := newOwnershipRepository(t, resolver)

			// Adding the same by-value resource twice must converge on exactly one
			// referrer; enumerating the Referrers API proves "exactly one".
			addOwnershipResource(t, ctx, repo, component, version, resourceName, true)
			addOwnershipResource(t, ctx, repo, component, version, resourceName, true)

			subjectRef := resourceSubjectReference(t, ctx, resolver, repo, component, version, resourceName)
			assertOwnershipReferrer(t, ctx, resolver, subjectRef, component, version, resourceName, true)
		})

		t.Run("re-add flips ownershipPolicy Never -> Always (referrer appears only after Always)", func(t *testing.T) {
			const (
				component    = "ocm.software/ownership/policy-toggle"
				resourceName = "backend-image-local"
			)
			resolver, reg := ownershipRegistry(t)
			repo := newOwnershipRepository(t, resolver)

			// The same by-value resource is added twice with identical content, so the
			// subject digest is stable; only options.ownershipPolicy changes.
			dir := t.TempDir()
			writeOCILayoutTarball(t, dir, "hello-ocm.tar.gz", []byte("policy-toggle-payload"))
			toggleYAML := func(policy string) string {
				return fmt.Sprintf(`
components:
  - name: %[1]s
    version: %[2]s
    provider:
      name: ocm.software
    resources:
      - name: %[3]s
        version: %[2]s
        type: ociArtifact
        options:
          ownershipPolicy: %[4]s
        input:
          type: file/v1
          path: hello-ocm.tar.gz
          mediaType: application/vnd.ocm.software.oci.layout.v1+tar+gzip
`, component, version, resourceName, policy)
			}

			// Add #1 (Never): no referrer.
			addComponentVersionViaCLI(t, ctx, reg, dir, toggleYAML("Never"))
			subjectRef := resourceSubjectReference(t, ctx, resolver, repo, component, version, resourceName)
			assertOwnershipReferrer(t, ctx, resolver, subjectRef, component, version, resourceName, false)

			// Add #2 (Always, replacing the version): the referrer must now appear on
			// the unchanged subject.
			addComponentVersionViaCLI(t, ctx, reg, dir, toggleYAML("Always"), "--component-version-conflict-policy", "replace")
			assertOwnershipReferrer(t, ctx, resolver, subjectRef, component, version, resourceName, true)
		})

		t.Run("sibling resources get isolated referrers", func(t *testing.T) {
			r := require.New(t)
			const component = "ocm.software/ownership/siblings"
			resolver, _ := ownershipRegistry(t)
			repo := newOwnershipRepository(t, resolver)

			// Two by-value resources with distinct content in one component version:
			// each subject must carry exactly its own referrer, never the sibling's.
			mkRes := func(name string, payload []byte) descriptor.Resource {
				data, _ := createSingleLayerOCIImage(t, payload)
				res, err := repo.AddLocalResource(ctx, component, version, &descriptor.Resource{
					ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: name, Version: version}},
					Type:        "ociArtifact",
					Relation:    descriptor.LocalRelation,
					Access: &v2.LocalBlob{
						Type:           ocmruntime.Type{Name: v2.LocalBlobAccessType, Version: v2.LocalBlobAccessTypeVersion},
						MediaType:      layout.MediaTypeOCIImageLayoutTarGzipV1,
						LocalReference: digest.FromBytes(data).String(),
					},
				}, inmemory.New(bytes.NewReader(data)))
				r.NoError(err)
				// Create the by-value ownership referrer for the uploaded manifest.
				r.NoError(repo.AddOwnership(ctx, component, version, res, nil))
				return *res
			}
			backend := mkRes("backend", []byte("siblings-backend-payload"))
			frontend := mkRes("frontend", []byte("siblings-frontend-payload"))
			r.NoError(repo.AddComponentVersion(ctx, &descriptor.Descriptor{
				Meta: descriptor.Meta{Version: "v2"},
				Component: descriptor.Component{
					Provider:      descriptor.Provider{Name: "ocm.software"},
					ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: component, Version: version}},
					Resources:     []descriptor.Resource{backend, frontend},
				},
			}))

			backendSubject := resourceSubjectReference(t, ctx, resolver, repo, component, version, "backend")
			frontendSubject := resourceSubjectReference(t, ctx, resolver, repo, component, version, "frontend")
			r.NotEqual(backendSubject, frontendSubject, "sibling resources must have distinct subjects")

			for _, sub := range []struct {
				name    string
				subject string
			}{
				{"backend", backendSubject},
				{"frontend", frontendSubject},
			} {
				t.Run(sub.name+" carries exactly its own referrer", func(t *testing.T) {
					assertOwnershipReferrer(t, ctx, resolver, sub.subject, component, version, sub.name, true)
				})
			}
		})
	})

	t.Run("ocm transfer", func(t *testing.T) {
		// The shared component version is transferred to a fresh target registry via
		// the real CLI. The by-value resource's referrer rides inside its local-blob
		// layout, so it reaches the target with or without --copy-resources; the
		// by-reference resources only materialise on the target with --copy-resources.
		t.Run("without --copy-resources", func(t *testing.T) {
			dstResolver, dstRepo := transferTestAsset(t, ctx, srcReg, component, version, false, "")

			t.Run("backend-image-local (by-value) — referrer reaches the target", func(t *testing.T) {
				subject := resourceSubjectReference(t, ctx, dstResolver, dstRepo, component, version, "backend-image-local")
				assertOwnershipReferrer(t, ctx, dstResolver, subject, component, version, "backend-image-local", true)
			})

			// The by-reference resources still point at the source — there is no
			// target subject to assert.
		})

		t.Run("with --copy-resources", func(t *testing.T) {
			dstResolver, dstRepo := transferTestAsset(t, ctx, srcReg, component, version, true, "")

			t.Run("backend-image-local (by-value) — referrer reaches the target", func(t *testing.T) {
				subject := resourceSubjectReference(t, ctx, dstResolver, dstRepo, component, version, "backend-image-local")
				assertOwnershipReferrer(t, ctx, dstResolver, subject, component, version, "backend-image-local", true)
			})

			// The transfer copies each by-reference image together with its ownership
			// referrers, so backend-image-always gains its referrer on the target;
			// backend-image-external opted out and never had one.
			t.Run("backend-image-always (by-reference) — referrer reaches the target", func(t *testing.T) {
				subject := resourceSubjectReference(t, ctx, dstResolver, dstRepo, component, version, "backend-image-always")
				assertOwnershipReferrer(t, ctx, dstResolver, subject, component, version, "backend-image-always", true)
			})

			t.Run("backend-image-external (by-reference) — no referrer on the target", func(t *testing.T) {
				subject := resourceSubjectReference(t, ctx, dstResolver, dstRepo, component, version, "backend-image-external")
				assertOwnershipReferrer(t, ctx, dstResolver, subject, component, version, "backend-image-external", false)
			})
		})

		t.Run("with --copy-resources --upload-as ociArtifact", func(t *testing.T) {
			// --upload-as ociArtifact uploads the by-value resource as a standalone OCI
			// artifact; its digest is unchanged, so the referrer must remain
			// discoverable against the uploaded image reference.
			dstResolver, dstRepo := transferTestAsset(t, ctx, srcReg, component, version, true, "ociArtifact")

			t.Run("backend-image-local (by-value) — referrer reaches the uploaded OCI artifact on the target", func(t *testing.T) {
				subject := resourceSubjectReference(t, ctx, dstResolver, dstRepo, component, version, "backend-image-local")
				assertOwnershipReferrer(t, ctx, dstResolver, subject, component, version, "backend-image-local", true)
			})

			t.Run("backend-image-always (by-reference) — referrer reaches the target", func(t *testing.T) {
				subject := resourceSubjectReference(t, ctx, dstResolver, dstRepo, component, version, "backend-image-always")
				assertOwnershipReferrer(t, ctx, dstResolver, subject, component, version, "backend-image-always", true)
			})
		})
	})
}

// --- ocm transfer: driving the real `ocm transfer component-version` command ----

// transferTestAsset transfers the component version into a fresh target registry
// via the real `ocm transfer component-version` command and returns the target's
// resolver and repository.
func transferTestAsset(t *testing.T, ctx context.Context, srcReg *internal.OCIRegistry, component, version string, copyResources bool, uploadAs string) (*urlresolver.CachingResolver, *oci.Repository) {
	t.Helper()
	r := require.New(t)

	dstResolver, dstReg := ownershipRegistry(t)
	dstRepo := newOwnershipRepository(t, dstResolver)

	// The command needs credentials for both registries.
	cfgPath, err := internal.CreateOCMConfigForRegistry(t, []internal.ConfigOpts{
		{Host: srcReg.Host, Port: srcReg.Port, User: srcReg.User, Password: srcReg.Password},
		{Host: dstReg.Host, Port: dstReg.Port, User: dstReg.User, Password: dstReg.Password},
	})
	r.NoError(err)

	args := []string{
		"transfer", "component-version",
		fmt.Sprintf("http://%s//%s:%s", srcReg.RegistryAddress, component, version),
		fmt.Sprintf("http://%s", dstReg.RegistryAddress),
		"--config", cfgPath,
	}
	if copyResources {
		args = append(args, "--copy-resources")
	}
	if uploadAs != "" {
		args = append(args, "--upload-as", uploadAs)
	}

	transferCMD := cmd.New()
	transferOut := new(bytes.Buffer)
	transferCMD.SetOut(transferOut)
	transferCMD.SetErr(transferOut)
	transferCMD.SetArgs(args)

	transferCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	t.Cleanup(cancel)
	r.NoError(transferCMD.ExecuteContext(transferCtx), "transfer should succeed: %s", transferOut.String())
	return dstResolver, dstRepo
}

// --- ocm add cv: driving the real `ocm add component-version` command ---------

// addComponentVersionViaCLI writes constructorYAML into constructorDir (the root
// for relative file/v1 input paths) and runs the real `ocm add component-version`
// command against reg.
func addComponentVersionViaCLI(t *testing.T, ctx context.Context, reg *internal.OCIRegistry, constructorDir, constructorYAML string, extraArgs ...string) {
	t.Helper()
	r := require.New(t)

	constructorPath := filepath.Join(constructorDir, "constructor.yaml")
	r.NoError(os.WriteFile(constructorPath, []byte(constructorYAML), os.ModePerm))

	cfgPath, err := internal.CreateOCMConfigForRegistry(t, []internal.ConfigOpts{
		{Host: reg.Host, Port: reg.Port, User: reg.User, Password: reg.Password},
	})
	r.NoError(err)

	addCMD := cmd.New()
	out := new(bytes.Buffer)
	addCMD.SetOut(out)
	addCMD.SetErr(out)
	addCMD.SetArgs(append([]string{
		"add", "component-version",
		"--repository", fmt.Sprintf("http://%s", reg.RegistryAddress),
		"--constructor", constructorPath,
		"--config", cfgPath,
	}, extraArgs...))

	addCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	r.NoError(addCMD.ExecuteContext(addCtx), "add cv should succeed: %s", out.String())
}

// pushByReferenceImage uploads a one-layer OCI image to imageRef (attaching no
// referrer) and returns its reference for a constructor access to point at.
func pushByReferenceImage(t *testing.T, ctx context.Context, repo *oci.Repository, resourceName, version, imageRef string, payload []byte) string {
	t.Helper()
	r := require.New(t)
	data, access := createSingleLayerOCIImage(t, payload, imageRef)
	uploaded, err := repo.UploadResource(ctx, &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: resourceName, Version: version}},
		Type:        "ociArtifact",
		Access:      access,
	}, inmemory.New(bytes.NewReader(data)))
	r.NoError(err)
	return uploaded.Access.(*v1.OCIImage).ImageReference
}

// resourceSubjectReference returns the full OCI reference of resourceName's
// content — the subject an ownership referrer points at: the local-blob digest in
// the component repository for by-value, the access' imageReference for
// by-reference.
func resourceSubjectReference(t *testing.T, ctx context.Context, resolver *urlresolver.CachingResolver, repo *oci.Repository, component, version, resourceName string) string {
	t.Helper()
	r := require.New(t)
	desc, err := repo.GetComponentVersion(ctx, component, version)
	r.NoError(err)
	for _, res := range desc.Component.Resources {
		if res.Name != resourceName {
			continue
		}
		var img v1.OCIImage
		if err := ociaccess.Scheme.Convert(res.Access, &img); err == nil && img.ImageReference != "" {
			return img.ImageReference
		}
		var local v2.LocalBlob
		r.NoError(v2.Scheme.Convert(res.Access, &local))
		ref, err := looseref.ParseReference(resolver.ComponentVersionReference(ctx, component, version))
		r.NoError(err)
		ref.Tag = ""
		ref.Reference.Reference = local.LocalReference
		return ref.String()
	}
	t.Fatalf("resource %q not present in component version %s:%s", resourceName, component, version)
	return ""
}

// writeOCILayoutTarball writes a deterministic one-layer OCI image layout tarball
// to dir/name for use as a file/v1 input.
func writeOCILayoutTarball(t *testing.T, dir, name string, payload []byte) {
	t.Helper()
	data, _ := createSingleLayerOCIImage(t, payload)
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0o600))
}

// --- shared helpers -----------------------------------------------------------

// addOwnershipResource adds a component version with a single by-value resource
// to repo. When createReferrer is true it also records an ownership referrer for
// it via repo.AddOwnership.
func addOwnershipResource(t *testing.T, ctx context.Context, repo *oci.Repository, component, version, resourceName string, createReferrer bool) {
	t.Helper()
	r := require.New(t)

	data, _ := createSingleLayerOCIImage(t, []byte("transfer-byvalue-payload"))
	res, err := repo.AddLocalResource(ctx, component, version, &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: resourceName, Version: version}},
		Type:        "ociArtifact",
		Relation:    descriptor.LocalRelation,
		Access: &v2.LocalBlob{
			Type:           ocmruntime.Type{Name: v2.LocalBlobAccessType, Version: v2.LocalBlobAccessTypeVersion},
			MediaType:      layout.MediaTypeOCIImageLayoutTarGzipV1,
			LocalReference: digest.FromBytes(data).String(),
		},
	}, inmemory.New(bytes.NewReader(data)))
	r.NoError(err)
	if createReferrer {
		r.NoError(repo.AddOwnership(ctx, component, version, res, nil))
	}
	r.NoError(repo.AddComponentVersion(ctx, &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			Provider:      descriptor.Provider{Name: "ocm.software"},
			ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: component, Version: version}},
			Resources:     []descriptor.Resource{*res},
		},
	}))
}

// assertOwnershipReferrer asserts the ownership-referrer state of resourceName on
// the subject at subjectRef. When wantReferrer is true, exactly one referrer must
// exist, carrying the owning component/version and resource identity; otherwise
// none.
func assertOwnershipReferrer(t *testing.T, ctx context.Context, resolver *urlresolver.CachingResolver, subjectRef, component, version, resourceName string, wantReferrer bool) {
	t.Helper()
	r := require.New(t)

	referrers := listOwnershipReferrers(t, ctx, resolver, subjectRef)
	if !wantReferrer {
		r.Empty(referrers, "resource %q must not carry an ownership referrer", resourceName)
		return
	}
	r.Len(referrers, 1, "resource %q must carry exactly one ownership referrer", resourceName)
	ref := referrers[0]

	assert.Equal(t, component, ref.Annotations[annotations.OwnershipComponentName])
	assert.Equal(t, version, ref.Annotations[annotations.OwnershipComponentVersion])

	var payload struct {
		Identity map[string]string `json:"identity"`
		Kind     string            `json:"kind"`
	}
	r.NoError(json.Unmarshal([]byte(ref.Annotations[annotations.ArtifactAnnotationKey]), &payload))
	assert.Equal(t, "resource", payload.Kind)
	assert.Equal(t, resourceName, payload.Identity["name"])
	assert.Equal(t, version, payload.Identity["version"])

	// The Referrers API indexes by subject, so also assert the referrer's subject
	// digest matches the resolved subject manifest.
	store, err := resolver.StoreForReference(ctx, subjectRef)
	r.NoError(err)
	sref, err := looseref.ParseReference(subjectRef)
	r.NoError(err)
	subject, err := store.Resolve(ctx, sref.ReferenceOrTag())
	r.NoError(err)

	rc, err := store.Fetch(ctx, ref)
	r.NoError(err)
	defer func() { r.NoError(rc.Close()) }()
	var manifest ociImageSpecV1.Manifest
	r.NoError(json.NewDecoder(rc).Decode(&manifest))
	r.NotNil(manifest.Subject, "ownership referrer manifest must carry a subject")
	assert.Equal(t, subject.Digest, manifest.Subject.Digest, "referrer subject must match the target subject manifest digest")
}

// listOwnershipReferrers resolves reference and returns its referrers carrying
// [annotations.OwnershipArtifactType] via the OCI Referrers API.
func listOwnershipReferrers(t *testing.T, ctx context.Context, resolver *urlresolver.CachingResolver, reference string) []ociImageSpecV1.Descriptor {
	t.Helper()
	r := require.New(t)
	store, err := resolver.StoreForReference(ctx, reference)
	r.NoError(err)
	graphStore, ok := store.(content.ReadOnlyGraphStorage)
	r.Truef(ok, "store %T must implement content.ReadOnlyGraphStorage for referrers discovery", store)
	ref, err := looseref.ParseReference(reference)
	r.NoError(err)
	subject, err := store.Resolve(ctx, ref.ReferenceOrTag())
	r.NoError(err)
	refs, err := orasregistry.Referrers(ctx, graphStore, subject, annotations.OwnershipArtifactType)
	r.NoError(err)
	return refs
}

// newOwnershipRepository builds an oci.Repository backed by resolver with a
// per-test temp dir.
func newOwnershipRepository(t *testing.T, resolver *urlresolver.CachingResolver) *oci.Repository {
	t.Helper()
	repo, err := oci.NewRepository(
		oci.WithResolver(resolver),
		oci.WithTempDir(t.TempDir()),
	)
	require.NoError(t, err)
	return repo
}

// ownershipRegistry starts an htpasswd-protected distribution registry and returns
// a resolver pointing at it together with its connection details.
func ownershipRegistry(t *testing.T) (*urlresolver.CachingResolver, *internal.OCIRegistry) {
	t.Helper()
	r := require.New(t)

	reg, err := internal.CreateOCIRegistry(t)
	r.NoError(err)

	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(reg.RegistryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(internal.CreateAuthClient(reg.RegistryAddress, reg.User, reg.Password)),
	)
	r.NoError(err)
	return resolver, reg
}
