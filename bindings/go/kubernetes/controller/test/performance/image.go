package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/registry/remote"
)

// pushImage pushes a minimal but valid single-layer image. The layer content
// derives from the repository name, so every image has its own digest.
func pushImage(ctx context.Context, host string, plainHTTP bool, repository, tag string) error {
	repo, err := remote.NewRepository(host + "/" + repository)
	if err != nil {
		return err
	}
	repo.PlainHTTP = plainHTTP

	layer, diffID, err := imageLayer(repository)
	if err != nil {
		return fmt.Errorf("building layer: %w", err)
	}
	layerDesc, err := oras.PushBytes(ctx, repo, ocispec.MediaTypeImageLayerGzip, layer)
	if err != nil {
		return fmt.Errorf("pushing layer: %w", err)
	}

	config, err := json.Marshal(ocispec.Image{
		Platform: ocispec.Platform{Architecture: "amd64", OS: "linux"},
		RootFS:   ocispec.RootFS{Type: "layers", DiffIDs: []digest.Digest{diffID}},
	})
	if err != nil {
		return err
	}
	configDesc, err := oras.PushBytes(ctx, repo, ocispec.MediaTypeImageConfig, config)
	if err != nil {
		return fmt.Errorf("pushing config: %w", err)
	}

	manifest, err := oras.PackManifest(ctx, repo, oras.PackManifestVersion1_1, "", oras.PackManifestOptions{
		ConfigDescriptor: &configDesc,
		Layers:           []ocispec.Descriptor{layerDesc},
	})
	if err != nil {
		return fmt.Errorf("pushing manifest: %w", err)
	}
	return repo.Tag(ctx, manifest, tag)
}

// imageLayer returns a gzipped tar with one file, and the digest of the
// uncompressed tar that the image config lists as diff ID.
func imageLayer(content string) ([]byte, digest.Digest, error) {
	var tarball bytes.Buffer
	tw := tar.NewWriter(&tarball)
	if err := tw.WriteHeader(&tar.Header{Name: "resource", Mode: 0o644, Size: int64(len(content))}); err != nil {
		return nil, "", err
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		return nil, "", err
	}
	if err := tw.Close(); err != nil {
		return nil, "", err
	}

	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	if _, err := zw.Write(tarball.Bytes()); err != nil {
		return nil, "", err
	}
	if err := zw.Close(); err != nil {
		return nil, "", err
	}
	return compressed.Bytes(), digest.FromBytes(tarball.Bytes()), nil
}
