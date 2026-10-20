package repository

import (
	"context"
	"fmt"
	"net/http"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/wget/checksum"
	"ocm.software/open-component-model/bindings/go/wget/checksum/httpverify"
	"ocm.software/open-component-model/bindings/go/wget/internal/download"
	v1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

// newStream builds the lazy blob for the wget access. No request is sent.
//
// The configured checksum policy runs over the streamed bytes once the body ends:
// under Require and Prefer the source-advertised checksum is verified, and Skip
// performs no policy verification. A mismatch fails the final read and the close.
func (r *ResourceRepository) newStream(ctx context.Context, wget *v1.Wget, credentials runtime.Typed) *download.Stream {
	policy, hasPolicy := httpverify.PolicyForMode(r.checksumConfig.ModeForURL(wget.URL))

	opts := []download.Option{
		download.WithClient(r.client),
		download.WithMaxDownloadSize(r.maxDownloadSize),
		download.WithCredentials(credentials),
	}

	var verify download.VerifyFunc
	if hasPolicy {
		opts = append(opts, download.WithDigestAlgorithms(httpverify.DigestAlgorithms(policy)...))
		url := wget.URL
		verify = func(headers http.Header, digests map[string]string) error {
			if _, _, err := checksum.Resolve(ctx, policy, checksum.Input{
				URL:      url,
				Headers:  headers,
				Computed: digests,
			}); err != nil {
				return fmt.Errorf("checksum verification failed for wget access %q: %w", url, err)
			}
			return nil
		}
	}

	return download.NewStream(ctx, download.Request{
		URL:        wget.URL,
		MediaType:  wget.MediaType,
		Header:     wget.Header,
		Verb:       wget.Verb,
		Body:       wget.Body,
		NoRedirect: wget.NoRedirect,
	}, verify, opts...)
}

// streamResource wraps stream so that the content is verified against the
// resource digest as it is read. When the access does not pin a media type, the
// request is sent once here to read it from the response; the first read of the
// returned blob uses that response, so no second request is sent.
func streamResource(ctx context.Context, resource *descriptor.Resource, stream *download.Stream) (blob.ReadOnlyBlob, error) {
	verified, err := repository.VerifyDownload(ctx, resource, stream)
	if err != nil {
		return nil, err
	}
	if _, known := stream.MediaType(); !known {
		if err := stream.Prime(); err != nil {
			_ = stream.Close()
			return nil, err
		}
	}
	return verified, nil
}
