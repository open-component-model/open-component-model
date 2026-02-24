package component_version_test

import (
	"bytes"
	"crypto"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/signing"
	"ocm.software/open-component-model/cli/cmd/internal/test"
	"ocm.software/open-component-model/cli/internal/reference/compref"
)

// setupTestRepositoryWithDescriptorLibrary creates a test repository with the given component versions
func setupTestRepositoryWithDescriptorLibrary(t *testing.T, versions ...*descriptor.Descriptor) (string, error) {
	r := require.New(t)
	archivePath := t.TempDir()
	fs, err := filesystem.NewFS(archivePath, os.O_RDWR)
	r.NoError(err, "could not create test filesystem")
	archive := ctf.NewFileSystemCTF(fs)
	helperRepo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(archive)))
	r.NoError(err, "could not create helper test repository")

	ctx := t.Context()
	for _, desc := range versions {
		r.NoError(helperRepo.AddComponentVersion(ctx, desc), "could not add component version to test repository")
	}

	return archivePath, nil
}

// createTestDescriptor creates a test component descriptor with the given name and version
func createTestDescriptor(name, version string) *descriptor.Descriptor {
	return &descriptor.Descriptor{
		Meta: descriptor.Meta{
			Version: "v2",
		},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    name,
					Version: version,
				},
			},
			Provider: descriptor.Provider{
				Name: "ocm.software",
			},
			Resources: []descriptor.Resource{},
		},
	}
}

// addReference adds a reference to another component to a descriptor
func addReference(t *testing.T, parent, child *descriptor.Descriptor, refName string) {
	t.Helper()
	dig, err := signing.GenerateDigest(t.Context(), child, slog.Default(), v4alpha1.Algorithm, crypto.SHA256.String())
	require.NoError(t, err)

	parent.Component.References = append(parent.Component.References, descriptor.Reference{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    refName,
				Version: child.Component.Version,
			},
		},
		Component: child.Component.Name,
		Digest:    *dig,
	})
}

func TestTransferComponentVersion(t *testing.T) {
	fromDesc := createTestDescriptor("ocm.software/test-component", "0.0.1")
	fromPath, err := setupTestRepositoryWithDescriptorLibrary(t, fromDesc)
	require.NoError(t, err)

	toPath := t.TempDir()

	fromRef := compref.Ref{
		Repository: &ctfv1.Repository{
			FilePath: fromPath,
		},
		Component: fromDesc.Component.Name,
		Version:   fromDesc.Component.Version,
	}

	// First transfer
	logs := test.NewJSONLogReader()
	result := new(bytes.Buffer)

	// We need to format the target correctly as a repository spec json/yaml or use the ctf:: prefix if supported by the cli parser for just a path
	// Looking at compref.ParseRepository, it supports ctf::<path>
	targetArg := fmt.Sprintf("ctf::%s", toPath)

	_, err = test.OCM(t, test.WithArgs("transfer", "component-version", fromRef.String(), targetArg), test.WithOutput(result), test.WithErrorOutput(logs))
	require.NoError(t, err)

	// Verify existence in target
	fs, err := filesystem.NewFS(toPath, os.O_RDWR)
	require.NoError(t, err)
	archive := ctf.NewFileSystemCTF(fs)
	targetRepo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(archive)))
	require.NoError(t, err)

	ctx := t.Context()
	desc, err := targetRepo.GetComponentVersion(ctx, fromDesc.Component.Name, fromDesc.Component.Version)
	require.NoError(t, err)
	require.NotNil(t, desc)
	require.Equal(t, fromDesc.Component.Name, desc.Component.Name)
	require.Equal(t, fromDesc.Component.Version, desc.Component.Version)

	logEntries, err := logs.List()
	require.NoError(t, err)

	// Check for specific log message "transfer completed successfully"
	found := false
	for _, e := range logEntries {
		if strings.Contains(fmt.Sprint(e), "transfer completed successfully") {
			found = true
			break
		}
	}
	require.True(t, found, "expected success log message")
}

func TestTransferComponentVersionRecursive(t *testing.T) {
	childDesc := createTestDescriptor("ocm.software/child-component", "0.0.1")
	parentDesc := createTestDescriptor("ocm.software/parent-component", "1.0.0")
	addReference(t, parentDesc, childDesc, "child")

	fromPath, err := setupTestRepositoryWithDescriptorLibrary(t, childDesc, parentDesc)
	require.NoError(t, err)

	toPath := t.TempDir()

	fromRef := compref.Ref{
		Repository: &ctfv1.Repository{
			FilePath: fromPath,
		},
		Component: parentDesc.Component.Name,
		Version:   parentDesc.Component.Version,
	}

	logs := test.NewJSONLogReader()
	result := new(bytes.Buffer)

	targetArg := fmt.Sprintf("ctf::%s", toPath)

	_, err = test.OCM(t, test.WithArgs("transfer", "component-version", fromRef.String(), targetArg, "--recursive"), test.WithOutput(result), test.WithErrorOutput(logs))
	require.NoError(t, err)

	// Verify existence of BOTH in target
	fs, err := filesystem.NewFS(toPath, os.O_RDWR)
	require.NoError(t, err)
	archive := ctf.NewFileSystemCTF(fs)
	targetRepo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(archive)))
	require.NoError(t, err)

	ctx := t.Context()

	// Check parent
	pDesc, err := targetRepo.GetComponentVersion(ctx, parentDesc.Component.Name, parentDesc.Component.Version)
	require.NoError(t, err)
	require.NotNil(t, pDesc)

	// Check child
	cDesc, err := targetRepo.GetComponentVersion(ctx, childDesc.Component.Name, childDesc.Component.Version)
	require.NoError(t, err)
	require.NotNil(t, cDesc)

	logEntries, err := logs.List()
	require.NoError(t, err)

	// Check for success log
	found := false
	for _, e := range logEntries {
		if strings.Contains(fmt.Sprint(e), "transfer completed successfully") {
			found = true
			break
		}
	}
	require.True(t, found, "expected success log message")
}

// TestTransferComponentVersionPreservesSignatures verifies that signatures on a component
// descriptor are preserved when transferring a component version that has local blob resources.
func TestTransferComponentVersionPreservesSignatures(t *testing.T) {
	r := require.New(t)

	// Create a descriptor with a local blob resource
	fromDesc := createTestDescriptor("ocm.software/signed-component", "1.0.0")
	fromDesc.Component.Resources = []descriptor.Resource{
		{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-blob",
					Version: "1.0.0",
				},
			},
			Type:     "plainText",
			Relation: descriptor.LocalRelation,
			Access: &v2.LocalBlob{
				MediaType: "text/plain",
			},
		},
	}

	// Sign the descriptor to add signatures
	dig, err := signing.GenerateDigest(t.Context(), fromDesc, slog.Default(), v4alpha1.Algorithm, crypto.SHA256.String())
	r.NoError(err, "should be able to generate digest")
	fromDesc.Signatures = []descriptor.Signature{
		{
			Name:   "test-signature",
			Digest: *dig,
			Signature: descriptor.SignatureInfo{
				Algorithm: "RSASSA-PSS",
				Value:     "dGVzdC1zaWduYXR1cmUtdmFsdWU=",
				MediaType: "application/vnd.ocm.signature.rsa",
			},
		},
	}

	// Setup source CTF with the signed component
	archivePath := t.TempDir()
	fs, err := filesystem.NewFS(archivePath, os.O_RDWR)
	r.NoError(err)
	archive := ctf.NewFileSystemCTF(fs)
	sourceRepo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(archive)))
	r.NoError(err)

	ctx := t.Context()

	// Add the local blob resource data to the repository
	blobData := []byte("Hello, signed world!")
	updatedRes, err := sourceRepo.AddLocalResource(
		ctx,
		fromDesc.Component.Name,
		fromDesc.Component.Version,
		&fromDesc.Component.Resources[0],
		inmemory.New(bytes.NewReader(blobData)),
	)
	r.NoError(err, "should be able to add local resource")
	fromDesc.Component.Resources[0] = *updatedRes

	// Add the component version to the source CTF
	r.NoError(sourceRepo.AddComponentVersion(ctx, fromDesc), "should be able to add signed component version")

	// Verify source has signatures
	srcDesc, err := sourceRepo.GetComponentVersion(ctx, fromDesc.Component.Name, fromDesc.Component.Version)
	r.NoError(err)
	r.NotEmpty(srcDesc.Signatures, "source descriptor should have signatures")

	// Transfer to target CTF with --copy-resources
	toPath := t.TempDir()
	fromRef := compref.Ref{
		Repository: &ctfv1.Repository{
			FilePath: archivePath,
		},
		Component: fromDesc.Component.Name,
		Version:   fromDesc.Component.Version,
	}
	targetArg := fmt.Sprintf("ctf::%s", toPath)

	logs := test.NewJSONLogReader()
	result := new(bytes.Buffer)
	_, err = test.OCM(t,
		test.WithArgs("transfer", "component-version", fromRef.String(), targetArg, "--copy-resources"),
		test.WithOutput(result),
		test.WithErrorOutput(logs),
	)
	r.NoError(err, "transfer should succeed")

	// Verify target has signatures
	targetFS, err := filesystem.NewFS(toPath, os.O_RDWR)
	r.NoError(err)
	targetArchive := ctf.NewFileSystemCTF(targetFS)
	targetRepo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(targetArchive)))
	r.NoError(err)

	targetDesc, err := targetRepo.GetComponentVersion(ctx, fromDesc.Component.Name, fromDesc.Component.Version)
	r.NoError(err, "should be able to retrieve transferred component")
	r.Equal(fromDesc.Component.Name, targetDesc.Component.Name)
	r.Equal(fromDesc.Component.Version, targetDesc.Component.Version)

	// Verify signatures were preserved
	r.Len(targetDesc.Signatures, 1, "transferred descriptor should have 1 signature")
	r.Equal("test-signature", targetDesc.Signatures[0].Name)
	r.Equal(fromDesc.Signatures[0].Digest.HashAlgorithm, targetDesc.Signatures[0].Digest.HashAlgorithm)
	r.Equal(fromDesc.Signatures[0].Digest.Value, targetDesc.Signatures[0].Digest.Value)
	r.Equal("RSASSA-PSS", targetDesc.Signatures[0].Signature.Algorithm)
	r.Equal("dGVzdC1zaWduYXR1cmUtdmFsdWU=", targetDesc.Signatures[0].Signature.Value)

	// Verify resource was also transferred
	r.Len(targetDesc.Component.Resources, 1, "transferred descriptor should have 1 resource")
	r.Equal("test-blob", targetDesc.Component.Resources[0].Name)
}
