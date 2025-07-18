package transformer

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
)

// extractOCIArtifact goes through the layers and extracts specific information based on a function provided by the
// calling implementation.
func extractOCIArtifact(ctx context.Context, store content.Fetcher, artifact ociImageSpecV1.Descriptor, processLayerFunc func(ctx context.Context, store content.Fetcher, layer ociImageSpecV1.Descriptor, tarWriter *tar.Writer) error) (_ blob.ReadOnlyBlob, err error) {
	manifestReader, err := store.Fetch(ctx, artifact)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch artifact manifest: %w", err)
	}
	defer func() {
		err = errors.Join(err, manifestReader.Close())
	}()

	manifestData, err := io.ReadAll(manifestReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest data: %w", err)
	}

	var manifest ociImageSpecV1.Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("failed to unmarshal manifest: %w", err)
	}

	var tarBuffer bytes.Buffer
	tarWriter := tar.NewWriter(&tarBuffer)
	defer func() {
		err = errors.Join(err, tarWriter.Close())
	}()

	for _, layer := range manifest.Layers {
		if err := processLayerFunc(ctx, store, layer, tarWriter); err != nil {
			return nil, fmt.Errorf("failed to process layer %s: %w", layer.Digest, err)
		}
	}

	// create the right tar and the MediaType
	resultBlob := inmemory.New(bytes.NewReader(tarBuffer.Bytes()))
	resultBlob.SetMediaType("application/tar")

	return resultBlob, nil
}
