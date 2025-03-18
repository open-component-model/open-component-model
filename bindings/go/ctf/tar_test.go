package ctf_test

import (
	"os"
	"path/filepath"
	"testing"

	"ocm.software/open-component-model/bindings/go/ctf"
)

func Test_Sanity(t *testing.T) {
	t.Run("simple archiving and extraction from CTF that is pre-existing", func(t *testing.T) {
		path := filepath.Join("testdata", "archive")
		_, err := os.Stat(path)
		if err != nil {
			t.Skipf("skipping test, %s not found", path)
		}

		archive, err := ctf.OpenCTF(path, ctf.FormatDirectory, ctf.O_RDONLY)
		if err != nil {
			t.Errorf("unable to open CTF: %v", err)
		}

		tar := filepath.Join(t.TempDir(), "archive.tar")
		if err := ctf.Archive(archive, tar, ctf.FormatTAR); err != nil {
			t.Errorf("unable to archive CTF: %v", err)
		}

		if _, err = os.Stat(tar); err != nil {
			t.Errorf("archive not found: %v", err)
		}

		archive, err = ctf.OpenCTF(tar, ctf.FormatTAR, ctf.O_RDONLY)
		if err != nil {
			t.Errorf("unable to open CTF: %v", err)
		}

		index, err := archive.GetIndex()
		if err != nil {
			t.Errorf("unable to get index: %v", err)
		}
		if index == nil {
			t.Errorf("index is nil")
		}
		if len(index.GetArtifacts()) != 1 {
			t.Errorf("expected 1 artifact, got %d", len(index.GetArtifacts()))
		}

		blobs, err := archive.ListBlobs()
		if err != nil {
			t.Errorf("unable to list blobs: %v", err)
		}

		if len(blobs) != 3 {
			t.Errorf("expected 3 blob, got %d", len(blobs))
		}
	})

}
