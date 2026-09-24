package download

import (
	"fmt"
	"net/http"

	gitclient "github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

func init() {
	// Repository construction installs a client only when HTTPConfig is supplied.
	// Install the guarded default here so unconfigured downloads are also protected.
	InstallHTTPClient(nil)
}

// InstallHTTPClient installs a guarded client in go-git's process-global HTTP
// and HTTPS registrations. Nil uses the default HTTP transport.
func InstallHTTPClient(client *http.Client) {
	configured := http.Client{Transport: http.DefaultTransport}
	if client != nil {
		configured = *client
	}
	previous := configured.CheckRedirect
	configured.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		// go-git checks scheme changes after following the redirect, too late to
		// prevent net/http from forwarding credentials to HTTP on the same host.
		if req.URL.Scheme == "http" {
			for _, prior := range via {
				if prior.URL.Scheme == "https" {
					return fmt.Errorf("git HTTP redirect from HTTPS to HTTP is not allowed")
				}
			}
		}
		if previous != nil {
			return previous(req, via)
		}
		return nil
	}
	transport := githttp.NewClient(&configured)
	gitclient.InstallProtocol("http", transport)
	gitclient.InstallProtocol("https", transport)
}
