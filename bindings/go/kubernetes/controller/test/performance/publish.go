package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"sync"

	"github.com/opencontainers/go-digest"
	"golang.org/x/sync/errgroup"
	"oras.land/oras-go/v2/registry/remote/retry"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	ociaccessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
)

const (
	resourceName        = "manifest"
	manifestMediaType   = "application/x-yaml"
	ociArtifactDigestV1 = "ociArtifactDigest/v1"
	publishWorkers      = 16
	// publishConns caps the connections to the registry, see newPublisher.
	publishConns = 4 * publishWorkers
)

// componentVersion is one component version the benchmark pushes.
type componentVersion struct {
	Name    string
	Version string
	// SubPath is the registry sub-path of the OCM repository; empty means the registry root.
	SubPath    string
	Resources  []resource
	References []reference
}

// resource is a local blob, or an OCI image access when ImageReference is set.
type resource struct {
	Name string
	Blob []byte
	// ImageReference is an image as the controller reaches it from inside the cluster.
	ImageReference string
}

// image is one OCI image the benchmark pushes for descriptors to reference.
type image struct {
	// Repository is relative to the registry.
	Repository string
	Tag        string
	// Reference is the image as the controller reaches it from inside the cluster.
	Reference string
}

type reference struct {
	Name      string
	Component string
	Version   string
}

// publisher pushes component versions to the benchmark registry.
type publisher struct {
	// host is the bare registry host; the resolver takes it as reference prefix.
	host      string
	plainHTTP bool
	tempDir   string
	client    *http.Client
	// signer is nil for unsigned workloads, which then carry no image or reference digests either.
	signer *signer

	mu sync.Mutex
	// imageDigests maps an image reference to its manifest digest.
	imageDigests map[string]digest.Digest
	// digests maps name:version to the normalised digest of a published component version.
	digests map[string]descriptor.Digest
}

func newPublisher(baseURL, tempDir string, s *signer) (*publisher, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parsing registry URL: %w", err)
	}
	// http.DefaultTransport keeps 2 idle connections per host, so a parallel publish dials thousands.
	// Docker Desktop's port forwarder drops connections under that churn, and oras does not retry the EOF.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxConnsPerHost = publishConns
	transport.MaxIdleConnsPerHost = publishConns
	client := &http.Client{Transport: retry.NewTransport(transport)}
	return &publisher{
		host: u.Host, plainHTTP: u.Scheme == "http", tempDir: tempDir, client: client, signer: s,
		imageDigests: map[string]digest.Digest{}, digests: map[string]descriptor.Digest{},
	}, nil
}

func (p *publisher) publishImages(ctx context.Context, images []image) error {
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(publishWorkers)
	for _, img := range images {
		g.Go(func() error {
			dig, err := pushImage(ctx, p.client, p.host, p.plainHTTP, img.Repository, img.Tag)
			if err != nil {
				return fmt.Errorf("pushing image %s: %w", img.Repository, err)
			}
			p.mu.Lock()
			defer p.mu.Unlock()
			p.imageDigests[img.Reference] = dig
			return nil
		})
	}
	return g.Wait()
}

// publish pushes level by level, leaves first: a reference digest needs the
// referenced component version to exist.
func (p *publisher) publish(ctx context.Context, cvs []componentVersion) error {
	for _, level := range levels(cvs) {
		if err := p.publishLevel(ctx, level); err != nil {
			return err
		}
	}
	return nil
}

func (p *publisher) publishLevel(ctx context.Context, cvs []componentVersion) error {
	g, ctx := errgroup.WithContext(ctx)
	work := make(chan componentVersion)

	g.Go(func() error {
		defer close(work)
		for _, cv := range cvs {
			select {
			case work <- cv:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})

	for range publishWorkers {
		g.Go(func() error {
			repos := map[string]*oci.Repository{}
			for cv := range work {
				repo, ok := repos[cv.SubPath]
				if !ok {
					var err error
					if repo, err = p.repository(cv.SubPath); err != nil {
						return err
					}
					repos[cv.SubPath] = repo
				}
				if err := p.push(ctx, repo, cv); err != nil {
					return fmt.Errorf("publishing %s:%s: %w", cv.Name, cv.Version, err)
				}
			}
			return nil
		})
	}

	return g.Wait()
}

func (p *publisher) repository(subPath string) (*oci.Repository, error) {
	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(p.host),
		urlresolver.WithSubPath(subPath),
		urlresolver.WithPlainHTTP(p.plainHTTP),
		urlresolver.WithBaseClient(p.client),
	)
	if err != nil {
		return nil, fmt.Errorf("creating resolver: %w", err)
	}

	scheme := ocmruntime.NewScheme()
	ociaccess.MustAddToScheme(scheme)
	v2.MustAddToScheme(scheme)

	tmp, err := os.MkdirTemp(p.tempDir, "publish-")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}

	return oci.NewRepository(oci.WithResolver(resolver), oci.WithScheme(scheme), oci.WithTempDir(tmp))
}

func (p *publisher) push(ctx context.Context, repo *oci.Repository, cv componentVersion) error {
	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			Provider: descriptor.Provider{Name: "ocm.software"},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: cv.Name, Version: cv.Version},
			},
		},
	}

	for _, r := range cv.Resources {
		meta := descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: r.Name, Version: cv.Version}}
		if r.ImageReference != "" {
			res := descriptor.Resource{
				Relation:    descriptor.ExternalRelation,
				ElementMeta: meta,
				Type:        "ociImage",
				Access: &ociaccessv1.OCIImage{
					Type:           ocmruntime.NewVersionedType(ociaccessv1.OCIImageType, "v1"),
					ImageReference: r.ImageReference,
				},
			}
			if p.signer != nil {
				dig, err := p.imageDigest(r.ImageReference)
				if err != nil {
					return err
				}
				res.Digest = dig
			}
			desc.Component.Resources = append(desc.Component.Resources, res)
			continue
		}

		added, err := repo.AddLocalResource(ctx, cv.Name, cv.Version, &descriptor.Resource{
			Relation:    descriptor.LocalRelation,
			ElementMeta: meta,
			Type:        "blob",
			Access: &v2.LocalBlob{
				LocalReference: digest.FromBytes(r.Blob).String(),
				MediaType:      manifestMediaType,
			},
		}, inmemory.New(bytes.NewReader(r.Blob)))
		if err != nil {
			return fmt.Errorf("adding local resource %s: %w", r.Name, err)
		}
		desc.Component.Resources = append(desc.Component.Resources, *added)
	}

	for _, ref := range cv.References {
		r := descriptor.Reference{
			ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: ref.Name, Version: ref.Version}},
			Component:   ref.Component,
		}
		if p.signer != nil {
			dig, err := p.componentDigest(ref.Component, ref.Version)
			if err != nil {
				return err
			}
			r.Digest = dig
		}
		desc.Component.References = append(desc.Component.References, r)
	}

	if p.signer != nil {
		if err := p.sign(ctx, desc); err != nil {
			return err
		}
	}
	if err := repo.AddComponentVersion(ctx, desc); err != nil {
		return fmt.Errorf("adding component version: %w", err)
	}
	return nil
}

// sign signs desc and records its digest for the component versions that reference it.
func (p *publisher) sign(ctx context.Context, desc *descriptor.Descriptor) error {
	// The controller accepts a descriptor that is not safely digestible without verifying it,
	// so a missing digest here would turn the signed run into an unsigned one without a trace.
	if err := signing.IsSafelyDigestible(&desc.Component); err != nil {
		return fmt.Errorf("%s:%s: %w", desc.Component.Name, desc.Component.Version, err)
	}
	dig, err := signing.GenerateDigest(ctx, desc, slog.Default(), v4alpha1.Algorithm, signingHash)
	if err != nil {
		return fmt.Errorf("digesting %s:%s: %w", desc.Component.Name, desc.Component.Version, err)
	}
	if err := p.signer.sign(ctx, desc, *dig); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.digests[desc.Component.Name+":"+desc.Component.Version] = *dig
	return nil
}

func (p *publisher) imageDigest(reference string) (*descriptor.Digest, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	dig, ok := p.imageDigests[reference]
	if !ok {
		return nil, fmt.Errorf("image %s was not pushed", reference)
	}
	return &descriptor.Digest{HashAlgorithm: signingHash, NormalisationAlgorithm: ociArtifactDigestV1, Value: dig.Encoded()}, nil
}

func (p *publisher) componentDigest(name, version string) (descriptor.Digest, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	dig, ok := p.digests[name+":"+version]
	if !ok {
		return descriptor.Digest{}, fmt.Errorf("referenced component version %s:%s was not published before its parent", name, version)
	}
	return dig, nil
}

// levels groups component versions so that every level references only earlier ones.
func levels(cvs []componentVersion) [][]componentVersion {
	index := make(map[string]int, len(cvs))
	for i, cv := range cvs {
		index[cv.Name+":"+cv.Version] = i
	}
	depth := make([]int, len(cvs))
	for i := range depth {
		depth[i] = -1
	}
	var walk func(i int) int
	walk = func(i int) int {
		if depth[i] < 0 {
			depth[i] = 0
			for _, ref := range cvs[i].References {
				if j, ok := index[ref.Component+":"+ref.Version]; ok {
					depth[i] = max(depth[i], walk(j)+1)
				}
			}
		}
		return depth[i]
	}

	var out [][]componentVersion
	for i, cv := range cvs {
		d := walk(i)
		for len(out) <= d {
			out = append(out, nil)
		}
		out[d] = append(out[d], cv)
	}
	return out
}
