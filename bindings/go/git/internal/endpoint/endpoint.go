package endpoint

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/transport"
)

// Endpoint is a parsed git repository URL with a lowercase protocol and host.
type Endpoint struct {
	// URL is the repository URL to hand to go-git.
	URL      string
	Protocol string
	User     string
	Password string
	Host     string
	Port     int
	Path     string
	// Ref and Commit are selected by the URL fragment, as in #branch=main.
	Ref    string
	Commit string
}

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
//
// A fragment may select a ref or commit with branch=<name>, tag=<name> or
// commit=<sha>, as in https://host/org/repo.git#branch=main. It is removed from
// URL: go-git would append its request path to the fragment.
func Parse(repository string) (*Endpoint, error) {
	if strings.TrimSpace(repository) == "" {
		return nil, fmt.Errorf("repository must not be empty")
	}

	repository, fragment, _ := strings.Cut(repository, "#")

	u, err := parseURL(repository)
	if err != nil {
		return nil, fmt.Errorf("invalid git repository URL")
	}

	ep := &Endpoint{
		URL:      repository,
		Protocol: strings.ToLower(u.Scheme),
		Host:     strings.ToLower(u.Hostname()),
		Path:     u.Path,
	}
	if err := ep.parseFragment(fragment); err != nil {
		return nil, err
	}
	if u.User != nil {
		ep.User = u.User.Username()
		ep.Password, _ = u.User.Password()
	}
	if port := u.Port(); port != "" {
		if ep.Port, err = strconv.Atoi(port); err != nil {
			return nil, fmt.Errorf("invalid git repository port")
		}
	}

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

		if !filepath.IsAbs(ep.Path) {
			if ep.Path, err = filepath.Abs(ep.Path); err != nil {
				return nil, fmt.Errorf("cannot resolve file repository path: %w", err)
			}
		}
		ep.URL = "file://" + ep.Path
	default:
		return nil, fmt.Errorf("unsupported git transport %q", ep.Protocol)
	}

	return ep, nil
}

func (ep *Endpoint) parseFragment(fragment string) error {
	values, err := url.ParseQuery(fragment)
	if err != nil {
		return fmt.Errorf("invalid git repository URL fragment")
	}

	if values.Has("branch") && values.Has("tag") {
		return fmt.Errorf("git repository URL fragment must not set both branch and tag")
	}

	for key, value := range values {
		if len(value) != 1 || value[0] == "" {
			return fmt.Errorf("git repository URL fragment %q requires exactly one value", key)
		}

		switch key {
		case "branch":
			ep.Ref = plumbing.NewBranchReferenceName(value[0]).String()
		case "tag":
			ep.Ref = plumbing.NewTagReferenceName(value[0]).String()
		case "commit":
			ep.Commit = value[0]
		default:
			return fmt.Errorf("unsupported git repository URL fragment %q, expected branch, tag or commit", key)
		}
	}

	return nil
}

// Selectors merges ref and commit with the selectors of the URL fragment.
// Both may name the same ref or commit, but not different ones.
func (ep *Endpoint) Selectors(ref, commit string) (string, string, error) {
	if ep.Ref != "" {
		if ref != "" && ref != ep.Ref && plumbing.NewBranchReferenceName(ref).String() != ep.Ref &&
			plumbing.NewTagReferenceName(ref).String() != ep.Ref {
			return "", "", fmt.Errorf("git ref %q conflicts with %q selected by the repository URL", ref, ep.Ref)
		}
		ref = ep.Ref
	}

	if ep.Commit != "" {
		if commit != "" && !strings.EqualFold(commit, ep.Commit) {
			return "", "", fmt.Errorf("git commit %q conflicts with %q selected by the repository URL", commit, ep.Commit)
		}
		commit = ep.Commit
	}

	return ref, commit, nil
}

// parseURL keeps the host of file URLs: go-git reads everything after file://
// as the path, so file://localhost/srv/repo.git would become a relative path.
func parseURL(repository string) (*url.URL, error) {
	if u, err := url.Parse(repository); err == nil && strings.EqualFold(u.Scheme, protocolFile) {
		return u, nil
	}

	return transport.ParseURL(repository)
}

func Port(ep *Endpoint) string {
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
