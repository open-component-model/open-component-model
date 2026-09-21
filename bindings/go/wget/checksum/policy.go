package checksum

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
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
	// Algorithms restricts which algorithms the source considers, strongest first.
	// Empty means all supported algorithms.
	Algorithms []Algorithm
	// URL is the absolute URL of the checksum resource for externalUrl sources.
	// Empty falls back to `<baseURL>.<alg.Extension>` (Maven's convention).
	// One URL per source: to cover multiple algorithms or hosts, add multiple
	// externalUrl sources. Kept as a plain string so this package does not
	// depend on any templating language.
	URL string
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
	// FetchURL retrieves the external checksum for a specific pre-resolved URL and
	// algorithm. When nil, externalUrl sources are skipped. Typically backed by
	// [ExternalFetcher.FetchURL].
	FetchURL func(ctx context.Context, checksumURL string, alg Algorithm) (Expected, bool, error)
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
	for i, src := range policy.Sources {
		switch src.Type {
		case SourceStream:
			// Explicit "trust the stream": stop here, no verification.
			slog.DebugContext(ctx, "checksum: source is stream — no verification",
				"url", in.URL, "index", i)
			return Expected{}, false, nil
		case SourceHTTPHeader:
			candidates := FromHeaders(in.Headers, src.Headers)
			if exp, ok := Select(candidates, src.Algorithms); ok {
				if verr := Verify(in.Computed, exp); verr != nil {
					return Expected{}, false, verr
				}
				return exp, true, nil
			}
			slog.DebugContext(ctx, "checksum: header source yielded no candidate",
				"url", in.URL, "index", i, "candidates", len(candidates))
		case SourceExternalURL:
			if in.FetchURL == nil {
				slog.DebugContext(ctx, "checksum: external-url source skipped (no fetcher)",
					"url", in.URL, "index", i)
				continue
			}
			algs := src.Algorithms
			if len(algs) == 0 {
				algs = All
			}
			exp, ok, ferr := fetchExternal(ctx, src, in, algs)
			if ferr != nil {
				return Expected{}, false, ferr
			}
			if ok {
				if verr := Verify(in.Computed, exp); verr != nil {
					return Expected{}, false, verr
				}
				return exp, true, nil
			}
			slog.DebugContext(ctx, "checksum: external-url source yielded no candidate",
				"url", in.URL, "index", i)
		default:
			return Expected{}, false, fmt.Errorf("unsupported checksum source type %q", src.Type)
		}
	}

	// No source produced an expected checksum.
	if policy.OnMissing == Compute {
		slog.DebugContext(ctx, "checksum: no source yielded a digest; onMissing=compute — no verification",
			"url", in.URL)
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

// ResolveAdvertised walks the policy's sources in order and returns the first
// [Expected] checksum a source advertises, without requiring a completed
// download. It is meant for callers that pin from what the source claims rather
// than compute from bytes — today, the Wget/v1 access-side digest processor.
//
// prefer restricts and orders the algorithms accepted from the sources: an
// advertised digest is returned only when its algorithm appears in prefer, and
// among competing offers the earliest match in prefer wins. Empty prefer means
// "any supported algorithm, strongest first" (see [All]).
//
// Semantics differ from [Resolve]:
//
//   - No [Input.Computed] map is consulted; no [Verify] is called.
//   - A [SourceStream] entry is treated as "no advertised digest here" — it
//     signals "trust the stream", which for pin-from-source means the caller
//     should fall back to downloading and computing.
//   - When no source yields a digest, ok is false; the caller decides whether
//     to fall back to download-and-hash or to fail (per OnMissing).
//
// probe is the transport into the completed response headers on the access
// side. When nil, only sources that do not require a HEAD/GET are consulted —
// today only externalUrl via [Input.FetchURL] and, if headers are already known,
// [Input.Headers].
func ResolveAdvertised(ctx context.Context, policy Policy, in Input, prefer []Algorithm) (Expected, bool, error) {
	if len(prefer) == 0 {
		prefer = All
	}
	for i, src := range policy.Sources {
		switch src.Type {
		case SourceStream:
			// Explicit "trust the stream" — the caller must download to
			// compute a digest.
			slog.DebugContext(ctx, "checksum: advertised source is stream — no advertised digest",
				"url", in.URL, "index", i)
			return Expected{}, false, nil
		case SourceHTTPHeader:
			candidates := FromHeaders(in.Headers, src.Headers)
			if exp, ok := Select(candidates, intersect(src.Algorithms, prefer)); ok {
				return exp, true, nil
			}
			slog.DebugContext(ctx, "checksum: advertised header source yielded nothing",
				"url", in.URL, "index", i, "candidates", len(candidates))
		case SourceExternalURL:
			if in.FetchURL == nil {
				slog.DebugContext(ctx, "checksum: advertised external-url source skipped (no fetcher)",
					"url", in.URL, "index", i)
				continue
			}
			algs := intersect(src.Algorithms, prefer)
			if len(algs) == 0 {
				slog.DebugContext(ctx, "checksum: advertised external-url source skipped (no accepted algorithms)",
					"url", in.URL, "index", i)
				continue
			}
			exp, ok, ferr := fetchExternal(ctx, src, in, algs)
			if ferr != nil {
				return Expected{}, false, ferr
			}
			if ok {
				return exp, true, nil
			}
			slog.DebugContext(ctx, "checksum: advertised external-url source yielded nothing",
				"url", in.URL, "index", i)
		default:
			return Expected{}, false, fmt.Errorf("unsupported checksum source type %q", src.Type)
		}
	}
	slog.DebugContext(ctx, "checksum: no source advertised a digest",
		"url", in.URL)
	return Expected{}, false, nil
}

// intersect returns the algorithms present in both a and prefer, in prefer's
// order. Empty a means "no restriction from the source", so prefer is returned
// as-is; empty prefer means "no restriction from the caller", so a is returned.
func intersect(a []Algorithm, prefer []Algorithm) []Algorithm {
	if len(a) == 0 {
		return prefer
	}
	if len(prefer) == 0 {
		return a
	}
	allow := make(map[string]struct{}, len(a))
	for _, x := range a {
		allow[x.OCMName] = struct{}{}
	}
	out := make([]Algorithm, 0, len(prefer))
	for _, p := range prefer {
		if _, ok := allow[p.OCMName]; ok {
			out = append(out, p)
		}
	}
	return out
}

// fetchExternal walks algs in order and returns the first external checksum that
// resolves for src. When src.URL is set the same URL is fetched for every
// algorithm (typically each source binds a single algorithm via Source.Algorithms);
// otherwise the Maven default `<baseURL>.<alg.Extension>` is used per algorithm.
func fetchExternal(ctx context.Context, src Source, in Input, algs []Algorithm) (Expected, bool, error) {
	for _, alg := range algs {
		u := resolveExternalURL(src, in.URL, alg)
		exp, ok, err := in.FetchURL(ctx, u, alg)
		if err != nil {
			return Expected{}, false, err
		}
		if ok {
			return exp, true, nil
		}
	}
	return Expected{}, false, nil
}

// resolveExternalURL returns the checksum URL for src at alg: the explicit
// [Source.URL] when set, otherwise the Maven default: alg.Extension appended
// to baseURL's path, preserving query and fragment. Falls back to naive string
// concatenation for unparseable URLs so callers never see an error here.
func resolveExternalURL(src Source, baseURL string, alg Algorithm) string {
	if src.URL != "" {
		return src.URL
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Path == "" && u.Opaque == "" && u.Host == "" {
		return baseURL + "." + alg.Extension
	}
	if u.Opaque != "" {
		u.Opaque += "." + alg.Extension
	} else {
		u.Path += "." + alg.Extension
	}
	return u.String()
}
