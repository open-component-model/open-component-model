package examples

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	access "ocm.software/open-component-model/bindings/go/oci/spec/access"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// newTestScheme creates a runtime scheme with the OCI access types and v2
// descriptor types registered, as required by the OCI repository.
func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	access.MustAddToScheme(s)
	v2.MustAddToScheme(s)
	return s
}

// newTestRepo creates a CTF-backed OCI repository using a temporary directory.
// This pattern avoids external dependencies and is suitable for unit tests.
func newTestRepo(t *testing.T) *oci.Repository {
	t.Helper()
	r := require.New(t)

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo, err := oci.NewRepository(ocictf.WithCTF(store), oci.WithTempDir(t.TempDir()))
	r.NoError(err)
	return repo
}

// TestExample_CreateCTFRepository demonstrates creating a CTF-backed OCI
// repository backed by a temporary filesystem.
func TestExample_CreateCTFRepository(t *testing.T) {
	r := require.New(t)

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)

	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo, err := oci.NewRepository(ocictf.WithCTF(store), oci.WithTempDir(t.TempDir()))
	r.NoError(err)
	r.NotNil(repo)
}

// TestExample_AddAndGetComponentVersion shows how to store a component version
// in a repository and retrieve it.
func TestExample_AddAndGetComponentVersion(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()
	repo := newTestRepo(t)

	// Build a minimal component descriptor.
	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			Provider: descriptor.Provider{Name: "acme.org"},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "acme.org/my-app",
					Version: "1.0.0",
				},
			},
		},
	}

	// Verify the component does not exist yet.
	_, err := repo.GetComponentVersion(ctx, "acme.org/my-app", "1.0.0")
	r.ErrorIs(err, repository.ErrNotFound)

	// Add and retrieve the component version.
	r.NoError(repo.AddComponentVersion(ctx, desc))

	got, err := repo.GetComponentVersion(ctx, "acme.org/my-app", "1.0.0")
	r.NoError(err)
	r.Equal("acme.org/my-app", got.Component.Name)
	r.Equal("1.0.0", got.Component.Version)
}

// TestExample_ListComponentVersions demonstrates listing all versions of a
// component in a repository.
func TestExample_ListComponentVersions(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()
	repo := newTestRepo(t)

	component := "acme.org/versioned-app"
	versions := []string{"1.0.0", "1.1.0", "2.0.0"}

	for _, ver := range versions {
		desc := &descriptor.Descriptor{
			Meta: descriptor.Meta{Version: "v2"},
			Component: descriptor.Component{
				Provider: descriptor.Provider{Name: "acme.org"},
				ComponentMeta: descriptor.ComponentMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    component,
						Version: ver,
					},
				},
			},
		}
		r.NoError(repo.AddComponentVersion(ctx, desc))
	}

	listed, err := repo.ListComponentVersions(ctx, component)
	r.NoError(err)
	r.Len(listed, 3)
}

// TestExample_AddAndGetLocalResource demonstrates adding a blob resource to a
// component version and retrieving it by identity.
func TestExample_AddAndGetLocalResource(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()
	repo := newTestRepo(t)

	component := "acme.org/resource-app"
	version := "1.0.0"
	content := []byte("resource payload")

	// Create the component version first.
	res := &descriptor.Resource{
		Relation: descriptor.LocalRelation,
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "my-resource",
				Version: "1.0.0",
			},
		},
		Type: "plainText",
		Access: &v2.LocalBlob{
			LocalReference: digest.FromBytes(content).String(),
			MediaType:      "text/plain",
		},
	}

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			Provider: descriptor.Provider{Name: "acme.org"},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: component, Version: version},
			},
			Resources: []descriptor.Resource{*res},
		},
	}

	// Upload the resource blob.
	b := inmemory.New(bytes.NewReader(content))
	newRes, err := repo.AddLocalResource(ctx, component, version, res, b)
	r.NoError(err)
	desc.Component.Resources[0] = *newRes

	// Store the component version.
	r.NoError(repo.AddComponentVersion(ctx, desc))

	// Retrieve the resource by identity.
	readBlob, _, err := repo.GetLocalResource(ctx, component, version, map[string]string{
		"name":    "my-resource",
		"version": "1.0.0",
	})
	r.NoError(err)

	var buf bytes.Buffer
	r.NoError(blob.Copy(&buf, readBlob))
	r.Equal(content, buf.Bytes())
}

// TestExample_AddAndGetLocalSource demonstrates adding a source artifact to a
// component version and retrieving it.
func TestExample_AddAndGetLocalSource(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()
	repo := newTestRepo(t)

	component := "acme.org/source-app"
	version := "1.0.0"
	content := []byte("source archive content")

	src := &descriptor.Source{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    "my-source",
				Version: "1.0.0",
			},
		},
		Type: "git",
		Access: &v2.LocalBlob{
			LocalReference: digest.FromBytes(content).String(),
			MediaType:      "application/x-tar",
		},
	}

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			Provider: descriptor.Provider{Name: "acme.org"},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: component, Version: version},
			},
			Sources: []descriptor.Source{*src},
		},
	}

	// Upload the source blob.
	b := inmemory.New(bytes.NewReader(content))
	newSrc, err := repo.AddLocalSource(ctx, component, version, src, b)
	r.NoError(err)
	desc.Component.Sources[0] = *newSrc

	// Store the component version.
	r.NoError(repo.AddComponentVersion(ctx, desc))

	// Retrieve the source by identity.
	readBlob, _, err := repo.GetLocalSource(ctx, component, version, map[string]string{
		"name":    "my-source",
		"version": "1.0.0",
	})
	r.NoError(err)

	rc, err := readBlob.ReadCloser()
	r.NoError(err)
	defer rc.Close()

	data, err := io.ReadAll(rc)
	r.NoError(err)
	r.Equal(content, data)
}
