package runtime

import (
	"fmt"
	"hash/fnv"
	"maps"
	"net/url"
	"slices"
	"strings"
)

const (
	IdentityAttributeType     = "type"
	IdentityAttributeHostname = "hostname"
	IdentityAttributeScheme   = "scheme"
	IdentityAttributePath     = "path"
	IdentityAttributePort     = "port"
)

// Identity is a map that represents a set of attributes that uniquely identity
// arbitrary resources. It is used in various places in Open Component Model to uniquely
// identity objects such as resources or components.
type Identity map[string]string

var _ Typed = Identity{}

// Equal is a function that checks if two identities are equal.
// It compares the keys and values of both identities.
// It does not use CanonicalHashV1 to compare the identities, because a plain
// map comparison is sufficient for equality of 2 identities.
func (i Identity) Equal(o Identity) bool {
	return maps.Equal(i, o)
}

// IsContainedIn checks if the identity i is contained in the identity o.
// It checks if all keys and values of i are present in o.
func (i Identity) IsContainedIn(o Identity) bool {
	for k, v := range i {
		if ov, ok := o[k]; !ok || ov != v {
			return false
		}
	}
	return true
}

// Clone creates a deep copy of the identity.
func (i Identity) Clone() Identity {
	return maps.Clone(i)
}

// CanonicalHashV1 is a canonicalization of an identity that can be used to uniquely identity it.
// it is backed by a FNV hash that is stabilized through the order of the keys in order as defined by slices.Sorted.
// The hash is not cryptographically secure and should not be used for security purposes.
// It is only used to identify the identity in a stable way.
func (i Identity) CanonicalHashV1() uint64 {
	h := fnv.New64()
	for key := range slices.Values(slices.Sorted(maps.Keys(i))) {
		// fnv64 can never fail to write
		_, _ = h.Write([]byte(key + i[key]))
	}
	return h.Sum64()
}

// GetType extracts the type or panics if failing.
// It should only be used if the type is known to be present and valid.
// For more information, check ParseType.
func (i Identity) GetType() Type {
	typ, err := i.ParseType()
	if err != nil {
		panic(err)
	}
	return typ
}

// ParseType attempts to parse the type from the identity.
// It returns an error if the type is not present or invalid.
func (i Identity) ParseType() (Type, error) {
	val, ok := i[IdentityAttributeType]
	if !ok {
		return Type{}, fmt.Errorf("missing identity attribute %q", IdentityAttributeType)
	}
	typ, err := TypeFromString(val)
	if err != nil {
		return Type{}, fmt.Errorf("invalid identity type %q: %w", val, err)
	}
	return typ, nil
}

// ParseURLToIdentity attempts parses the provided URL string into an Identity.
// Incorporated Attributes are
// - IdentityAttributeScheme
// - IdentityAttributePort
// - IdentityAttributeHostname
// - IdentityAttributePath
func ParseURLToIdentity(url string) (Identity, error) {
	purl, err := ParseURLAndAllowNoScheme(url)
	if err != nil {
		return nil, err
	}
	identity := Identity{}
	if purl.Scheme != "" {
		identity[IdentityAttributeScheme] = purl.Scheme
	}
	if purl.Port() != "" {
		identity[IdentityAttributePort] = purl.Port()
	}
	if purl.Hostname() != "" {
		identity[IdentityAttributeHostname] = purl.Hostname()
	}
	if purl.Path != "" {
		identity[IdentityAttributePath] = strings.TrimPrefix(purl.Path, "/")
	}
	return identity, nil
}

// ParseURLAndAllowNoScheme parses the provided URL string into a URL struct.
// it is a special case for url.Parse that allows URLs without a scheme by temporarily
// inserting a dummy scheme while parsing.
func ParseURLAndAllowNoScheme(urlToParse string) (*url.URL, error) {
	const dummyScheme = "dummy"
	if !strings.Contains(urlToParse, "://") {
		urlToParse = dummyScheme + "://" + urlToParse
	}
	parsedURL, err := url.Parse(urlToParse)
	if err != nil {
		return nil, err
	}
	if parsedURL.Scheme == dummyScheme {
		parsedURL.Scheme = ""
	}
	return parsedURL, nil
}
