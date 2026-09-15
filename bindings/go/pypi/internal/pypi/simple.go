package pypi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"golang.org/x/net/html"

	"ocm.software/open-component-model/bindings/go/pypi/spec/access/v1alpha1"
	credsv1 "ocm.software/open-component-model/bindings/go/pypi/spec/credentials/v1"
)

// AcceptHeader negotiates the PEP 691 JSON serialization of the Simple
// Repository API, falling back to the PEP 503 HTML serialization and finally to
// text/html for the oldest indexes.
const AcceptHeader = "application/vnd.pypi.simple.v1+json, application/vnd.pypi.simple.v1+html;q=0.2, text/html;q=0.01"

// GPGSignatureSuffix is the only sibling file a PyPI index co-locates with a
// distribution: the detached PGP signature.
const GPGSignatureSuffix = ".asc"

// FileRef identifies one resolvable distribution file discovered from the
// project detail page.
type FileRef struct {
	// URL is the absolute download URL of the file.
	URL string
	// Filename is the distribution file name (used as the tar entry name).
	Filename string
	// Hashes maps a lower-cased hash name (e.g. "sha256") to a hex digest, as
	// advertised by the index. May be empty.
	Hashes map[string]string
	// HasGPGSig reports whether the index advertises a detached ".asc"
	// signature next to the file. When unknown (index did not say), it is
	// false and the signature is still probed optionally on download.
	HasGPGSig bool
	// Yanked reports whether the file has been yanked (PEP 592).
	Yanked bool
}

// ProjectDetailURL is the Simple Repository API project detail URL for project
// under indexURL: "<indexURL>/<normalized-project>/", with the trailing slash
// the API requires.
func ProjectDetailURL(indexURL, project string) (string, error) {
	u, err := url.Parse(indexURL)
	if err != nil {
		return "", fmt.Errorf("error parsing indexUrl %q: %w", indexURL, err)
	}
	joined := u.JoinPath(v1alpha1.NormalizeProjectName(project)).String()
	if !strings.HasSuffix(joined, "/") {
		joined += "/"
	}
	return joined, nil
}

// Resolve returns one FileRef per selected distribution file of the project
// version, in a deterministic order. It fetches the project detail page,
// parses the JSON (PEP 691) or HTML (PEP 503) serialization the index served,
// and applies the access spec's Distributions filter.
func (c *Client) Resolve(ctx context.Context, p *v1alpha1.PyPI, creds *credsv1.PyPICredentials) ([]FileRef, error) {
	detailURL, err := ProjectDetailURL(p.IndexURL, p.Project)
	if err != nil {
		return nil, err
	}
	resp, err := c.Get(ctx, detailURL, AcceptHeader, creds)
	if err != nil {
		return nil, fmt.Errorf("error fetching project page %q: %w", detailURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("project %q not found at %q", p.Project, detailURL)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error fetching project page %q: unexpected status %d", detailURL, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading project page %q: %w", detailURL, err)
	}

	var all []FileRef
	if isJSON(resp.Header.Get("Content-Type")) {
		all, err = parseJSON(body, detailURL)
	} else {
		all, err = parseHTML(body, detailURL)
	}
	if err != nil {
		return nil, fmt.Errorf("error parsing project page %q: %w", detailURL, err)
	}
	return selectFiles(p, all)
}

// isJSON reports whether the Content-Type names the PEP 691 JSON serialization
// (or plain application/json).
func isJSON(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.Contains(ct, "json")
}

// jsonProjectDetail is the PEP 691 project detail response (only the fields we
// consume).
type jsonProjectDetail struct {
	Files []jsonFile `json:"files"`
}

type jsonFile struct {
	Filename string            `json:"filename"`
	URL      string            `json:"url"`
	Hashes   map[string]string `json:"hashes"`
	GPGSig   *bool             `json:"gpg-sig"`
	Yanked   json.RawMessage   `json:"yanked"`
}

// parseJSON decodes a PEP 691 project detail page. Relative file URLs are
// resolved against base (the project detail URL).
func parseJSON(body []byte, base string) ([]FileRef, error) {
	var detail jsonProjectDetail
	if err := json.Unmarshal(body, &detail); err != nil {
		return nil, fmt.Errorf("invalid JSON simple index response: %w", err)
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	refs := make([]FileRef, 0, len(detail.Files))
	for _, f := range detail.Files {
		if f.Filename == "" || f.URL == "" {
			continue
		}
		abs, err := resolveURL(baseURL, f.URL)
		if err != nil {
			return nil, err
		}
		ref := FileRef{
			URL:      abs,
			Filename: f.Filename,
			Hashes:   lowerHashes(f.Hashes),
			Yanked:   truthyYanked(f.Yanked),
		}
		if f.GPGSig != nil {
			ref.HasGPGSig = *f.GPGSig
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

// truthyYanked interprets the PEP 592 "yanked" field, which may be a boolean or
// a non-empty reason string.
func truthyYanked(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return b
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s != ""
	}
	return false
}

func lowerHashes(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[strings.ToLower(k)] = v
	}
	return out
}

// parseHTML decodes a PEP 503 project detail page: one anchor per file, the
// text being the file name, the href the download URL with an optional
// "#<hash>=<value>" fragment, and optional data-gpg-sig / data-yanked
// attributes.
func parseHTML(body []byte, base string) ([]FileRef, error) {
	baseURL, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("invalid HTML simple index response: %w", err)
	}
	var refs []FileRef
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			if ref, ok := anchorToFileRef(n, baseURL); ok {
				refs = append(refs, ref)
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return refs, nil
}

// anchorToFileRef turns one <a> element into a FileRef. ok is false when the
// anchor has no href or no filename text.
func anchorToFileRef(n *html.Node, baseURL *url.URL) (FileRef, bool) {
	var href, gpgSig, dataYanked string
	hasYanked := false
	for _, attr := range n.Attr {
		switch strings.ToLower(attr.Key) {
		case "href":
			href = attr.Val
		case "data-gpg-sig":
			gpgSig = attr.Val
		case "data-yanked":
			dataYanked = attr.Val
			hasYanked = true
		}
	}
	filename := strings.TrimSpace(anchorText(n))
	if href == "" || filename == "" {
		return FileRef{}, false
	}
	rawURL, hashes := splitURLFragmentHash(href)
	abs, err := resolveURL(baseURL, rawURL)
	if err != nil {
		return FileRef{}, false
	}
	_ = dataYanked // presence, not value, decides yanked
	return FileRef{
		URL:       abs,
		Filename:  filename,
		Hashes:    hashes,
		HasGPGSig: strings.EqualFold(gpgSig, "true"),
		Yanked:    hasYanked,
	}, true
}

func anchorText(n *html.Node) string {
	var sb strings.Builder
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.TextNode {
			sb.WriteString(child.Data)
		}
	}
	return sb.String()
}

// splitURLFragmentHash splits an "url#<hashname>=<hashvalue>" href into the URL
// and a single-entry hash map (empty when no fragment hash is present).
func splitURLFragmentHash(href string) (string, map[string]string) {
	idx := strings.LastIndex(href, "#")
	if idx < 0 {
		return href, nil
	}
	rawURL, frag := href[:idx], href[idx+1:]
	name, value, ok := strings.Cut(frag, "=")
	if !ok || name == "" || value == "" {
		return rawURL, nil
	}
	return rawURL, map[string]string{strings.ToLower(name): value}
}

// resolveURL resolves ref against base (relative URLs are allowed by the spec).
func resolveURL(base *url.URL, ref string) (string, error) {
	u, err := url.Parse(ref)
	if err != nil {
		return "", fmt.Errorf("invalid file URL %q: %w", ref, err)
	}
	return base.ResolveReference(u).String(), nil
}

// selectFiles applies the access spec's version and Distributions filter to the
// files discovered from the index, returning them in a deterministic order.
func selectFiles(p *v1alpha1.PyPI, files []FileRef) ([]FileRef, error) {
	byName := make(map[string]FileRef, len(files))
	for _, f := range files {
		byName[f.Filename] = f
	}

	// versionMatches holds every file (indexed) whose filename parses to the
	// requested version, regardless of yank state.
	versionMatches := make([]FileRef, 0, len(files))
	for _, f := range files {
		if v, ok := versionFromFilename(f.Filename); ok && v == p.Version {
			versionMatches = append(versionMatches, f)
		}
	}

	if len(p.Distributions) == 0 {
		selected := make([]FileRef, 0, len(versionMatches))
		for _, f := range versionMatches {
			if f.Yanked {
				continue
			}
			selected = append(selected, f)
		}
		if len(selected) == 0 {
			return nil, fmt.Errorf("no files found for %s version %q", p.Project, p.Version)
		}
		sortByFilename(selected)
		return selected, nil
	}

	var selected []FileRef
	seen := make(map[string]struct{})
	add := func(f FileRef) {
		if _, ok := seen[f.Filename]; ok {
			return
		}
		seen[f.Filename] = struct{}{}
		selected = append(selected, f)
	}
	for i, d := range p.Distributions {
		switch {
		case d.Filename != "":
			// An explicitly named file is taken even if yanked. It must exist
			// on the project page; when its name parses to a version, that
			// version must be the requested one, so a typo naming another
			// release is caught while an unusually-named file the user picked
			// on purpose is still honoured.
			f, ok := byName[d.Filename]
			if !ok {
				return nil, fmt.Errorf("distributions[%d]: file %q not found for %s version %q", i, d.Filename, p.Project, p.Version)
			}
			if v, ok := versionFromFilename(f.Filename); ok && v != p.Version {
				return nil, fmt.Errorf("distributions[%d]: file %q is not version %q", i, d.Filename, p.Version)
			}
			add(f)
		default:
			matches := filterByKind(versionMatches, d.Kind)
			if len(matches) == 0 {
				kind := d.Kind
				if kind == "" {
					kind = "any"
				}
				return nil, fmt.Errorf("distributions[%d]: no %s files found for %s version %q", i, kind, p.Project, p.Version)
			}
			sortByFilename(matches)
			for _, f := range matches {
				add(f)
			}
		}
	}
	return selected, nil
}

// filterByKind keeps the non-yanked files matching kind (empty kind = any).
func filterByKind(files []FileRef, kind string) []FileRef {
	var out []FileRef
	for _, f := range files {
		if f.Yanked {
			continue
		}
		if kind != "" && fileKind(f.Filename) != kind {
			continue
		}
		out = append(out, f)
	}
	return out
}

func sortByFilename(files []FileRef) {
	sort.Slice(files, func(i, j int) bool { return files[i].Filename < files[j].Filename })
}

// fileKind classifies a distribution file by its extension.
func fileKind(filename string) string {
	switch {
	case strings.HasSuffix(filename, ".whl"):
		return v1alpha1.KindWheel
	case strings.HasSuffix(filename, ".tar.gz"), strings.HasSuffix(filename, ".zip"):
		return v1alpha1.KindSdist
	default:
		return ""
	}
}

// versionFromFilename extracts the project version from a wheel or sdist file
// name. ok is false for a file name of an unknown kind.
//
// Wheel: "{distribution}-{version}(-{build tag})?-{python}-{abi}-{platform}.whl"
// so the version is the second "-"-separated field.
// Sdist: "{name}-{version}.tar.gz" / ".zip"; the version is the tail after the
// last "-", which for a released sdist is the version.
func versionFromFilename(filename string) (string, bool) {
	switch {
	case strings.HasSuffix(filename, ".whl"):
		stem := strings.TrimSuffix(filename, ".whl")
		parts := strings.Split(stem, "-")
		if len(parts) < 5 {
			return "", false
		}
		return parts[1], true
	case strings.HasSuffix(filename, ".tar.gz"):
		return sdistVersion(strings.TrimSuffix(filename, ".tar.gz"))
	case strings.HasSuffix(filename, ".zip"):
		return sdistVersion(strings.TrimSuffix(filename, ".zip"))
	default:
		return "", false
	}
}

// sdistVersion returns the version from an sdist stem "{name}-{version}". The
// name may itself contain "-", so the version is the substring after the last
// "-".
func sdistVersion(stem string) (string, bool) {
	idx := strings.LastIndex(stem, "-")
	if idx < 0 || idx == len(stem)-1 {
		return "", false
	}
	return stem[idx+1:], true
}
