package checksum

import (
	"context"
	"fmt"
)

// SourceType mirrors the wget input spec's checksum source types without importing
// it, keeping this package free of the spec dependency.
type SourceType string

const (
	// SourceHTTPHeader reads the checksum from download response headers.
	SourceHTTPHeader SourceType = "httpHeader"
	// SourceExternalURL fetches the checksum from a sibling URL.
	SourceExternalURL SourceType = "externalUrl"
	// SourceStream computes the digest from the stream without external verification.
	SourceStream SourceType = "stream"
)

// Source is one resolved checksum strategy.
type Source struct {
	Type SourceType
	// Headers are extra response header names to inspect (httpHeader).
	Headers []string
	// URLTemplate builds the checksum URL (externalUrl); empty uses the default.
	URLTemplate string
	// Algorithms restricts which algorithms the source considers, strongest first.
	// Empty means all supported algorithms.
	Algorithms []Algorithm
}

// OnMissing controls behaviour when no source yields an expected checksum.
type OnMissing string

const (
	// Fail aborts when no source yields a checksum. Default for a configured policy.
	Fail OnMissing = "fail"
	// Compute falls back to computing the digest from the stream.
	Compute OnMissing = "compute"
)

// Policy is the resolved checksum policy: an ordered source list and the
// behaviour when none yields a checksum.
type Policy struct {
	Sources   []Source
	OnMissing OnMissing
}

// Input carries what a policy needs from a completed download to resolve and
// verify an expected checksum.
type Input struct {
	// URL is the artifact URL, used to build external checksum URLs.
	URL string
	// Headers are the download response headers.
	Headers map[string][]string
	// Computed maps algorithm OCM names to the hex digest computed over the
	// downloaded bytes. It MUST contain every algorithm any source may verify
	// against; the caller pre-computes them during the download.
	Computed map[string]string
	// Fetch retrieves an external checksum. When nil, externalUrl sources are
	// skipped. Typically backed by [ExternalFetcher.Fetch].
	Fetch func(ctx context.Context, baseURL string, algs []Algorithm) (Expected, bool, error)
}

// Resolve applies the policy against a completed download: it walks the sources
// in order and, for the first that yields an expected checksum, verifies the
// downloaded bytes against it. It returns the verified expected checksum.
//
// Semantics:
//   - A source that yields a checksum whose value mismatches the computed digest
//     is a hard error (integrity failure).
//   - A stream source (or Compute-on-missing) yields no expected checksum: ok is
//     false and err is nil, signalling "compute and store without verification".
//   - When no source yields a checksum and OnMissing is Fail, an error is returned.
func Resolve(ctx context.Context, policy Policy, in Input) (expected Expected, verified bool, err error) {
	for _, src := range policy.Sources {
		switch src.Type {
		case SourceStream:
			// Explicit "trust the stream": stop here, no verification.
			return Expected{}, false, nil
		case SourceHTTPHeader:
			candidates := FromHeaders(in.Headers, src.Headers)
			if exp, ok := Select(candidates, src.Algorithms); ok {
				if verr := Verify(in.Computed, exp); verr != nil {
					return Expected{}, false, verr
				}
				return exp, true, nil
			}
		case SourceExternalURL:
			if in.Fetch == nil {
				continue
			}
			algs := src.Algorithms
			if len(algs) == 0 {
				algs = All
			}
			exp, ok, ferr := in.Fetch(ctx, in.URL, algs)
			if ferr != nil {
				return Expected{}, false, ferr
			}
			if ok {
				if verr := Verify(in.Computed, exp); verr != nil {
					return Expected{}, false, verr
				}
				return exp, true, nil
			}
		default:
			return Expected{}, false, fmt.Errorf("unsupported checksum source type %q", src.Type)
		}
	}

	// No source produced an expected checksum.
	if policy.OnMissing == Compute {
		return Expected{}, false, nil
	}
	return Expected{}, false, fmt.Errorf("no checksum could be obtained from any configured source and onMissing is %q", orFail(policy.OnMissing))
}

// RequiredAlgorithms returns the set of algorithms the policy may verify against,
// so the caller knows which digests to compute during the download. SHA-256 (the
// storage algorithm) is always included.
func RequiredAlgorithms(policy Policy) []Algorithm {
	seen := map[string]struct{}{StorageAlgorithm.OCMName: {}}
	out := []Algorithm{StorageAlgorithm}
	add := func(a Algorithm) {
		if _, dup := seen[a.OCMName]; dup {
			return
		}
		seen[a.OCMName] = struct{}{}
		out = append(out, a)
	}
	for _, src := range policy.Sources {
		algs := src.Algorithms
		if len(algs) == 0 {
			algs = All
		}
		for _, a := range algs {
			add(a)
		}
	}
	return out
}

func orFail(m OnMissing) OnMissing {
	if m == "" {
		return Fail
	}
	return m
}
