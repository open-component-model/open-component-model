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
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/cmd"
	"ocm.software/open-component-model/cli/integration/internal"
)

const ownershipVersion = "v1.0.0"

func Test_Integration_Ownership_AddCV_ByValue(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	const (
		component    = "ocm.software/ownership/add-cv/by-value"
		resourceName = "backend-image-local"
	)
	resolver, reg := ownershipRegistry(t)
	repo := newOwnershipRepository(t, resolver)

	dir := t.TempDir()
	writeOCILayoutTarball(t, dir, "hello-ocm.tar.gz", []byte("add-cv-by-value-payload"))
	addComponentVersionViaCLI(t, ctx, reg, dir, fmt.Sprintf(`
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
          ownershipPolicy: Always
        input:
          type: file/v1
          path: hello-ocm.tar.gz
          mediaType: application/vnd.ocm.software.oci.layout.v1+tar+gzip
`, component, ownershipVersion, resourceName))

	subject := localBlobSubjectReference(t, ctx, resolver, repo, component, ownershipVersion, runtime.Identity{"name": resourceName, "version": ownershipVersion})
	assertOwnershipReferrer(t, ctx, resolver, subject, component, ownershipVersion, resourceName, true)
}

func Test_Integration_Ownership_AddCV_ByReference(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	const (
		component    = "ocm.software/ownership/add-cv/by-reference"
		resourceName = "backend-image-always"
	)
	resolver, reg := ownershipRegistry(t)
	repo := newOwnershipRepository(t, resolver)

	ownedImageRef := pushByReferenceImage(t, ctx, repo, resourceName, ownershipVersion,
		fmt.Sprintf("%s/test-asset/backend-image-always:%s", reg.RegistryAddress, ownershipVersion),
		[]byte("add-cv-by-reference-payload"))

	addComponentVersionViaCLI(t, ctx, reg, t.TempDir(), fmt.Sprintf(`
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
          ownershipPolicy: Always
        access:
          type: OCIImage/v1
          imageReference: http://%[4]s
`, component, ownershipVersion, resourceName, ownedImageRef))

	assertOwnershipReferrer(t, ctx, resolver, ownedImageRef, component, ownershipVersion, resourceName, true)
}

func Test_Integration_Ownership_AddCV_ReplacePolicyToggle(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	const (
		component    = "ocm.software/ownership/add-cv/policy-toggle"
		resourceName = "backend-image-local"
	)
	resolver, reg := ownershipRegistry(t)
	repo := newOwnershipRepository(t, resolver)

	dir := t.TempDir()
	writeOCILayoutTarball(t, dir, "hello-ocm.tar.gz", []byte("policy-toggle-payload"))

	addComponentVersionViaCLI(t, ctx, reg, dir, fmt.Sprintf(`
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
          ownershipPolicy: Never
        input:
          type: file/v1
          path: hello-ocm.tar.gz
          mediaType: application/vnd.ocm.software.oci.layout.v1+tar+gzip
`, component, ownershipVersion, resourceName))
	subject := localBlobSubjectReference(t, ctx, resolver, repo, component, ownershipVersion, runtime.Identity{"name": resourceName, "version": ownershipVersion})
	assertOwnershipReferrer(t, ctx, resolver, subject, component, ownershipVersion, resourceName, false)

	addComponentVersionViaCLI(t, ctx, reg, dir, fmt.Sprintf(`
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
          ownershipPolicy: Always
        input:
          type: file/v1
          path: hello-ocm.tar.gz
          mediaType: application/vnd.ocm.software.oci.layout.v1+tar+gzip
`, component, ownershipVersion, resourceName),
		"--component-version-conflict-policy", "replace")
	assertOwnershipReferrer(t, ctx, resolver, subject, component, ownershipVersion, resourceName, true)
}

func Test_Integration_Ownership_AddCV_ReplaceDoesNotRemoveExistingReferrer(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	const (
		component    = "ocm.software/ownership/add-cv/policy-toggle-reverse"
		resourceName = "backend-image-local"
	)
	resolver, reg := ownershipRegistry(t)
	repo := newOwnershipRepository(t, resolver)

	dir := t.TempDir()
	writeOCILayoutTarball(t, dir, "hello-ocm.tar.gz", []byte("policy-toggle-reverse-payload"))

	addComponentVersionViaCLI(t, ctx, reg, dir, fmt.Sprintf(`
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
          ownershipPolicy: Always
        input:
          type: file/v1
          path: hello-ocm.tar.gz
          mediaType: application/vnd.ocm.software.oci.layout.v1+tar+gzip
`, component, ownershipVersion, resourceName))
	subject := localBlobSubjectReference(t, ctx, resolver, repo, component, ownershipVersion, runtime.Identity{"name": resourceName, "version": ownershipVersion})
	assertOwnershipReferrer(t, ctx, resolver, subject, component, ownershipVersion, resourceName, true)

	addComponentVersionViaCLI(t, ctx, reg, dir, fmt.Sprintf(`
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
          ownershipPolicy: Never
        input:
          type: file/v1
          path: hello-ocm.tar.gz
          mediaType: application/vnd.ocm.software.oci.layout.v1+tar+gzip
`, component, ownershipVersion, resourceName),
		"--component-version-conflict-policy", "replace")
	assertOwnershipReferrer(t, ctx, resolver, subject, component, ownershipVersion, resourceName, true)
}

func Test_Integration_Ownership_Transfer_ByValue(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	const (
		component    = "ocm.software/ownership/transfer/by-value"
		resourceName = "backend-image-local"
	)
	srcResolver, srcReg := ownershipRegistry(t)
	_ = srcResolver // only needed to satisfy ownershipRegistry's signature; the dst resolver drives the assertion below.

	srcDir := t.TempDir()
	writeOCILayoutTarball(t, srcDir, "hello-ocm.tar.gz", []byte("transfer-by-value-payload"))
	addComponentVersionViaCLI(t, ctx, srcReg, srcDir, fmt.Sprintf(`
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
          ownershipPolicy: Always
        input:
          type: file/v1
          path: hello-ocm.tar.gz
          mediaType: application/vnd.ocm.software.oci.layout.v1+tar+gzip
`, component, ownershipVersion, resourceName))

	dstResolver, dstRepo := transferComponentVersion(t, ctx, srcReg, component, ownershipVersion, true, "localBlob")
	subject := localBlobSubjectReference(t, ctx, dstResolver, dstRepo, component, ownershipVersion, runtime.Identity{"name": resourceName, "version": ownershipVersion})
	assertOwnershipReferrer(t, ctx, dstResolver, subject, component, ownershipVersion, resourceName, true)
}

func Test_Integration_Ownership_Transfer_ByReference(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	const (
		component    = "ocm.software/ownership/transfer/by-reference"
		resourceName = "backend-image-always"
	)
	srcResolver, srcReg := ownershipRegistry(t)
	srcRepo := newOwnershipRepository(t, srcResolver)

	ownedImageRef := pushByReferenceImage(t, ctx, srcRepo, resourceName, ownershipVersion,
		fmt.Sprintf("%s/test-asset/backend-image-always:%s", srcReg.RegistryAddress, ownershipVersion),
		[]byte("transfer-by-reference-payload"))
	addComponentVersionViaCLI(t, ctx, srcReg, t.TempDir(), fmt.Sprintf(`
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
          ownershipPolicy: Always
        access:
          type: OCIImage/v1
          imageReference: http://%[4]s
`, component, ownershipVersion, resourceName, ownedImageRef))

	dstResolver, dstRepo := transferComponentVersion(t, ctx, srcReg, component, ownershipVersion, true, "ociArtifact")
	subject := ociImageReference(t, ctx, dstRepo, component, ownershipVersion, runtime.Identity{"name": resourceName, "version": ownershipVersion})
	assertOwnershipReferrer(t, ctx, dstResolver, subject, component, ownershipVersion, resourceName, true)
}

func transferComponentVersion(t *testing.T, ctx context.Context, srcReg *internal.OCIRegistry, component, version string, copyResources bool, uploadAs string) (*urlresolver.CachingResolver, *oci.Repository) {
	t.Helper()
	r := require.New(t)

	dstResolver, dstReg := ownershipRegistry(t)
	dstRepo := newOwnershipRepository(t, dstResolver)

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

// pushByReferenceImage uploads a one-layer OCI image to imageRef (no ownership
// referrer attached) and returns the resolved image reference so a constructor
// access can point at it.
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
// reference of the resource matching identity — the subject an ownership referrer points at.
// For a by-value resource that is the component-descriptors repo @ local-blob digest;
// for a by-reference resource it is the access' imageReference.
// localBlobSubjectReference reads the component version back, locates the resource
// matching identity, and returns the registry@digest reference its LocalBlob access
// points at. Use this only when the resource's access is expected to be LocalBlob;
// for OCIImage access the caller already has the reference (it's access.imageReference).
func localBlobSubjectReference(t *testing.T, ctx context.Context, resolver oci.Resolver, repo *oci.Repository, component, version string, identity runtime.Identity) string {
	t.Helper()
	r := require.New(t)
	desc, err := repo.GetComponentVersion(ctx, component, version)
	r.NoError(err)

	for _, res := range desc.Component.Resources {
		if !identity.Match(res.ToIdentity(), runtime.IdentityMatchingChainFn(runtime.IdentitySubset)) {
			continue
		}
		local := &v2.LocalBlob{}
		r.NoError(v2.Scheme.Convert(res.Access, local),
			"resource %q access %q is not a LocalBlob — use access.imageReference directly", identity, res.Access.GetType())
		ref, err := looseref.ParseReference(resolver.ComponentVersionReference(ctx, component, version))
		r.NoError(err)
		ref.Tag = ""
		ref.Reference.Reference = local.LocalReference
		return ref.String()
	}
	t.Fatalf("resource matching identity %q not present in component version %s:%s", identity, component, version)
	return ""
}

// ociImageReference reads the component version back, locates the resource matching
// identity, and returns its OCIImage access's imageReference. Use when access is
// expected to be OCIImage (e.g. after `transfer --upload-as ociArtifact`).
func ociImageReference(t *testing.T, ctx context.Context, repo *oci.Repository, component, version string, identity runtime.Identity) string {
	t.Helper()
	r := require.New(t)
	desc, err := repo.GetComponentVersion(ctx, component, version)
	r.NoError(err)

	for _, res := range desc.Component.Resources {
		if !identity.Match(res.ToIdentity(), runtime.IdentityMatchingChainFn(runtime.IdentitySubset)) {
			continue
		}
		img := &v1.OCIImage{}
		r.NoError(ociaccess.Scheme.Convert(res.Access, img),
			"resource %q access %q is not an OCIImage", identity, res.Access.GetType())
		return img.ImageReference
	}
	t.Fatalf("resource matching identity %q not present in component version %s:%s", identity, component, version)
	return ""
}

// writeOCILayoutTarball writes a deterministic one-layer OCI image layout tarball
// to dir/name — the on-disk artifact a file/v1 input feeds into a by-value resource.
func writeOCILayoutTarball(t *testing.T, dir, name string, payload []byte) {
	t.Helper()
	data, _ := createSingleLayerOCIImage(t, payload)
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0o600))
}

// assertOwnershipReferrer asserts the ADR-0016 ownership-referrer state of resourceName
// on subjectRef. When wantReferrer is false, no ownership referrer must be present.
// When true, exactly one must be present and is verified end-to-end: its annotations
// carry the owning component/version and resource identity, and its manifest subject
// digest matches the resolved subject manifest on this registry.
func assertOwnershipReferrer(t *testing.T, ctx context.Context, resolver oci.Resolver, subjectRef, component, version, resourceName string, wantReferrer bool) {
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
// reference and returns every referrer carrying [annotations.OwnershipArtifactType].
func listOwnershipReferrers(t *testing.T, ctx context.Context, resolver oci.Resolver, reference string) []ociImageSpecV1.Descriptor {
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
