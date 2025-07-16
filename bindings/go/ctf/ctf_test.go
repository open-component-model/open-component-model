package ctf_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1"
	"ocm.software/open-component-model/bindings/go/ctf"
	v1 "ocm.software/open-component-model/bindings/go/ctf/index/v1"
)

func Test_CTF_ReadWrite(t *testing.T) {
	for _, format := range []ctf.FileFormat{
		ctf.FormatDirectory,
		ctf.FormatTAR,
		ctf.FormatTGZ,
	} {
		t.Run(format.String(), func(t *testing.T) {
			ctx := t.Context()
			r := require.New(t)
			name := "test" + map[ctf.FileFormat]string{
				ctf.FormatDirectory: "",
				ctf.FormatTAR:       ".tar",
				ctf.FormatTGZ:       ".tar.gz",
			}[format]
			path := filepath.Join(t.TempDir(), name)

			testBlob := inmemory.New(bytes.NewReader([]byte("test")))
			digest, _ := testBlob.Digest()

			err := ctf.WorkWithinCTF(ctx, ctf.OpenCTFOptions{
				Path: path,
				Flag: ctf.O_CREATE | ctf.O_RDWR,
			}, func(ctx context.Context, ctf ctf.CTF) error {
				if err := ctf.SaveBlob(ctx, testBlob); err != nil {
					return err
				}
				idx, err := ctf.GetIndex(ctx)
				if err != nil {
					return err
				}
				idx.AddArtifact(v1.ArtifactMetadata{
					Repository: "test-repo",
					Tag:        "latest",
					Digest:     digest,
					MediaType:  "application/json",
				})
				if err := ctf.SetIndex(ctx, idx); err != nil {
					return err
				}
				return nil
			})
			r.NoError(err)

			archive, discovered, err := ctf.OpenCTFByFileExtension(ctx, ctf.OpenCTFOptions{
				Path: path,
				Flag: ctf.O_RDONLY,
			})
			r.NoError(err)
			r.Equal(format, discovered)
			blobs, err := archive.ListBlobs(ctx)
			r.NoError(err)
			r.Len(blobs, 1)
			r.Contains(blobs, digest)
			blb, err := archive.GetBlob(ctx, digest)
			r.NoError(err)
			r.NotNil(blb)

			data, err := blb.ReadCloser()
			r.NoError(err)
			t.Cleanup(func() {
				r.NoError(data.Close())
			})
			readData, err := io.ReadAll(data)
			r.NoError(err)
			r.Equal("test", string(readData))
		})
	}
}

func Test_CTF_CustomTempFolder_TAR(t *testing.T) {
	ctx := context.Background()
	r := require.New(t)

	// Create a custom temp directory
	customTempDir := filepath.Join(t.TempDir(), "custom-temp")
	err := os.MkdirAll(customTempDir, 0755)
	r.NoError(err)

	// Create filesystem config with custom temp folder
	fsConfig := &v1alpha1.Config{
		TempFolder: customTempDir,
	}

	// Create a TAR file path
	tarPath := filepath.Join(t.TempDir(), "test.tar")

	testBlob := inmemory.New(bytes.NewReader([]byte("test data for custom temp")))
	digest, _ := testBlob.Digest()

	// Test that CTF operations use the custom temp folder
	err = ctf.WorkWithinCTF(ctx, ctf.OpenCTFOptions{
		Path:             tarPath,
		Flag:             ctf.O_CREATE | ctf.O_RDWR,
		FileSystemConfig: fsConfig,
	}, func(ctx context.Context, ctf ctf.CTF) error {
		if err := ctf.SaveBlob(ctx, testBlob); err != nil {
			return err
		}
		idx, err := ctf.GetIndex(ctx)
		if err != nil {
			return err
		}
		idx.AddArtifact(v1.ArtifactMetadata{
			Repository: "test-repo",
			Tag:        "custom-temp",
			Digest:     digest,
			MediaType:  "application/json",
		})
		if err := ctf.SetIndex(ctx, idx); err != nil {
			return err
		}

		// Verify that temporary files are created under our custom temp directory
		// Check if any directories starting with "ctf-" exist in our custom temp folder
		entries, err := os.ReadDir(customTempDir)
		if err != nil {
			return err
		}

		found := false
		for _, entry := range entries {
			if entry.IsDir() && strings.HasPrefix(entry.Name(), "ctf-") {
				found = true
				break
			}
		}
		r.True(found, "Expected to find ctf- temporary directory in custom temp folder")

		return nil
	})
	r.NoError(err)

	// Verify we can read the CTF back
	archive, discovered, err := ctf.OpenCTFByFileExtension(ctx, ctf.OpenCTFOptions{
		Path:             tarPath,
		Flag:             ctf.O_RDONLY,
		FileSystemConfig: fsConfig,
	})
	r.NoError(err)
	r.Equal(ctf.FormatTAR, discovered)

	blobs, err := archive.ListBlobs(ctx)
	r.NoError(err)
	r.Len(blobs, 1)
	r.Contains(blobs, digest)
}

func Test_CTF_CustomTempFolder_TGZ(t *testing.T) {
	ctx := context.Background()
	r := require.New(t)

	// Create a custom temp directory
	customTempDir := filepath.Join(t.TempDir(), "custom-temp-tgz")
	err := os.MkdirAll(customTempDir, 0755)
	r.NoError(err)

	// Create filesystem config with custom temp folder
	fsConfig := &v1alpha1.Config{
		TempFolder: customTempDir,
	}

	// Create a TGZ file path
	tgzPath := filepath.Join(t.TempDir(), "test.tar.gz")

	testBlob := inmemory.New(bytes.NewReader([]byte("test data for custom temp tgz")))
	digest, _ := testBlob.Digest()

	// Test that CTF operations use the custom temp folder
	err = ctf.WorkWithinCTF(ctx, ctf.OpenCTFOptions{
		Path:             tgzPath,
		Flag:             ctf.O_CREATE | ctf.O_RDWR,
		FileSystemConfig: fsConfig,
	}, func(ctx context.Context, ctf ctf.CTF) error {
		if err := ctf.SaveBlob(ctx, testBlob); err != nil {
			return err
		}
		idx, err := ctf.GetIndex(ctx)
		if err != nil {
			return err
		}
		idx.AddArtifact(v1.ArtifactMetadata{
			Repository: "test-repo",
			Tag:        "custom-temp-tgz",
			Digest:     digest,
			MediaType:  "application/json",
		})
		if err := ctf.SetIndex(ctx, idx); err != nil {
			return err
		}

		// Verify that temporary files are created under our custom temp directory
		entries, err := os.ReadDir(customTempDir)
		if err != nil {
			return err
		}

		found := false
		for _, entry := range entries {
			if entry.IsDir() && strings.HasPrefix(entry.Name(), "ctf-") {
				found = true
				break
			}
		}
		r.True(found, "Expected to find ctf- temporary directory in custom temp folder for TGZ")

		return nil
	})
	r.NoError(err)

	// Verify we can read the CTF back
	archive, discovered, err := ctf.OpenCTFByFileExtension(ctx, ctf.OpenCTFOptions{
		Path:             tgzPath,
		Flag:             ctf.O_RDONLY,
		FileSystemConfig: fsConfig,
	})
	r.NoError(err)
	r.Equal(ctf.FormatTGZ, discovered)

	blobs, err := archive.ListBlobs(ctx)
	r.NoError(err)
	r.Len(blobs, 1)
	r.Contains(blobs, digest)
}
