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

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/looseref"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	"ocm.software/open-component-model/bindings/go/oci/tar"
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
//     its local-blob layout and reaches the target with or without --copy-resources.
//     The by-reference resources only materialise with --copy-resources; when they
//     do, the transfer pulls each source image's ownership referrers (attached
//     out-of-band via the Referrers API) into the copy, so a resource that opted in
//     (Always) gains its referrer on the target while one that opted out (Never) does
//     not — both with and without --upload-as ociArtifact.
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

	ownedImageRef := pushByReferenceImage(t, ctx, srcRepo, "backend-image-always", version,
		fmt.Sprintf("%s/test-asset/backend-image-always:%s", srcAddr, version), []byte("backend-image-payload"))
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

			// The referrer is content-addressed off the subject, so adding the same
			// by-value resource twice must converge on exactly one referrer — not two.
			// Enumerating the live Referrers API (not Exists) is what proves "exactly one".
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

			// A single by-value (input) resource added twice. The on-disk tarball is
			// identical across both adds, so the resource's content — and therefore its
			// subject digest — is stable; only options.ownershipPolicy changes.
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

			// Add #1 — ownershipPolicy=Never: the resource is uploaded but opts out, so
			// its subject must carry no ownership referrer.
			addComponentVersionViaCLI(t, ctx, reg, dir, toggleYAML("Never"))
			subjectRef := resourceSubjectReference(t, ctx, resolver, repo, component, version, resourceName)
			assertOwnershipReferrer(t, ctx, resolver, subjectRef, component, version, resourceName, false)

			// Add #2 — flip the same resource to ownershipPolicy=Always and replace the
			// existing component version. The referrer must now appear on the unchanged
			// subject.
			addComponentVersionViaCLI(t, ctx, reg, dir, toggleYAML("Always"), "--component-version-conflict-policy", "replace")
			assertOwnershipReferrer(t, ctx, resolver, subjectRef, component, version, resourceName, true)
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
					assertOwnershipReferrer(t, ctx, resolver, sub.subject, component, version, sub.name, true)
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
			dstResolver, dstRepo := transferTestAsset(t, ctx, srcReg, component, version, false, "")

			t.Run("backend-image-local (by-value) — referrer reaches the target", func(t *testing.T) {
				subject := resourceSubjectReference(t, ctx, dstResolver, dstRepo, component, version, "backend-image-local")
				assertOwnershipReferrer(t, ctx, dstResolver, subject, component, version, "backend-image-local", true)
			})

			// The by-reference resources are left pointing at the source, so nothing about
			// them lands on the target — there is no target subject to assert.
		})

		t.Run("with --copy-resources", func(t *testing.T) {
			dstResolver, dstRepo := transferTestAsset(t, ctx, srcReg, component, version, true, "")

			t.Run("backend-image-local (by-value) — referrer reaches the target", func(t *testing.T) {
				subject := resourceSubjectReference(t, ctx, dstResolver, dstRepo, component, version, "backend-image-local")
				assertOwnershipReferrer(t, ctx, dstResolver, subject, component, version, "backend-image-local", true)
			})

			// Both by-reference resources are copied to the target. The transfer pulls
			// each source image's ownership referrers (attached out-of-band via the
			// Referrers API) into the copy, so backend-image-always — which opted in —
			// gains its referrer on the target; backend-image-external opted out (Never)
			// and never had one.
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
			// --upload-as ociArtifact re-uploads the by-value resource as a real,
			// standalone OCI artifact on the target instead of a component-descriptors
			// local blob. Its content (hence its manifest digest) is unchanged, so the
			// ownership referrer riding inside the layout must remain discoverable — now
			// against the uploaded OCI image reference subject.
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

		// CTF has no Referrers API and stores by-value resources as OCI image
		// layout tarballs in the component-descriptors repo. The ownership
		// referrer rides inside that layout, so it survives a CTF target transfer
		// regardless of --copy-resources. By-reference resources are not asserted
		// here: without --copy-resources they stay pointed at the source registry,
		// and with --copy-resources they get re-hosted as local blobs in the CTF
		// — the same by-value layout path the local resource exercises.
		t.Run("to CTF target — by-value referrer reaches the target layout", func(t *testing.T) {
			dstCTFPath, dstRepo := transferTestAssetToCTF(t, ctx, srcReg, component, version)
			t.Logf("destination CTF: %s", dstCTFPath)
			assertCTFLocalBlobReferrer(t, ctx, dstRepo, component, version, "backend-image-local", true)
		})
	})
}

// transferTestAssetToCTF transfers the shared test-asset component version from
// srcReg into a fresh CTF archive via the real `ocm transfer component-version`
// CLI command and returns the CTF archive path and an [oci.Repository] backed by
// it for asserting the result.
func transferTestAssetToCTF(t *testing.T, ctx context.Context, srcReg *internal.OCIRegistry, component, version string) (string, *oci.Repository) {
	t.Helper()
	r := require.New(t)

	dstCTF := filepath.Join(t.TempDir(), "dst-ctf")

	cfgPath, err := internal.CreateOCMConfigForRegistry(t, []internal.ConfigOpts{
		{Host: srcReg.Host, Port: srcReg.Port, User: srcReg.User, Password: srcReg.Password},
	})
	r.NoError(err)

	transferCMD := cmd.New()
	out := new(bytes.Buffer)
	transferCMD.SetOut(out)
	transferCMD.SetErr(out)
	transferCMD.SetArgs([]string{
		"transfer", "component-version",
		fmt.Sprintf("http://%s//%s:%s", srcReg.RegistryAddress, component, version),
		fmt.Sprintf("ctf::%s", dstCTF),
		"--config", cfgPath,
	})

	transferCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	t.Cleanup(cancel)
	r.NoError(transferCMD.ExecuteContext(transferCtx), "transfer to CTF should succeed: %s", out.String())

	fs, err := filesystem.NewFS(dstCTF, os.O_RDWR)
	r.NoError(err)
	dstRepo, err := oci.NewRepository(
		ocictf.WithCTF(ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))),
		oci.WithTempDir(t.TempDir()),
	)
	r.NoError(err)
	return dstCTF, dstRepo
}

// assertCTFLocalBlobReferrer asserts the ADR-0016 ownership referrer state of a
// by-value resource in a CTF target. The referrer rides inside the resource's
// OCI image layout tarball, so it is verified by downloading the resource (which
// materialises the local-blob layout) and inspecting the layout's index for a
// manifest declaring a subject — the ownership referrer.
//
// CTF has no Referrers API ([ctf.repository.Predecessors] returns nil), so the
// out-of-band Referrers-API verification used for OCI registries does not apply
// here; the layout-level check is what proves the referrer reached the target.
func assertCTFLocalBlobReferrer(t *testing.T, ctx context.Context, repo *oci.Repository, component, version, resourceName string, wantReferrer bool) {
	t.Helper()
	r := require.New(t)

	desc, err := repo.GetComponentVersion(ctx, component, version)
	r.NoError(err)
	var found *descriptor.Resource
	for i := range desc.Component.Resources {
		if desc.Component.Resources[i].Name == resourceName {
			found = &desc.Component.Resources[i]
			break
		}
	}
	r.NotNilf(found, "resource %q not present in component version %s:%s", resourceName, component, version)

	data, _, err := repo.GetLocalResource(ctx, component, version, found.ToIdentity())
	r.NoError(err)
	store, err := tar.ReadOCILayout(ctx, data)
	r.NoError(err)
	defer func() { r.NoError(store.Close()) }()

	referrers := store.Referrers(ctx)
	if !wantReferrer {
		r.Empty(referrers, "resource %q must not carry an ownership referrer in its CTF layout", resourceName)
		return
	}
	r.Len(referrers, 1, "resource %q must carry exactly one ownership referrer in its CTF layout", resourceName)
	ref := referrers[0]

	rc, err := store.Fetch(ctx, ref)
	r.NoError(err)
	defer func() { r.NoError(rc.Close()) }()
	var manifest ociImageSpecV1.Manifest
	r.NoError(json.NewDecoder(rc).Decode(&manifest))
	r.NotNil(manifest.Subject, "ownership referrer manifest must carry a subject")
	r.Equal(annotations.OwnershipArtifactType, manifest.ArtifactType)
	assert.Equal(t, component, manifest.Annotations[annotations.OwnershipComponentName])
	assert.Equal(t, version, manifest.Annotations[annotations.OwnershipComponentVersion])

	var payload struct {
		Identity map[string]string `json:"identity"`
		Kind     string            `json:"kind"`
	}
	r.NoError(json.Unmarshal([]byte(manifest.Annotations[annotations.ArtifactAnnotationKey]), &payload))
	assert.Equal(t, "resource", payload.Kind)
	assert.Equal(t, resourceName, payload.Identity["name"])
	assert.Equal(t, version, payload.Identity["version"])
}

// --- ocm transfer: driving the real `ocm transfer component-version` command ----

// transferTestAsset transfers the shared test-asset component version into a fresh
// target registry via the real `ocm transfer component-version` CLI command and
// returns the target's resolver and repository for asserting the result.
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
//
// The add cv half is driven end to end through the production CLI command, so the
// wired seam (GetResourceRepository -> constructorPlugin.AddOwnership) and
// the policy gate in constructor.processResource are both exercised as a user would
// hit them — no hand-wired constructor engine.

// addComponentVersionViaCLI writes constructorYAML into constructorDir — where the
// command roots relative file/v1 input paths — and runs the real
// `ocm add component-version` command against reg.
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

// assertOwnershipReferrer asserts the ADR-0016 ownership-referrer state of resourceName
// on the subject identified by subjectRef — a full OCI reference (by tag or digest)
// already resolved by the caller, be it a by-value resource (component-descriptors repo
// @ local-blob digest), a by-reference image, on the source or on a transfer target.
//
// When wantReferrer is false, no ownership referrer must be present. When true, exactly
// one must be present and it is verified: its annotations carry the owning
// component/version and resource identity, and its manifest subject digest matches the
// resolved subject manifest on this registry (the Referrers API indexes by subject, so a
// referrer with a stale or wrong subject digest would still be returned for this subject).
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
