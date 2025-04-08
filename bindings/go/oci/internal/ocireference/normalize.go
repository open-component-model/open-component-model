package ocireference

import (
	"errors"
	"fmt"
	"strings"

	"github.com/opencontainers/go-digest"
)

var ctfDomain = "ctf.ocm.software"

// normalizedNamed represents a name which has been
// normalized and has a familiar form. A familiar name
// is what is used in Docker UI. An example normalized
// name is "CTF/ubuntu" and corresponding
// familiar name of "ubuntu".
type normalizedNamed interface {
	Named
	Familiar() Named
}

// ParseCTFNormalizedNamed parses a string into a named reference
// transforming a familiar name from Docker UI to a fully
// qualified reference. If the value may be an identifier
// use ParseAnyReference.
func ParseCTFNormalizedNamed(s string) (Named, error) {
	if ok := anchoredIdentifierRegexp.MatchString(s); ok {
		return nil, fmt.Errorf("invalid repository name (%s), cannot specify 64-byte hexadecimal strings", s)
	}
	domain, remainder := splitCTFDomain(s)
	var remoteName string
	if tagSep := strings.IndexRune(remainder, ':'); tagSep > -1 {
		remoteName = remainder[:tagSep]
	} else {
		remoteName = remainder
	}
	if strings.ToLower(remoteName) != remoteName {
		return nil, errors.New("invalid reference format: repository name must be lowercase")
	}

	ref, err := Parse(domain + "/" + remainder)
	if err != nil {
		return nil, err
	}
	named, isNamed := ref.(Named)
	if !isNamed {
		return nil, fmt.Errorf("reference %s has no name", ref.String())
	}
	return named, nil
}

// splitCTFDomain splits a repository name to domain and remotename string.
// If no valid domain is found, the default domain is used. Repository name
// needs to be already validated before.
func splitCTFDomain(name string) (domain, remainder string) {
	i := strings.IndexRune(name, '/')
	if i == -1 || (!strings.ContainsAny(name[:i], ".:") && name[:i] != "localhost") {
		domain, remainder = ctfDomain, name
	} else {
		domain, remainder = name[:i], name[i+1:]
	}
	return
}

func familiarizeName(named namedRepository) repository {
	repo := repository{
		domain: named.Domain(),
		path:   named.Path(),
	}

	if repo.domain == ctfDomain {
		repo.domain = ""
	}
	return repo
}

func (r reference) Familiar() Named {
	return reference{
		namedRepository: familiarizeName(r.namedRepository),
		tag:             r.tag,
		digest:          r.digest,
	}
}

func (r repository) Familiar() Named {
	return familiarizeName(r)
}

func (t taggedReference) Familiar() Named {
	return taggedReference{
		namedRepository: familiarizeName(t.namedRepository),
		tag:             t.tag,
	}
}

func (c canonicalReference) Familiar() Named {
	return canonicalReference{
		namedRepository: familiarizeName(c.namedRepository),
		digest:          c.digest,
	}
}

// ParseAnyReference parses a reference string as a possible identifier,
// full digest, or familiar name.
func ParseAnyReference(ref string) (Reference, error) {
	if ok := anchoredIdentifierRegexp.MatchString(ref); ok {
		return digestReference("sha256:" + ref), nil
	}
	if dgst, err := digest.Parse(ref); err == nil {
		return digestReference(dgst), nil
	}

	return ParseCTFNormalizedNamed(ref)
}
