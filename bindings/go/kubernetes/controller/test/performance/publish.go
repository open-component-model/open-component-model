package main

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"

	"github.com/opencontainers/go-digest"
	"golang.org/x/sync/errgroup"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	ociaccessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

const (
	resourceName      = "manifest"
	manifestMediaType = "application/x-yaml"
	publishWorkers    = 16
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
}

func newPublisher(baseURL, tempDir string) (*publisher, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parsing registry URL: %w", err)
	}
	return &publisher{host: u.Host, plainHTTP: u.Scheme == "http", tempDir: tempDir}, nil
}

func (p *publisher) publishImages(ctx context.Context, images []image) error {
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(publishWorkers)
	for _, img := range images {
		g.Go(func() error {
			if err := pushImage(ctx, p.host, p.plainHTTP, img.Repository, img.Tag); err != nil {
				return fmt.Errorf("pushing image %s: %w", img.Repository, err)
			}
			return nil
		})
	}
	return g.Wait()
}

func (p *publisher) publish(ctx context.Context, cvs []componentVersion) error {
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
			desc.Component.Resources = append(desc.Component.Resources, descriptor.Resource{
				Relation:    descriptor.ExternalRelation,
				ElementMeta: meta,
				Type:        "ociImage",
				Access: &ociaccessv1.OCIImage{
					Type:           ocmruntime.NewVersionedType(ociaccessv1.OCIImageType, "v1"),
					ImageReference: r.ImageReference,
				},
			})
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
		desc.Component.References = append(desc.Component.References, descriptor.Reference{
			ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: ref.Name, Version: ref.Version}},
			Component:   ref.Component,
		})
	}

	if err := repo.AddComponentVersion(ctx, desc); err != nil {
		return fmt.Errorf("adding component version: %w", err)
	}
	return nil
}
