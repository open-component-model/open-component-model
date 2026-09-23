package input

import (
	"context"
	"fmt"
	nethttp "net/http"
	"net/url"
	"strings"

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
	identityv1 "ocm.software/open-component-model/bindings/go/wget/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/wget/spec/input"
	v1 "ocm.software/open-component-model/bindings/go/wget/spec/input/v1"
)

// genericBlobDigestV1 is the OCM normalisation algorithm recorded for a plain
// downloaded blob.
const genericBlobDigestV1 = "genericBlobDigest/v1"

var _ constructor.ResourceInputMethod = (*InputMethod)(nil)

// InputMethod implements [constructor.ResourceInputMethod] for wget-based
// inputs: it downloads a resource from an HTTP/S URL declared in the component
// constructor and returns it as a local blob.
type InputMethod struct {
	// HTTPConfig configures the HTTP client. When nil, a default client is used.
	HTTPConfig *httpv1alpha1.Config
	// ChecksumConfig steers the [checksumhttpv1alpha1.ChecksumMode] applied to
	// each downloaded resource. When nil (or when no entry matches the URL),
	// the digest is computed from the stream without verification.
	ChecksumConfig *checksumhttpv1alpha1.Config
	// MaxDownloadSize limits response body bytes; zero uses the download
	// package default, negative disables the limit.
	MaxDownloadSize int64
	// TempFolder is the directory the downloaded body is streamed into. Empty
	// uses the OS temp directory. The backing file outlives ProcessResource
	// because it holds the local-blob content; see [download.Blob].
	TempFolder string
}

func (i *InputMethod) GetInputMethodScheme() *runtime.Scheme {
	return input.Scheme
}

// GetResourceCredentialConsumerIdentity resolves the credential consumer
// identity from the wget URL, using the same consumer type as the access type
// so credentials configured for a host resolve for both.
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

// ProcessResource downloads the resource described by the wget input
// specification and returns it as a local blob.
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

	// Verification behaviour is a deployment concern: the input spec carries
	// no checksum mode. Operators configure it centrally via
	// checksum.http.config.ocm.software/v1alpha1.
	policy, hasPolicy := checksumPolicyForMode(i.ChecksumConfig.ModeForURL(wget.URL))

	opts := []download.Option{
		download.WithClient(client),
		download.WithCredentials(credentials),
		download.WithTempDir(i.TempFolder),
	}
	if i.MaxDownloadSize != 0 {
		opts = append(opts, download.WithMaxDownloadSize(i.MaxDownloadSize))
	}
	// Compute digests during the download iff a policy needs them or the
	// resource carries a provided digest to verify against.
	provided := resource.Digest
	needsDigest := hasPolicy || provided != nil
	if needsDigest {
		opts = append(opts, download.WithDigestAlgorithms(digestAlgorithms(policy)...))
	}

	data, err := download.Download(ctx, download.Request{
		URL:        wget.URL,
		MediaType:  wget.MediaType,
		Header:     wget.Header,
		Verb:       wget.Verb,
		Body:       wget.Body,
		NoRedirect: wget.NoRedirect,
	}, opts...)
	if err != nil {
		return nil, fmt.Errorf("error downloading wget input from %q: %w", wget.URL, err)
	}

	// A provided digest is verified against the downloaded bytes independently
	// of any policy: both check the actual bytes, never each other.
	if provided != nil {
		if err := verifyProvidedDigest(provided, data); err != nil {
			_ = data.Close()
			return nil, fmt.Errorf("provided digest verification failed for wget input from %q: %w", wget.URL, err)
		}
	}

	if hasPolicy {
		if err := httpverify.Verify(ctx, client, credentials, wget.URL, policy, data); err != nil {
			_ = data.Close()
			return nil, fmt.Errorf("checksum verification failed for wget input from %q: %w", wget.URL, err)
		}
	}

	// Record SHA-256 as the blob's precalculated digest so the resource is
	// stored with SHA-256/genericBlobDigest regardless of which algorithm
	// verified it.
	if needsDigest {
		if sha, ok := data.Digests()[checksum.StorageAlgorithm.OCMName]; ok && sha != "" {
			data.SetPrecalculatedDigest("sha256:" + sha)
		}
	}

	return &constructor.ResourceInputMethodResult{
		ProcessedBlobData: data,
	}, nil
}

func (i *InputMethod) GetCredentialTypeScheme() *runtime.Scheme {
	return wgetcreds.Scheme
}

// verifyProvidedDigest checks the computed content digest against a digest
// pinned on the resource. Only SHA-256/genericBlobDigest is supported.
func verifyProvidedDigest(provided *constructorruntime.Digest, data *download.Blob) error {
	if provided.HashAlgorithm != "" && !strings.EqualFold(provided.HashAlgorithm, checksum.StorageAlgorithm.OCMName) {
		return fmt.Errorf("unsupported provided hash algorithm %q: only %s is supported", provided.HashAlgorithm, checksum.StorageAlgorithm.OCMName)
	}
	if provided.NormalisationAlgorithm != "" && provided.NormalisationAlgorithm != genericBlobDigestV1 {
		return fmt.Errorf("unsupported provided normalisation algorithm %q: only %s is supported", provided.NormalisationAlgorithm, genericBlobDigestV1)
	}
	computed, ok := data.Digests()[checksum.StorageAlgorithm.OCMName]
	if !ok || computed == "" {
		return fmt.Errorf("no computed %s digest available to verify the provided digest against", checksum.StorageAlgorithm.OCMName)
	}
	// The provided value may be bare hex or go-digest form (sha256:<hex>).
	want := strings.TrimPrefix(provided.Value, "sha256:")
	if !strings.EqualFold(want, computed) {
		return fmt.Errorf("digest mismatch: expected %s, computed %s", want, computed)
	}
	return nil
}

// checksumPolicyForMode adapts a wire [checksumhttpv1alpha1.ChecksumMode] to
// the checksum package's Policy for the input side. The peek modes verify the
// downloaded bytes against the built-in source set; Compute and Disable skip
// verification entirely (ok=false) — the input method still records SHA-256
// because a local blob's identity is its bytes.
func checksumPolicyForMode(mode checksumhttpv1alpha1.ChecksumMode) (checksum.Policy, bool) {
	switch mode {
	case checksumhttpv1alpha1.ChecksumModePeekWithHEADOrFail:
		return checksum.Policy{Sources: checksum.BuiltinSources(), OnMissing: checksum.Fail}, true
	case checksumhttpv1alpha1.ChecksumModePeekWithHEADOrCompute:
		return checksum.Policy{Sources: checksum.BuiltinSources(), OnMissing: checksum.Compute}, true
	default: // Compute, Disable
		return checksum.Policy{}, false
	}
}

// digestAlgorithms maps a policy's required algorithms to download digest
// options keyed by OCM name, so the download computes them all in one pass.
func digestAlgorithms(policy checksum.Policy) []download.DigestAlgorithm {
	required := checksum.RequiredAlgorithms(policy)
	out := make([]download.DigestAlgorithm, 0, len(required))
	for _, alg := range required {
		out = append(out, download.DigestAlgorithm{
			Name: alg.OCMName,
			New:  alg.New,
		})
	}
	return out
}
