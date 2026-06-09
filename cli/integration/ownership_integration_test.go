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

// Test_Integration_Ownership verifies ownership (ADR 0016) end-to-end against live
// containerised registries, in two nested subtests:
//
//   - "ocm add cv": runs the real `ocm add component-version` on a constructor YAML
//     with three resources that differ only in options.ownershipPolicy. The two that
//     opt in (Always) — one by-value, one by-reference — become discoverable as
//     ownership referrers; the one that does not (Never) gets none. Further subtests
//     cover idempotent re-adds and sibling isolation.
//   - "ocm transfer": transfers the same component version to a fresh registry with
//     the real `ocm transfer component-version`. The by-value referrer rides inside
//     its local-blob layout and reaches the target with or without --copy-resources;
//     the by-reference resources only materialise with --copy-resources and carry no
//     referrer, since transfer copies only referrers inside the layout, not ones
//     attached out-of-band via the Referrers API.
//
// Verification goes through the OCI Referrers API (`registry.Referrers`).
func Test_Integration_Ownership(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	const (
		version   = "v1.0.0"
		component = "ocm.software/test-asset"
	)

	// One component version is constructed once via the real `ocm add
	// component-version` command and re-used by both halves of the test. It is built
	// from a single component-constructor YAML carrying three resources that differ
	// only in how each opts into ownership (ADR 0016) via options.ownershipPolicy —
	// never via relation:
	//   - backend-image-local: a by-value file/v1 input (an OCI image layout
	//     tarball), ownershipPolicy=Always        → referrer created
	//   - backend-image: a by-reference image, ownershipPolicy=Always → referrer created
	//   - backend-image-external: a by-reference image, ownershipPolicy=Never → none
	// The two by-reference resources point at distinct images so each subject's
	// referrers can be asserted independently. The source registry also hosts the
	// images for the by-reference resources.
	srcResolver, srcReg := ownershipRegistry(t)
	srcAddr := srcReg.RegistryAddress
	srcRepo := newOwnershipRepository(t, srcResolver)

	constructorDir := t.TempDir()
	writeOCILayoutTarball(t, constructorDir, "hello-ocm.tar.gz", []byte("backend-image-local-payload"))

	ownedImageRef := pushByReferenceImage(t, ctx, srcRepo, "backend-image", version,
		fmt.Sprintf("%s/test-asset/backend-image:%s", srcAddr, version), []byte("backend-image-payload"))
	externalImageRef := pushByReferenceImage(t, ctx, srcRepo, "backend-image-external", version,
		fmt.Sprintf("%s/test-asset/backend-image-external:%s", srcAddr, version), []byte("backend-image-external-payload"))

	// The by-reference accesses carry an http:// scheme so the CLI talks plain HTTP
	// to the containerised registry.
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
      - name: backend-image
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
			r := require.New(t)
			subjectRef := resourceSubjectReference(t, ctx, srcResolver, srcRepo, component, version, "backend-image-local")
			referrers := listOwnershipReferrers(t, ctx, srcResolver, subjectRef)
			r.Len(referrers, 1, "a by-value input resource that opts in must get exactly one ownership referrer")
			assertOwnership(t, ctx, srcResolver, subjectRef, referrers[0], component, version, "backend-image-local")
		})

		t.Run("imageReference access (ownershipPolicy=Always) — ownership referrer is created", func(t *testing.T) {
			r := require.New(t)
			referrers := listOwnershipReferrers(t, ctx, srcResolver, ownedImageRef)
			r.Len(referrers, 1, "a by-reference resource that opts in must get exactly one ownership referrer on its image")
			assertOwnership(t, ctx, srcResolver, ownedImageRef, referrers[0], component, version, "backend-image")
		})

		t.Run("imageReference access (ownershipPolicy=Never) — ownership referrer is not created", func(t *testing.T) {
			r := require.New(t)
			r.Empty(listOwnershipReferrers(t, ctx, srcResolver, externalImageRef),
				"a resource that does not opt in must not get an ownership referrer")
		})

		t.Run("by-value create is idempotent (single referrer on re-add)", func(t *testing.T) {
			r := require.New(t)
			const (
				component    = "ocm.software/ownership/idempotent"
				resourceName = "backend-image"
			)
			resolver, _ := ownershipRegistry(t)
			repo := newOwnershipRepository(t, resolver)

			// The referrer is content-addressed off the subject, so adding the same
			// by-value resource twice must converge on exactly one referrer — not two.
			// Enumerating the live Referrers API (not Exists) is what proves "exactly one".
			addOwnershipResource(t, ctx, repo, component, version, resourceName, true)
			addOwnershipResource(t, ctx, repo, component, version, resourceName, true)

			subjectRef := resourceSubjectReference(t, ctx, resolver, repo, component, version, resourceName)
			referrers := listOwnershipReferrers(t, ctx, resolver, subjectRef)
			r.Len(referrers, 1, "re-adding the same by-value resource must leave exactly one ownership referrer")
			assertOwnership(t, ctx, resolver, subjectRef, referrers[0], component, version, resourceName)
		})

		t.Run("sibling resources get isolated referrers", func(t *testing.T) {
			r := require.New(t)
			const component = "ocm.software/ownership/siblings"
			resolver, _ := ownershipRegistry(t)
			repo := newOwnershipRepository(t, resolver)

			// Two by-value resources with distinct content (distinct subjects) in one
			// component version: each subject must carry exactly its own referrer, with
			// its own software.ocm.artifact identity — never the sibling's.
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
					r := require.New(t)
					referrers := listOwnershipReferrers(t, ctx, resolver, sub.subject)
					r.Len(referrers, 1, "%s must carry exactly its own referrer", sub.name)
					assertOwnership(t, ctx, resolver, sub.subject, referrers[0], component, version, sub.name)
				})
			}
		})
	})

	t.Run("ocm transfer", func(t *testing.T) {
		// The shared test-asset component version is transferred into a fresh target
		// registry with the real `ocm transfer component-version` CLI command, and each
		// of its three resources is asserted on the target:
		//   - backend-image-local: a by-value resource whose ownership referrer rides
		//     inside its local-blob layout. A component-version transfer always carries
		//     local blobs, so the referrer reaches the target whether or not resources
		//     are copied by value — --copy-resources is exercised both ways to lock that
		//     in.
		//   - backend-image / backend-image-external: by-reference resources. They only
		//     materialise on the target when --copy-resources copies them, and the
		//     by-reference upload path copies only referrers that ride inside the layout
		//     — never the ones attached out-of-band via the Referrers API. So even
		//     backend-image, which carries a referrer in the source, lands none on the
		//     target.
		t.Run("without --copy-resources", func(t *testing.T) {
			dstResolver, dstRepo := transferTestAsset(t, ctx, srcReg, component, version, false)

			t.Run("backend-image-local (by-value) — referrer reaches the target", func(t *testing.T) {
				assertLocalReferrerOnTarget(t, ctx, dstResolver, dstRepo, component, version)
			})

			// The by-reference resources are left pointing at the source, so nothing about
			// them lands on the target — there is no target subject to assert.
		})

		t.Run("with --copy-resources", func(t *testing.T) {
			dstResolver, dstRepo := transferTestAsset(t, ctx, srcReg, component, version, true)

			t.Run("backend-image-local (by-value) — referrer reaches the target", func(t *testing.T) {
				assertLocalReferrerOnTarget(t, ctx, dstResolver, dstRepo, component, version)
			})

			// Both by-reference resources are copied to the target, but neither gains an
			// ownership referrer: backend-image's source referrer was attached via the
			// Referrers API (not inside the layout) so the by-reference upload path drops
			// it, and backend-image-external never had one.
			t.Run("backend-image (by-reference) — no referrer on the target", func(t *testing.T) {
				assertNoReferrerOnTarget(t, ctx, dstResolver, dstRepo, component, version, "backend-image")
			})

			t.Run("backend-image-external (by-reference) — no referrer on the target", func(t *testing.T) {
				assertNoReferrerOnTarget(t, ctx, dstResolver, dstRepo, component, version, "backend-image-external")
			})
		})
	})
}

// --- ocm transfer: driving the real `ocm transfer component-version` command ----

// transferTestAsset transfers the shared test-asset component version into a fresh
// target registry via the real `ocm transfer component-version` CLI command and
// returns the target's resolver and repository for asserting the result.
func transferTestAsset(t *testing.T, ctx context.Context, srcReg *internal.OCIRegistry, component, version string, copyResources bool) (*urlresolver.CachingResolver, *oci.Repository) {
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

// assertLocalReferrerOnTarget asserts the by-value resource's referrer reached the
// target — it travels inside the layout, so it does regardless of --copy-resources.
func assertLocalReferrerOnTarget(t *testing.T, ctx context.Context, dstResolver *urlresolver.CachingResolver, dstRepo *oci.Repository, component, version string) {
	t.Helper()
	r := require.New(t)
	subject := resourceSubjectReference(t, ctx, dstResolver, dstRepo, component, version, "backend-image-local")
	referrers := listOwnershipReferrers(t, ctx, dstResolver, subject)
	r.Len(referrers, 1, "the by-value resource's ownership referrer must reach the target")
	assertOwnership(t, ctx, dstResolver, subject, referrers[0], component, version, "backend-image-local")
}

// assertNoReferrerOnTarget asserts a by-reference resource copied to the target gained
// no ownership referrer.
func assertNoReferrerOnTarget(t *testing.T, ctx context.Context, dstResolver *urlresolver.CachingResolver, dstRepo *oci.Repository, component, version, resourceName string) {
	t.Helper()
	r := require.New(t)
	subject := resourceSubjectReference(t, ctx, dstResolver, dstRepo, component, version, resourceName)
	r.Empty(listOwnershipReferrers(t, ctx, dstResolver, subject),
		"by-reference resource %q must not gain an ownership referrer on transfer", resourceName)
}

// --- ocm add cv: driving the real `ocm add component-version` command ---------
//
// The add cv half is driven end to end through the production CLI command, so the
// wired seam (GetResourceRepository -> constructorPlugin.AddOwnership) and
// the policy gate in constructor.processResource are both exercised as a user would
// hit them — no hand-wired constructor engine.

// addComponentVersionViaCLI writes constructorYAML into constructorDir — where the
// command roots relative file/v1 input paths — and runs the real
// `ocm add component-version` command against reg.
func addComponentVersionViaCLI(t *testing.T, ctx context.Context, reg *internal.OCIRegistry, constructorDir, constructorYAML string) {
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
	addCMD.SetArgs([]string{
		"add", "component-version",
		"--repository", fmt.Sprintf("http://%s", reg.RegistryAddress),
		"--constructor", constructorPath,
		"--config", cfgPath,
	})

	addCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	r.NoError(addCMD.ExecuteContext(addCtx), "add cv should succeed: %s", out.String())
}

// pushByReferenceImage uploads a one-layer OCI image to imageRef as a by-reference
// resource carrying no ownership policy (so the push itself attaches no referrer)
// and returns the reference of the uploaded image for a constructor access to point
// at. add cv attaches a referrer to an existing image; it never pushes one.
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

// resourceSubjectReference reads the component version back and returns the full OCI
// reference of resourceName's content — the subject an ownership referrer points at.
// For a by-value resource that is the component-descriptors repo @ local-blob digest;
// for a by-reference resource it is the access' imageReference.
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
// (an application/vnd.ocm.software.oci.layout.v1+tar+gzip blob) to dir/name — the
// on-disk artifact a file/v1 input feeds into a by-value resource.
func writeOCILayoutTarball(t *testing.T, dir, name string, payload []byte) {
	t.Helper()
	data, _ := createSingleLayerOCIImage(t, payload)
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0o600))
}

// --- shared helpers -----------------------------------------------------------

// addOwnershipResource authors a single by-value resource (an OCI image layout
// local blob) on repo and adds a component version holding it. When createReferrer
// is true it also pushes one ownership referrer next to the uploaded manifest (via
// repo.AddOwnership), so the resource becomes a transfer source that carries the
// referrer.
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

// assertOwnership checks an ADR-0016 ownership referrer: its annotations and that
// its subject points at the subject manifest as it exists on this registry.
// subjectRef is the full OCI reference of the owned artifact (by tag or digest).
func assertOwnership(t *testing.T, ctx context.Context, resolver *urlresolver.CachingResolver, subjectRef string, ref ociImageSpecV1.Descriptor, component, version, resourceName string) {
	t.Helper()
	r := require.New(t)

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

	// The Referrers API indexes by subject, so a referrer with a stale or wrong
	// subject digest would still be returned for this subject — assert the
	// referrer manifest's subject actually matches the resolved subject digest.
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

// listOwnershipReferrers walks the OCI Referrers API for the subject identified by
// reference — a full OCI reference, by tag or by digest — and returns every referrer
// carrying [annotations.OwnershipArtifactType]. It serves both a by-value subject
// (component-descriptors repo @ local-blob digest) and a by-reference OCI image
// (its access' ImageReference).
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
// a resolver pointing at it together with its connection details. The container is
// torn down on test cleanup.
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
