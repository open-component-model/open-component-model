package input

import (
	"context"
	"fmt"
	nethttp "net/http"
	"net/url"
	"strings"

	godigest "github.com/opencontainers/go-digest"

	checksumhttpv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/checksum/http/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	httpclient "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/wget/checksum"
	"ocm.software/open-component-model/bindings/go/wget/checksum/httpverify"
	"ocm.software/open-component-model/bindings/go/wget/internal/download"
	wgetcreds "ocm.software/open-component-model/bindings/go/wget/spec/credentials"
	wgetidentity "ocm.software/open-component-model/bindings/go/wget/spec/identity"
	identityv1 "ocm.software/open-component-model/bindings/go/wget/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/wget/spec/input"
	v1 "ocm.software/open-component-model/bindings/go/wget/spec/input/v1"
)

// genericBlobDigestV1 is the OCM normalisation algorithm recorded for a plain
// downloaded blob.
const genericBlobDigestV1 = "genericBlobDigest/v1"

var _ constructor.ResourceInputMethod = (*InputMethod)(nil)

// InputMethod implements the [constructor.ResourceInputMethod] interface for wget-based inputs.
// It downloads a resource from an HTTP/S URL declared in the component constructor
// and returns it as a local blob to be stored in the component version.
type InputMethod struct {
	// HTTPConfig configures the HTTP client (timeouts, retries, TLS, routing) used for
	// downloads. When nil, a default client is used.
	HTTPConfig *httpv1alpha1.Config
	// ChecksumConfig steers the [checksumhttpv1alpha1.ChecksumMode] applied to
	// each downloaded resource. When nil (or when no entry matches the URL),
	// the digest is computed from the stream without verification.
	ChecksumConfig *checksumhttpv1alpha1.Config
	// MaxDownloadSize limits the number of bytes read from a response body. When zero,
	// the download package default [download.DefaultMaxDownloadSize] is used. A negative value disables the limit.
	MaxDownloadSize int64
}

func (i *InputMethod) GetInputMethodScheme() *runtime.Scheme {
	return input.Scheme
}

// GetResourceCredentialConsumerIdentity resolves the credential consumer identity for a
// wget input from its URL, using the same wget consumer type as the access type so that
// credentials configured for a host resolve for both.
func (i *InputMethod) GetResourceCredentialConsumerIdentity(_ context.Context, resource *constructorruntime.Resource) (runtime.Identity, error) {
	wget := v1.Wget{}
	if err := i.GetInputMethodScheme().Convert(resource.Input, &wget); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	if wget.URL == "" {
		return nil, fmt.Errorf("url is required")
	}

	parsed, err := url.Parse(wget.URL)
	if err != nil {
		return nil, fmt.Errorf("wget url is not a valid url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("wget url must use http or https scheme, got %q", parsed.Scheme)
	}

	identity, err := identityv1.IdentityFromURL(wget.URL)
	if err != nil {
		return nil, fmt.Errorf("error parsing wget URL to identity: %w", err)
	}

	return identity, nil
}

// ProcessResource opens the resource described by the wget input specification and
// returns a blob streaming it as local blob data to be stored in the component version.
//
// Compression is a transport concern: gzip is requested, and the stored content
// and its digest are what a request accepting no compression would return
// (docs/adr/0031_wget_transfer_compression.md). Negotiated gzip is decoded, while
// a Content-Encoding the server keeps for uncompressed requests is part of the
// content. Content-Digest headers, which cover the transferred bytes, are verified
// against those but never pin the content digest.
//
// The blob's digest is known up front when the resource pins a SHA-256 digest or
// the source advertises one for the content. The pinned digest and the checksum
// policy are verified against the bytes as they are read: a mismatch fails the
// blob's reader at the end of the body (its last Read and its Close).
func (i *InputMethod) ProcessResource(ctx context.Context, resource *constructorruntime.Resource, credentials runtime.Typed) (*constructor.ResourceInputMethodResult, error) {
	wget := v1.Wget{}
	if err := i.GetInputMethodScheme().Convert(resource.Input, &wget); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	if wget.URL == "" {
		return nil, fmt.Errorf("url is required in wget input spec")
	}

	var client *nethttp.Client
	if i.HTTPConfig != nil {
		client = httpclient.New(httpclient.WithConfig(i.HTTPConfig))
	}

	policy, hasPolicy := httpverify.PolicyForMode(i.ChecksumConfig.ModeForURL(wget.URL))

	var providedHex string
	if resource.Digest != nil {
		var err error
		if providedHex, err = providedSHA256(resource.Digest); err != nil {
			return nil, fmt.Errorf("provided digest verification failed for wget input from %q: %w", wget.URL, err)
		}
	}

	opts := []download.Option{
		download.WithClient(client),
		download.WithCredentials(credentials),
	}
	if i.MaxDownloadSize != 0 {
		opts = append(opts, download.WithMaxDownloadSize(i.MaxDownloadSize))
	}
	// Compute digests while reading iff a policy needs them or the resource
	// carries a provided digest to verify against.
	if hasPolicy || resource.Digest != nil {
		opts = append(opts,
			download.WithDigestAlgorithms(httpverify.DigestAlgorithms(policy)...),
			download.WithVerifier(func(header nethttp.Header, computed download.Computed) error {
				if hasPolicy {
					if err := httpverify.Verify(ctx, wget.URL, policy, header, computed); err != nil {
						return fmt.Errorf("checksum verification failed for wget input from %q: %w", wget.URL, err)
					}
				}
				if computed.Pinned != "" {
					want := strings.TrimPrefix(computed.Pinned, "sha256:")
					got := computed.Content[checksum.StorageAlgorithm.OCMName]
					if !strings.EqualFold(want, got) {
						return fmt.Errorf("digest mismatch for wget input from %q: expected %s, computed %s", wget.URL, want, got)
					}
				}
				return nil
			}),
		)
	}

	data, err := download.Download(ctx, download.Request{
		URL:              wget.URL,
		MediaType:        wget.MediaType,
		Header:           wget.Header,
		Verb:             wget.Verb,
		Body:             wget.Body,
		NoRedirect:       wget.NoRedirect,
		CompressTransfer: true,
	}, opts...)
	if err != nil {
		return nil, fmt.Errorf("error downloading wget input from %q: %w", wget.URL, err)
	}

	if hasPolicy {
		if _, _, err := checksum.Expect(ctx, policy, checksum.Input{URL: wget.URL, Headers: data.Headers()}, nil); err != nil {
			_ = data.Close()
			return nil, fmt.Errorf("checksum verification failed for wget input from %q: %w", wget.URL, err)
		}
	}

	// Pin SHA-256 when it is known before reading; the verifier checks it against
	// the bytes even when the policy verified another algorithm.
	switch {
	case providedHex != "":
		data.SetPrecalculatedDigest("sha256:" + providedHex)
	case hasPolicy:
		// A checksum over the transferred bytes does not describe decoded content.
		exp, ok, err := checksum.ResolveAdvertised(ctx, policy,
			checksum.Input{URL: wget.URL, Headers: data.Headers(), Decoded: data.Decoded()},
			[]checksum.Algorithm{checksum.StorageAlgorithm})
		if err != nil {
			_ = data.Close()
			return nil, fmt.Errorf("checksum verification failed for wget input from %q: %w", wget.URL, err)
		}
		if ok {
			data.SetPrecalculatedDigest("sha256:" + exp.Value)
		}
	}

	return &constructor.ResourceInputMethodResult{
		ProcessedBlobData: data,
	}, nil
}

func (i *InputMethod) GetCredentialTypeScheme() *runtime.Scheme {
	return wgetcreds.Scheme
}

// GetConsumerIdentityTypeScheme returns the scheme with the consumer identity types the
// wget input method resolves credentials for, including the HTTP aliases.
func (i *InputMethod) GetConsumerIdentityTypeScheme() *runtime.Scheme {
	return wgetidentity.Scheme
}

// providedSHA256 validates a digest pinned on the resource and returns its
// lowercase hex value. Only SHA-256/genericBlobDigest is supported.
func providedSHA256(provided *constructorruntime.Digest) (string, error) {
	if provided.HashAlgorithm != "" && !strings.EqualFold(provided.HashAlgorithm, checksum.StorageAlgorithm.OCMName) {
		return "", fmt.Errorf("unsupported provided hash algorithm %q: only %s is supported", provided.HashAlgorithm, checksum.StorageAlgorithm.OCMName)
	}
	if provided.NormalisationAlgorithm != "" && provided.NormalisationAlgorithm != genericBlobDigestV1 {
		return "", fmt.Errorf("unsupported provided normalisation algorithm %q: only %s is supported", provided.NormalisationAlgorithm, genericBlobDigestV1)
	}
	// The provided value may be bare hex or go-digest form (sha256:<hex>).
	hex := strings.ToLower(strings.TrimPrefix(provided.Value, "sha256:"))
	if err := godigest.NewDigestFromEncoded(godigest.SHA256, hex).Validate(); err != nil {
		return "", fmt.Errorf("invalid provided digest %q: %w", provided.Value, err)
	}
	return hex, nil
}
