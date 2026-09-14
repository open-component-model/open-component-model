package endpoint

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
)

const (
	protocolHTTP  = "http"
	protocolHTTPS = "https"
	protocolSSH   = "ssh"
	protocolGit   = "git"
	protocolFile  = "file"
)

// Parse parses a git repository URL and rejects forms the download cannot use.
//
// Supported are http(s)://, ssh:// and git:// URLs with a host and a path, the
// scp-like form user@host:path, and local repositories as file:///path,
// file://localhost/path or a plain path. A string that has no scheme and is not
// scp-like is a local path, relative to the working directory unless absolute.
func Parse(repository string) (*transport.Endpoint, error) {
	if strings.TrimSpace(repository) == "" {
		return nil, fmt.Errorf("repository must not be empty")
	}

	ep, err := transport.NewEndpoint(repository)
	if err != nil {
		return nil, fmt.Errorf("invalid git repository URL")
	}

	ep.Protocol = strings.ToLower(ep.Protocol)
	switch ep.Protocol {
	case protocolHTTP, protocolHTTPS, protocolSSH, protocolGit:
		if ep.Host == "" || ep.Path == "" || ep.Path == "/" {
			return nil, fmt.Errorf("git repository URL requires a hostname and path")
		}

		if ep.Port < 0 || ep.Port > 65535 {
			return nil, fmt.Errorf("invalid git repository port")
		}
	case protocolFile:
		// file:///path and plain paths have no host, file://localhost/path names it.
		if ep.Host != "" && ep.Host != "localhost" {
			return nil, fmt.Errorf("file repository URL must refer to the local host")
		}

		if ep.Path == "" {
			return nil, fmt.Errorf("file repository path must not be empty")
		}
	default:
		return nil, fmt.Errorf("unsupported git transport %q", ep.Protocol)
	}

	return ep, nil
}

func Hostname(ep *transport.Endpoint) string {
	return strings.ToLower(strings.Trim(ep.Host, "[]"))
}

func Port(ep *transport.Endpoint) string {
	if ep.Port != 0 {
		return strconv.Itoa(ep.Port)
	}

	switch ep.Protocol {
	case protocolHTTP:
		return "80"
	case protocolHTTPS:
		return "443"
	case protocolSSH:
		return "22"
	case protocolGit:
		return "9418"
	default:
		return ""
	}
}
