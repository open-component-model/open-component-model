package oci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"

	v1 "ocm.software/open-component-model/bindings/go/oci/access/v1"
)

func getOCIImageLayerRecursively(ctx context.Context, src Store, desc ociImageSpecV1.Descriptor, layer *v1.OCIImageLayer) (ociImageSpecV1.Descriptor, error) {
	if desc.Digest == layer.Digest {
		return desc, nil
	}

	var manifests []ociImageSpecV1.Descriptor
	var err error

	switch desc.MediaType {
	case ociImageSpecV1.MediaTypeImageIndex:
		manifests, err = getManifestsFromIndex(ctx, src, desc)
	case ociImageSpecV1.MediaTypeImageManifest:
		manifests, err = getLayersFromManifest(ctx, src, desc)
	default:
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("layer %s not found", layer.Digest)
	}

	if err != nil {
		return ociImageSpecV1.Descriptor{}, err
	}

	resultChan := make(chan ociImageSpecV1.Descriptor)
	errChan := make(chan error)
	var wg sync.WaitGroup

	for _, m := range manifests {
		wg.Add(1)
		go func(m ociImageSpecV1.Descriptor) {
			defer wg.Done()
			if desc, err := getOCIImageLayerRecursively(ctx, src, m, layer); err != nil {
				errChan <- err
			} else {
				resultChan <- desc
			}
		}(m)
	}

	// Close channels when all goroutines complete
	go func() {
		wg.Wait()
		close(resultChan)
		close(errChan)
	}()

	// Collect all errors
	var errs []error
	done := make(chan struct{})
	go func() {
		for err := range errChan {
			errs = append(errs, err)
		}
		close(done)
	}()

	// Wait for success or all errors
	select {
	case result, ok := <-resultChan:
		if ok {
			return result, nil
		}
		<-done // Wait for error collection to complete
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("layer %s not found: %w", layer.Digest, errors.Join(errs...))
	case <-ctx.Done():
		return ociImageSpecV1.Descriptor{}, ctx.Err()
	}
}

func getManifestsFromIndex(ctx context.Context, src Store, desc ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error) {
	idx, err := src.Fetch(ctx, desc)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch index: %w", err)
	}
	defer func() {
		err = errors.Join(err, idx.Close())
	}()

	var index ociImageSpecV1.Index
	if err := json.NewDecoder(idx).Decode(&index); err != nil {
		return nil, fmt.Errorf("failed to decode index: %w", err)
	}

	return index.Manifests, nil
}

func getLayersFromManifest(ctx context.Context, src Store, desc ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error) {
	manifestStream, err := src.Fetch(ctx, desc)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch manifest: %w", err)
	}
	defer func() {
		err = errors.Join(err, manifestStream.Close())
	}()

	var manifest ociImageSpecV1.Manifest
	if err := json.NewDecoder(manifestStream).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("failed to decode manifest: %w", err)
	}

	return manifest.Layers, nil
}
