// Package stream opens wget resources as HTTP response streams without writing them to disk,
// for consumers that forward the content right away (e.g. uploads to another HTTP target).
package stream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ocmhttp "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/wget/internal/download"
	accessspec "ocm.software/open-component-model/bindings/go/wget/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

// Streamer opens resources with a Wget access as the body of the HTTP response.
type Streamer struct {
	HTTPConfig *httpv1alpha1.Config
}

// OpenResource sends the request described by the Wget access of resource, authenticated
// with credentials (WgetCredentials/v1 or Credentials/v1), and returns the response body and
// its size (-1 when unknown). The caller must close the body. Resources with another access
// type yield errors.ErrUnsupported.
//
// Unlike the wget resource repository, no source-advertised checksum policy is applied: the
// content is not available before it has been consumed, so callers verify it themselves.
func (s *Streamer) OpenResource(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (io.ReadCloser, int64, error) {
	if resource == nil || resource.Access == nil {
		return nil, 0, errors.New("resource access is required")
	}
	if !accessspec.Scheme.IsRegistered(resource.Access.GetType()) {
		return nil, 0, errors.ErrUnsupported
	}
	var wget v1.Wget
	if err := accessspec.Scheme.Convert(resource.Access, &wget); err != nil {
		return nil, 0, fmt.Errorf("error converting resource access spec: %w", err)
	}
	header := http.Header(wget.Header).Clone()
	if header == nil {
		header = http.Header{}
	}
	// An explicit Accept-Encoding stops Go's transport from transparently decompressing the
	// body, so the stream carries exactly the bytes the server stores.
	header.Set("Accept-Encoding", "identity")
	resp, err := download.Open(ctx, download.Request{
		URL:        wget.URL,
		Header:     header,
		Verb:       wget.Verb,
		Body:       wget.Body,
		NoRedirect: wget.NoRedirect,
	},
		download.WithClient(ocmhttp.New(ocmhttp.WithConfig(s.HTTPConfig))),
		download.WithCredentials(credentials),
	)
	if err != nil {
		return nil, 0, err
	}
	return resp.Body, resp.ContentLength, nil
}
