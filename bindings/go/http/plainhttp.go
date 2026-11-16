package http

import (
	"context"
	"fmt"
	"log/slog"

	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
)

// PlainHTTPFor decides whether host is reached over cleartext HTTP and logs
// the warning that decision carries.
//
// Only cfg turns plain HTTP on: the entry for host, else the global setting.
// fallback applies to hosts cfg does not decide. scheme is the prefix the
// caller saw on the URL, "" when there was none; it never changes the
// decision and is only checked against it. An http:// prefix on a host that
// stays on HTTPS is an error, because the request would silently go over
// HTTPS when the URL asked for cleartext. A plain HTTP host logs that the
// connection goes unencrypted, unless the URL asked for http:// itself.
//
// Callers add their own rules in front of this one: the OCI resolver deprecates
// every prefix, and a classic Helm repository URL keeps https:// whatever cfg
// says.
func PlainHTTPFor(ctx context.Context, cfg *httpv1alpha1.Config, host, scheme string, fallback bool) (bool, error) {
	plain := fallback
	if configured := cfg.ResolveHost(host).PlainHTTP; configured != nil {
		plain = *configured
	}
	switch {
	case scheme == "http" && !plain:
		return false, fmt.Errorf("the URL uses http://, but plain HTTP is not enabled for host %q. Enable plainHTTP for the host in the HTTP client configuration (see %s)", host, plainHTTPGuide)
	case plain && scheme != "http":
		slog.WarnContext(ctx, "plain HTTP is enabled for this host; the connection is downgraded to cleartext HTTP and requests, including credentials, are sent unencrypted",
			slog.String("host", host))
	}
	return plain, nil
}

// plainHTTPGuide shows where plainHTTP is set. Callers such as the Helm
// download pass the error on as it is, so the error itself must point there.
const plainHTTPGuide = "https://ocm.software/docs/guides/transfer/configure-http/#use-plain-http-for-a-local-registry"
