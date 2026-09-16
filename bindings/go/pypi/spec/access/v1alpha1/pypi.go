package v1alpha1

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// Type is the name of the PyPI access type. The canonical wire form is
// "pypi/v1alpha1".
const Type = "pypi"

// Distribution kinds selectable through the access spec.
const (
	// KindSdist selects source distributions (.tar.gz, .zip).
	KindSdist = "sdist"
	// KindWheel selects built distributions (.whl).
	KindWheel = "wheel"
)

// PyPI describes access to one or more distribution files of a PyPI project at
// a fixed version. The project is addressed through the Simple Repository API
// (PEP 503 HTML / PEP 691 JSON): its files are discovered from the project
// detail page rather than computed from coordinates, because PyPI file names
// carry build tags and platform tags that cannot be reconstructed from the
// project name and version alone.
//
// Distributions optionally narrows the files to the wanted subset by kind
// ("sdist"/"wheel") or by exact file name. When empty, every non-yanked file
// of the version is taken. The index supplies each file's download URL and its
// hashes inline, and the GPG signature (".asc"), when the index advertises it,
// is fetched next to the file. The result is always one application/x-tgz
// archive, even for a single file.
//
// PyPI versions are immutable released artifacts, so a version always names one
// fixed set of files; there is no LATEST or SNAPSHOT concept as in Maven.
//
// Credentials (username/password, a "__token__" API token, or a bearer token
// in identityToken) are supplied through the credential resolver and keyed by
// the "PyPIRepository" consumer identity.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type PyPI struct {
	// +ocm:jsonschema-gen:enum=pypi/v1alpha1,PyPI/v1alpha1
	Type runtime.Type `json:"type"`

	// IndexURL is the base URL of the Simple Repository API index
	// (e.g. https://pypi.org/simple).
	IndexURL string `json:"indexUrl"`
	// Project is the PyPI project (distribution) name (e.g. requests). It is
	// normalized before use, so "Foo.Bar" and "foo-bar" address the same page.
	Project string `json:"project"`
	// Version is the released project version (e.g. 2.32.3).
	Version string `json:"version"`
	// Distributions lists the files to access. When empty, every non-yanked
	// file of the version is taken.
	Distributions []Distribution `json:"distributions,omitempty"`
}

// Distribution selects a subset of a project version's files, either by kind
// or by exact file name.
type Distribution struct {
	// Kind selects files by distribution kind: "sdist" or "wheel". Ignored
	// when Filename is set. Empty (with an empty Filename) selects any file.
	Kind string `json:"kind,omitempty"`
	// Filename selects one file by its exact name (e.g.
	// requests-2.32.3-py3-none-any.whl). Takes precedence over Kind.
	Filename string `json:"filename,omitempty"`
}

var normalizeSeparators = regexp.MustCompile(`[-_.]+`)

// NormalizeProjectName applies the PEP 503 name normalization: lowercase and
// collapse every run of ".", "-" or "_" to a single "-". The Simple Repository
// API requires the normalized name in the project detail URL.
func NormalizeProjectName(name string) string {
	return normalizeSeparators.ReplaceAllString(strings.ToLower(name), "-")
}

// Validate checks that the coordinates are present and safe to place in a URL
// path, that IndexURL is an absolute URL, and that every Distributions entry is
// well-formed and unique.
func (p *PyPI) Validate() error {
	var errs []error
	for _, c := range []struct{ name, value string }{
		{"project", p.Project}, {"version", p.Version},
	} {
		if c.value == "" {
			errs = append(errs, fmt.Errorf("%s is required", c.name))
		} else if escapesPathSegment(c.value) {
			errs = append(errs, fmt.Errorf("%s %q must not contain path separators or \"..\"", c.name, c.value))
		}
	}
	if p.IndexURL == "" {
		errs = append(errs, errors.New("indexUrl is required"))
	} else if u, err := url.Parse(p.IndexURL); err != nil {
		errs = append(errs, fmt.Errorf("indexUrl is not a valid URL: %w", err))
	} else if u.Scheme == "" || u.Host == "" {
		errs = append(errs, fmt.Errorf("indexUrl %q must be an absolute URL with a scheme and a host", p.IndexURL))
	}
	errs = append(errs, validateDistributions(p.Distributions)...)
	return errors.Join(errs...)
}

// IsPinnedVersion reports whether Version names one fixed set of files. A PyPI
// release version is immutable, so this is always true. The method exists so a
// digest processor or a by-value transfer can apply the same pin check across
// access types without special-casing PyPI.
func (p *PyPI) IsPinnedVersion() bool {
	return true
}

func validateDistributions(distributions []Distribution) []error {
	var errs []error
	seen := make(map[Distribution]struct{}, len(distributions))
	for i, d := range distributions {
		if d.Filename == "" && d.Kind != "" && d.Kind != KindSdist && d.Kind != KindWheel {
			errs = append(errs, fmt.Errorf("distributions[%d]: kind %q must be %q or %q", i, d.Kind, KindSdist, KindWheel))
		}
		if d.Filename != "" && escapesPathSegment(d.Filename) {
			errs = append(errs, fmt.Errorf("distributions[%d]: filename %q must not contain path separators or \"..\"", i, d.Filename))
		}
		if _, dup := seen[d]; dup {
			errs = append(errs, fmt.Errorf("distributions[%d]: duplicate entry (kind %q, filename %q)", i, d.Kind, d.Filename))
		}
		seen[d] = struct{}{}
	}
	return errs
}

// escapesPathSegment reports whether v could leave the URL path segment it is
// placed in. The project name and each explicit file name end up in a URL path,
// and url.JoinPath resolves "..", so a value holding one would fetch from
// outside the project directory.
func escapesPathSegment(v string) bool {
	return strings.ContainsAny(v, `/\`) || strings.Contains(v, "..")
}
