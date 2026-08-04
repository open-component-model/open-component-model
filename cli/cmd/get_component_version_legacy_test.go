package cmd_test

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	componentConfig "ocm.software/open-component-model/bindings/go/oci/spec/config/component"
	ocidescriptor "ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/cmd/internal/test"
)

// Media types written by pre-OCM Gardener (cnudie) tooling. They are spelled out
// here instead of referencing the constants under test so that the fixture pins
// the wire format a real cnudie repository has on disk.
const (
	cnudieConfigMediaType     = "application/vnd.gardener.cloud.cnudie.component.config.v1+json"
	cnudieConfig2MediaType    = "application/vnd.oci.gardener.cloud.cnudie.component-descriptor-metadata.config.v2+json"
	cnudieDescriptorMediaType = "application/vnd.gardener.cloud.cnudie.component-descriptor.v2+yaml+tar"
)

// Test_Get_Component_Version_Legacy_Cnudie lists component versions published by
// pre-OCM Gardener (cnudie) tooling. Their manifests carry no
// software.ocm.componentversion annotation, so the config media type is the only
// marker identifying them as component versions. Before the fix every tag was
// skipped and `ocm get cv` failed with "no roots provided and no roots found in
// the dag", a regression against OCM v1 which lists such repositories fine.
//
// Regression test for https://github.com/open-component-model/open-component-model/issues/2889.
func Test_Get_Component_Version_Legacy_Cnudie(t *testing.T) {
	const component = "ocm.software/legacy-component"

	for _, tt := range []struct {
		name            string
		configMediaType string
	}{
		{name: "cnudie component config v1", configMediaType: cnudieConfigMediaType},
		{name: "cnudie component descriptor metadata config v2", configMediaType: cnudieConfig2MediaType},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			archivePath := t.TempDir()
			for _, version := range []string{"0.0.1", "0.0.2"} {
				addLegacyCnudieComponentVersion(t, archivePath, createTestDescriptor(component, version), tt.configMediaType)
			}

			ref := compref.Ref{
				Repository: &ctfv1.Repository{FilePath: archivePath},
				Component:  component,
			}

			result := new(bytes.Buffer)
			_, err := test.OCM(t,
				test.WithArgs("get", "cv", ref.String(), "--output=json"),
				test.WithOutput(result),
				test.WithErrorOutput(test.NewJSONLogReader()),
			)
			r.NoError(err, "listing legacy cnudie component versions failed")

			var listed []struct {
				Component struct {
					Name    string `json:"name"`
					Version string `json:"version"`
				} `json:"component"`
			}
			r.NoError(json.NewDecoder(result).Decode(&listed), "failed to decode result JSON")

			versions := make([]string, 0, len(listed))
			for _, entry := range listed {
				r.Equal(component, entry.Component.Name)
				versions = append(versions, entry.Component.Version)
			}
			r.Equal([]string{"0.0.2", "0.0.1"}, versions, "expected both legacy component versions, newest first")
		})
	}
}

// addLegacyCnudieComponentVersion stores desc in the CTF at archivePath the way
// pre-OCM Gardener (cnudie) tooling did: an OCI image manifest without any
// annotations, pointing at a config of the given legacy media type and a
// descriptor layer stored as a YAML tar.
func addLegacyCnudieComponentVersion(t *testing.T, archivePath string, desc *descriptor.Descriptor, configMediaType string) {
	t.Helper()
	r := require.New(t)
	ctx := t.Context()

	fs, err := filesystem.NewFS(archivePath, os.O_RDWR)
	r.NoError(err, "could not create test filesystem")
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))

	reference := store.ComponentVersionReference(ctx, desc.Component.Name, desc.Component.Version)
	repository, err := store.StoreForReference(ctx, reference)
	r.NoError(err, "could not create store for reference %q", reference)

	descriptorBuffer, err := ocidescriptor.SingleFileEncodeDescriptor(runtime.NewScheme(), desc, cnudieDescriptorMediaType)
	r.NoError(err, "could not encode descriptor")
	descriptorLayer := content.NewDescriptorFromBytes(cnudieDescriptorMediaType, descriptorBuffer.Bytes())
	r.NoError(repository.Push(ctx, descriptorLayer, bytes.NewReader(descriptorBuffer.Bytes())))

	configRaw, err := json.Marshal(componentConfig.Config{ComponentDescriptorLayer: &descriptorLayer})
	r.NoError(err, "could not marshal component config")
	config := content.NewDescriptorFromBytes(configMediaType, configRaw)
	r.NoError(repository.Push(ctx, config, bytes.NewReader(configRaw)))

	// Deliberately without annotations: cnudie predates software.ocm.componentversion.
	manifestRaw, err := json.Marshal(ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config:    config,
		Layers:    []ociImageSpecV1.Descriptor{descriptorLayer},
	})
	r.NoError(err, "could not marshal manifest")
	manifest := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageManifest, manifestRaw)
	r.NoError(repository.Push(ctx, manifest, bytes.NewReader(manifestRaw)))
	r.NoError(repository.Tag(ctx, manifest, reference))
}
