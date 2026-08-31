package externalartifact

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	fluxtar "github.com/fluxcd/pkg/tar"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/ocm"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution"
	"ocm.software/open-component-model/kubernetes/controller/internal/resolution/workerpool"
	"ocm.software/open-component-model/kubernetes/controller/internal/setup"
	"ocm.software/open-component-model/kubernetes/controller/internal/util"
	"ocm.software/open-component-model/kubernetes/controller/internal/verification"
	"ocm.software/open-component-model/kubernetes/controller/pkg/configuration"
)

// gzipMagic are the first two bytes of a gzip stream (RFC 1952). Downloaded OCM
// resources that carry Helm charts are gzip-compressed tar archives; anything
// else (e.g. a plain Kustomization manifest) is treated as a single file.
var gzipMagic = []byte{0x1f, 0x8b}

// resolveAndDownload takes a Ready Resource and materialises its OCM content
// into destDir on the local filesystem, ready to be packaged into a
// Flux artifact.
//
//   - Helm charts (and any other gzip'd tar the resource carries) are untarred
//     into destDir so the re-packaged artifact contains the chart directory,
//     which is what the Flux helm-controller expects behind a chartRef.
//   - Everything else (Kustomize overlays, plain manifests) is written verbatim
//     as a single file so the Flux kustomize-controller can build it.
//
// It returns the matched OCM resource descriptor so the caller can derive the
// artifact revision from its version.
func (r *Reconciler) resolveAndDownload(
	ctx context.Context,
	resource *v1alpha1.Resource,
	destDir string,
) (*descriptor.Resource, error) {
	cfg, err := r.loadConfiguration(ctx, resource)
	if err != nil {
		return nil, err
	}

	repo, componentDescriptor, err := r.resolveComponentVersion(ctx, resource, cfg)
	if err != nil {
		return nil, err
	}

	matchedResource, err := matchResource(resource, componentDescriptor)
	if err != nil {
		return nil, err
	}

	resourceBlob, err := downloadResourceBlob(ctx, r.PluginManager, repo, componentDescriptor, matchedResource, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to download resource blob: %w", err)
	}

	if err := materialise(ctx, resourceBlob, matchedResource, destDir, r.MaxResourceSizeBytes); err != nil {
		return nil, err
	}

	return matchedResource, nil
}

// loadConfiguration resolves and loads the effective OCM configuration for the
// resource, mirroring the resource/deployer controllers.
func (r *Reconciler) loadConfiguration(
	ctx context.Context,
	resource *v1alpha1.Resource,
) (*configuration.Configuration, error) {
	configs := resource.Status.EffectiveOCMConfig
	cfg, err := configuration.LoadConfigurations(ctx, r.Client, resource.GetNamespace(), configs)
	if err != nil {
		return nil, fmt.Errorf("failed to load configurations: %w", err)
	}

	return cfg, nil
}

// resolveComponentVersion builds a cache-backed repository from the resource's
// component status and resolves the (possibly reference-path-nested) component
// descriptor the resource belongs to.
func (r *Reconciler) resolveComponentVersion(
	ctx context.Context,
	resource *v1alpha1.Resource,
	cfg *configuration.Configuration,
) (*resolution.CacheBackedRepository, *descriptor.Descriptor, error) {
	if resource.Status.Component == nil || resource.Status.Component.RepositorySpec == nil {
		return nil, nil, fmt.Errorf("resource component status is not populated")
	}

	repoSpec := &ocmruntime.Raw{}
	if err := repoSpec.UnmarshalJSON(resource.Status.Component.RepositorySpec.Raw); err != nil {
		return nil, nil, fmt.Errorf("failed to decode repository spec: %w", err)
	}

	component, err := util.GetReadyObject[v1alpha1.Component, *v1alpha1.Component](ctx, r.Client, client.ObjectKey{
		Namespace: resource.GetNamespace(),
		Name:      resource.Spec.ComponentRef.Name,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get ready component: %w", err)
	}

	verifications, err := verification.GetVerifications(ctx, r.Client, component)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get verifications: %w", err)
	}

	requesterFunc := func() workerpool.RequesterInfo {
		return workerpool.RequesterInfo{
			NamespacedName: k8stypes.NamespacedName{
				Namespace: resource.GetNamespace(),
				Name:      resource.GetName(),
			},
		}
	}

	opts := &resolution.RepositoryOptions{
		RepositorySpec:  repoSpec,
		Configuration:   cfg,
		SigningRegistry: r.PluginManager.SigningRegistry,
		Verifications:   verifications,
		RequesterFunc:   requesterFunc,
	}

	repo, err := r.Resolver.NewCacheBackedRepository(ctx, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create cache-backed repository: %w", err)
	}

	componentDescriptor, err := repo.GetComponentVersion(ctx,
		resource.Status.Component.Component,
		resource.Status.Component.Version)
	if err != nil && !errors.Is(err, workerpool.ErrNotSafelyDigestible) {
		return nil, nil, fmt.Errorf("failed to get component version: %w", err)
	}

	// The resource status already records the component version the resource was
	// resolved against (including any reference-path nesting done by the resource
	// controller), so no further reference-path walk is required here.
	return repo, componentDescriptor, nil
}

// matchResource finds the descriptor resource matching the Resource CR's
// referenced identity within the component descriptor.
func matchResource(
	resource *v1alpha1.Resource,
	componentDescriptor *descriptor.Descriptor,
) (*descriptor.Resource, error) {
	resourceIdentity := resource.Spec.Resource.ByReference.Resource
	for i, res := range componentDescriptor.Component.Resources {
		if resourceIdentity.Match(res.ToIdentity(), ocm.IdentityFuncIgnoreVersion()) {
			return &componentDescriptor.Component.Resources[i], nil
		}
	}

	return nil, fmt.Errorf("resource with identity %v not found in component %s:%s",
		resourceIdentity, componentDescriptor.Component.Name, componentDescriptor.Component.Version)
}

// downloadResourceBlob downloads a resource blob using either the repository
// (for local blobs) or the plugin manager (for external access types like OCI
// images). This mirrors the deployer's resource download path so the two stay
// consistent in how they access OCM content.
func downloadResourceBlob(
	ctx context.Context,
	pm *manager.PluginManager,
	repo *resolution.CacheBackedRepository,
	componentDescriptor *descriptor.Descriptor,
	resource *descriptor.Resource,
	cfg *configuration.Configuration,
) (blob.ReadOnlyBlob, error) {
	typed, err := v2.Scheme.NewObject(resource.Access.GetType())
	if err != nil {
		return nil, fmt.Errorf("failed to resolve access type: %w", err)
	}

	if _, ok := typed.(*v2.LocalBlob); ok {
		localBlob, _, err := repo.GetLocalResource(ctx,
			componentDescriptor.Component.Name,
			componentDescriptor.Component.Version,
			resource.ToIdentity())
		if err != nil {
			return nil, fmt.Errorf("failed to get local resource: %w", err)
		}

		return localBlob, nil
	}

	// Non-local access types (e.g. OCI artifacts) use the plugin manager.
	resourcePlugin, err := pm.ResourcePluginRegistry.GetResourcePlugin(ctx, resource.Access)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource plugin: %w", err)
	}

	creds, err := resolveResourceCredentials(ctx, pm, resource, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve credentials: %w", err)
	}

	return resourcePlugin.DownloadResource(ctx, resource, creds)
}

// resolveResourceCredentials resolves credentials for accessing a resource via
// a resource plugin.
func resolveResourceCredentials(
	ctx context.Context,
	pm *manager.PluginManager,
	resource *descriptor.Resource,
	cfg *configuration.Configuration,
) (ocmruntime.Typed, error) {
	if cfg == nil {
		return nil, nil
	}

	resourcePlugin, err := pm.ResourcePluginRegistry.GetResourcePlugin(ctx, resource.Access)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource plugin: %w", err)
	}

	id, err := resourcePlugin.GetResourceCredentialConsumerIdentity(ctx, resource)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource credential consumer identity: %w", err)
	}

	logger := log.FromContext(ctx)
	credGraph, err := setup.NewCredentialGraph(ctx, cfg.Config, setup.CredentialGraphOptions{
		PluginManager: pm,
		Logger:        &logger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create credential graph: %w", err)
	}

	creds, err := credGraph.Resolve(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve credentials: %w", err)
	}

	return creds, nil
}

// materialise writes the resource blob into destDir. gzip'd tar archives (Helm
// charts) are extracted; all other content is written as a single file named
// after the resource. maxSize caps the uncompressed bytes written (0 disables
// the cap), guarding against decompression bombs and oversized resources.
func materialise(
	ctx context.Context,
	resourceBlob blob.ReadOnlyBlob,
	resource *descriptor.Resource,
	destDir string,
	maxSize int64,
) (err error) {
	reader, err := resourceBlob.ReadCloser()
	if err != nil {
		return fmt.Errorf("failed to get reader for resource blob: %w", err)
	}
	defer func() { err = errors.Join(err, reader.Close()) }()

	// Peek at the first bytes to detect a gzip stream without consuming the
	// reader.
	buffered := bufio.NewReader(reader)
	header, perr := buffered.Peek(len(gzipMagic))
	if perr != nil && !errors.Is(perr, io.EOF) {
		return fmt.Errorf("failed to inspect resource content: %w", perr)
	}

	if bytes.Equal(header, gzipMagic) {
		// Helm chart / gzip'd tar: extract into destDir so the artifact is a
		// directory tree the Flux controllers can consume directly.
		//
		// The extraction preserves the archive's internal layout. This matters
		// for Helm charts: the Flux helm-controller requires the chart to live
		// under a single base directory (e.g. "mychart/Chart.yaml") and rejects
		// artifacts whose content sits at the tar root with
		// "chart illegally contains content outside the base directory".
		// A real OCM Helm chart resource is a `helm package` .tgz, which already
		// nests everything under <chartname>/, so extract-then-repackage keeps
		// that base dir intact. Verified end-to-end against a real Flux
		// helm-controller (see test/e2e-flux).
		//
		// WithMaxUntarSize bounds the extracted size to guard against
		// decompression bombs; a non-positive maxSize disables the cap.
		untarSize := untarSizeLimit(maxSize)
		if err := fluxtar.Untar(buffered, destDir, fluxtar.WithMaxUntarSize(untarSize)); err != nil {
			return fmt.Errorf("failed to extract resource archive: %w", err)
		}

		return nil
	}

	// Non-archive content: write verbatim as a single file.
	fileName := singleFileName(resource)
	log.FromContext(ctx).V(1).Info("writing resource as single file", "file", fileName)

	target := filepath.Join(destDir, fileName)
	out, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("failed to create artifact file: %w", err)
	}
	defer func() { err = errors.Join(err, out.Close()) }()

	// Cap the copy to guard against oversized resources.
	var src io.Reader = buffered
	if maxSize > 0 {
		// Read one extra byte so we can detect an over-limit resource rather than
		// silently truncating it.
		src = io.LimitReader(buffered, maxSize+1)
	}
	written, err := io.Copy(out, src)
	if err != nil {
		return fmt.Errorf("failed to write resource content: %w", err)
	}
	if maxSize > 0 && written > maxSize {
		return fmt.Errorf("%w: resource exceeds %d bytes", ErrArtifactTooLarge, maxSize)
	}

	return nil
}

// untarSizeLimit converts a byte cap (0 = unlimited) into the fluxtar
// convention where a negative value means unlimited.
func untarSizeLimit(maxSize int64) int {
	if maxSize <= 0 {
		return -1
	}

	return int(maxSize)
}

// singleFileName derives a stable, path-safe file name for a non-archive
// resource. The OCM resource name is attacker-influenceable, so it is reduced to
// a single, separator-free component with no parent references — independent of
// the host OS, since the name may later appear in a URL or be consumed on a
// different platform. Content intended for the Flux kustomize-controller must
// have a .yaml extension to be picked up, so unknown types default to a YAML
// manifest name.
func singleFileName(resource *descriptor.Resource) string {
	// Normalise both separator styles to '/', take the last segment, and reject
	// anything that isn't a clean single component.
	normalised := strings.ReplaceAll(resource.Name, "\\", "/")
	name := path.Base(path.Clean("/" + normalised))
	if name == "" || name == "." || name == "/" || name == ".." || strings.Contains(name, "..") {
		name = "resource"
	}

	switch resource.Type {
	case "helmChart":
		return name + ".tgz"
	default:
		return name + ".yaml"
	}
}
