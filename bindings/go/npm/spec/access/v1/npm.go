package v1

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// NPM describes the access for a package in an npm registry.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type NPM struct {
	// +ocm:jsonschema-gen:enum=NPM/v1
	// +ocm:jsonschema-gen:enum:deprecated=npm,npm/v1,NPM
	Type runtime.Type `json:"type"`

	// Registry is the HTTP(S) base URL of the npm registry or a file:// path.
	// For file registries, the literal file:// prefix is stripped and the
	// remaining absolute or relative path is used unchanged.
	Registry string `json:"registry"`

	// Package is the name of the npm package, optionally scoped (@scope/name).
	Package string `json:"package"`

	// Version identifies the npm package version. Any nonempty value is
	// accepted for legacy compatibility, including dist-tags and version ranges.
	Version string `json:"version"`
}

// Validate verifies that registry, package and version are set and that the
// registry is an HTTP(S) URL with a host or a file:// path. Package and version
// values are preserved without enforcing npm publication or semantic version rules.
func (n *NPM) Validate() error {
	if n == nil {
		return errors.New("npm access is required")
	}

	if n.Registry == "" {
		return errors.New("registry is required")
	}
	if strings.HasPrefix(n.Registry, "file://") {
		// Legacy file registries are literal paths, not parsed file URLs.
		if strings.TrimPrefix(n.Registry, "file://") == "" {
			return errors.New("file registry path is required")
		}
	} else {
		registry, err := url.Parse(n.Registry)
		if err != nil {
			return fmt.Errorf("invalid registry %q: %w", n.Registry, err)
		}
		if registry.Scheme != "http" && registry.Scheme != "https" {
			return fmt.Errorf("registry must use the http, https or file:// scheme, got %q", registry.Scheme)
		}
		if registry.Host == "" {
			return fmt.Errorf("registry %q has no host", n.Registry)
		}
	}

	if n.Package == "" {
		return errors.New("package is required")
	}

	if n.Version == "" {
		return errors.New("version is required")
	}

	return nil
}
