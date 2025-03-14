package v1

import (
	"fmt"
	"hash/fnv"
	"maps"
	"net/url"
	"slices"
	"strings"

	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	IdentityAttributeType     = "type"
	IdentityAttributeHostname = "hostname"
	IdentityAttributeScheme   = "scheme"
	IdentityAttributePath     = "path"
	IdentityAttributePort     = "port"
)

type Identity map[string]string

func (i Identity) Equal(o Identity) bool {
	return maps.Equal(i, o)
}
func (i Identity) IsContainedIn(o Identity) bool {
	for k, v := range i {
		if ov, ok := o[k]; !ok || ov != v {
			return false
		}
	}
	return true
}

func (i Identity) Clone() Identity {
	return maps.Clone(i)
}

// CanonicalHashV1 is a canonicalization of an identity that can be used to uniquely identity it.
// it is backed by a FNV hash that is stabilized through the order of the keys.
func (i Identity) CanonicalHashV1() (uint64, error) {
	h := fnv.New64()
	for key := range slices.Values(slices.Sorted(maps.Keys(i))) {
		if _, err := h.Write([]byte(key + i[key])); err != nil {
			return 0, err
		}
	}
	return h.Sum64(), nil
}

// GetType extracts the required identity type attribute.
func (i Identity) GetType() (runtime.Type, error) {
	val, ok := i[IdentityAttributeType]
	if !ok {
		return runtime.Type{}, fmt.Errorf("missing identity attribute %q", IdentityAttributeType)
	}

	return runtime.NewUngroupedUnversionedType(val), nil
}

// ParseURLToIdentity attempts parses the provided URL string into an Identity.
// Incorporated Attributes are
// - IdentityAttributeScheme
// - IdentityAttributePort
// - IdentityAttributeHostname
// - IdentityAttributePath
func ParseURLToIdentity(url string) (Identity, error) {
	purl, err := parseURL(url)
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

func parseURL(urlToParse string) (*url.URL, error) {
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
