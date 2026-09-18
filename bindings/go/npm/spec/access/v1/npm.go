package v1

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/Masterminds/semver/v3"

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

	// Registry is the base URL of the npm registry.
	Registry string `json:"registry"`

	// Package is the name of the npm package, optionally scoped (@scope/name).
	Package string `json:"package"`

	// Version is the exact version of the npm package. Version ranges and
	// dist-tags are not accepted.
	Version string `json:"version"`
}

// packageName is npm's "valid for old packages" rule, not the stricter rule for
// packages that can be published today. An access type has to be able to name
// what is already on a registry, and the registry still serves names that
// predate the current rule, such as the upper-case JSONStream. The character
// class is the set encodeURIComponent leaves untouched, which is what npm
// requires of a name it puts in a URL path.
var packageName = regexp.MustCompile(`^(@[A-Za-z0-9\-_.!~*'()]+/)?[A-Za-z0-9\-_.!~*'()]+$`)

// maxPackageNameLength is the npm registry limit for a full package name,
// including the scope.
const maxPackageNameLength = 214

// Validate verifies that registry, package and version are set, that the
// registry is an http(s) URL, and that the version pins one exact release.
func (n *NPM) Validate() error {
	if n == nil {
		return errors.New("npm access is required")
	}

	if n.Registry == "" {
		return errors.New("registry is required")
	}
	registry, err := url.Parse(n.Registry)
	if err != nil {
		return fmt.Errorf("invalid registry %q: %w", n.Registry, err)
	}
	if registry.Scheme != "http" && registry.Scheme != "https" {
		return fmt.Errorf("registry must use the http or https scheme, got %q", registry.Scheme)
	}
	if registry.Host == "" {
		return fmt.Errorf("registry %q has no host", n.Registry)
	}

	if n.Package == "" {
		return errors.New("package is required")
	}
	if len(n.Package) > maxPackageNameLength {
		return fmt.Errorf("package name must not be longer than %d characters", maxPackageNameLength)
	}
	// npm rejects these outright, for new and existing packages alike.
	if strings.HasPrefix(n.Package, ".") || strings.HasPrefix(n.Package, "_") {
		return fmt.Errorf("invalid package name %q: must not start with a period or an underscore", n.Package)
	}
	if !packageName.MatchString(n.Package) {
		return fmt.Errorf("invalid package name %q", n.Package)
	}

	if n.Version == "" {
		return errors.New("version is required")
	}
	// StrictNewVersion only accepts a complete version, so ranges ("^1.2.3")
	// and dist-tags ("latest") are rejected here.
	if _, err := semver.StrictNewVersion(n.Version); err != nil {
		return fmt.Errorf("version %q must be an exact semantic version, not a range or dist-tag: %w", n.Version, err)
	}

	return nil
}
