package ctf

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"

	"github.com/opencontainers/go-digest"
	ociimagespec "github.com/opencontainers/image-spec/specs-go/v1"
)

// completeIndexDescriptor fills in the media type and size of an artifact set
// index entry if the legacy OCM library omitted them. Without a media type, OCI
// tooling cannot find the entry's successors, and without a size it cannot verify it.
func completeIndexDescriptor(fsys fs.FS, format ArtifactSetFormat, desc *ociimagespec.Descriptor) error {
	if desc.MediaType != "" && desc.Size > 0 {
		return nil
	}
	name, err := artifactSetBlobPath(desc.Digest.String(), format)
	if err != nil {
		return err
	}
	if desc.Size <= 0 {
		info, err := fs.Stat(fsys, name)
		if err != nil {
			return fmt.Errorf("unable to stat index entry %s: %w", desc.Digest, err)
		}
		desc.Size = info.Size()
	}
	if desc.MediaType == "" {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return fmt.Errorf("unable to read index entry %s: %w", desc.Digest, err)
		}
		if desc.MediaType, err = manifestMediaType(data); err != nil {
			return fmt.Errorf("unable to determine media type of index entry %s: %w", desc.Digest, err)
		}
	}
	return nil
}

// manifestMediaType returns the media type a manifest or index declares itself,
// falling back to its structure if the optional mediaType field is absent.
func manifestMediaType(data []byte) (string, error) {
	var probe struct {
		MediaType string          `json:"mediaType"`
		Manifests json.RawMessage `json:"manifests"`
		Config    json.RawMessage `json:"config"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return "", fmt.Errorf("not a manifest or index: %w", err)
	}
	switch {
	case probe.MediaType != "":
		return probe.MediaType, nil
	case probe.Manifests != nil:
		return ociimagespec.MediaTypeImageIndex, nil
	case probe.Config != nil:
		return ociimagespec.MediaTypeImageManifest, nil
	default:
		return "", fmt.Errorf("neither mediaType, manifests nor config present")
	}
}

// artifactSetBlobPath returns the path of the blob with the given digest within
// an artifact set of the given format.
func artifactSetBlobPath(dig string, format ArtifactSetFormat) (string, error) {
	if format == ArtifactSetFormatOCI {
		d, err := digest.Parse(dig)
		if err != nil {
			return "", fmt.Errorf("invalid digest %q: %w", dig, err)
		}
		return path.Join(BlobsDirectoryName, d.Algorithm().String(), d.Encoded()), nil
	}
	file, err := ToBlobFileName(dig)
	if err != nil {
		return "", err
	}
	return path.Join(BlobsDirectoryName, file), nil
}
