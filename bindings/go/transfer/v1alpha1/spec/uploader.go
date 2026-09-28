package spec

import (
	"fmt"
	"iter"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// HTTPUploaderConfigType is the config type that routes matching resources through
// the HTTP streaming upload transformer. The config type itself selects the target
// transformer, so there is no nested transformer sub-type to specify.
const HTTPUploaderConfigType = "http.uploader.transfer.config.ocm.software"

func init() {
	Scheme.MustRegisterWithAlias(&HTTPUploaderConfig{},
		runtime.NewVersionedType(HTTPUploaderConfigType, Version),
		runtime.NewUnversionedType(HTTPUploaderConfigType),
	)
}

// UploaderConfig is the common contract implemented by every uploader configuration
// type. An uploader is a declarative rule that reroutes a matched resource through a
// custom upload target during transfer instead of the default download → local-blob
// path. Concrete types ([HTTPUploaderConfig], [OCIUploaderConfig]) carry the
// target-specific request fields inline; the config type itself selects the target
// transformer.
//
// All uploader configurations are extracted from the central generic config with
// [LookupUploaderConfigs], which preserves declaration order: the first uploader that
// matches a resource and applies to it wins.
type UploaderConfig interface {
	runtime.Typed
	// GetMatch returns the rule selecting the resources this uploader applies to.
	GetMatch() UploaderMatch
	// Validate reports whether the configuration is well-formed.
	Validate() error
}

// TypeResolver lists the aliases of an access type (ociArtifact, OCIImage, OCIImage/v1 are
// aliases of one type), so uploader rules match whichever alias a descriptor uses.
// [runtime.Scheme] implements it.
type TypeResolver interface {
	// ResolveCanonicalType returns the canonical type of typ or one of its aliases.
	ResolveCanonicalType(typ runtime.Type) (canonical runtime.Type, ok bool)
	// AliasesIter yields all aliases of a canonical type.
	AliasesIter(canonical runtime.Type) iter.Seq[runtime.Type]
}

// HTTPUploaderConfig is a declarative rule that streams matching resources to a
// custom HTTP target during transfer. It is carried as an entry inside the central
// generic configuration (generic.config.ocm.software/v1), as a sibling of [Config],
// and extracted with [LookupUploaderConfigs]. Each entry is an independent rule;
// entries are not merged. The config type dedicates it to the HTTP streaming target,
// so the upload request fields are declared inline rather than in a nested stream
// sub-type.
//
//	type: generic.config.ocm.software/v1
//	configurations:
//	  - type: http.uploader.transfer.config.ocm.software/v1alpha1
//	    match:
//	      accessType: Wget/v1
//	    targetURL: '${"https://mytarget.registry.com/uploads" + url(resource.access.url).path}'
//	    method: PUT
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type HTTPUploaderConfig struct {
	// +ocm:jsonschema-gen:enum=http.uploader.transfer.config.ocm.software/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=http.uploader.transfer.config.ocm.software
	Type runtime.Type `json:"type"`

	// MatchSpec selects the resources this uploader applies to. It is exposed as the
	// `match` field.
	MatchSpec UploaderMatch `json:"match"`

	// TargetURL is a standalone CEL expression wrapped in ${...} (referencing the
	// source resource via the `resource` alias) that resolves to the upload URL.
	// Append any static query string directly inside the expression.
	TargetURL string `json:"targetURL"`
	// Method is the HTTP verb used for the upload request (e.g. PUT).
	Method string `json:"method,omitempty"`
	// Header carries additional HTTP request headers.
	Header map[string][]string `json:"header,omitempty"`
	// NoRedirect disables following HTTP redirects for the upload request.
	NoRedirect bool `json:"noRedirect,omitempty"`
	// MediaType overrides the media type recorded on the uploaded resource; when
	// empty it defaults to the source access media type where available.
	MediaType string `json:"mediaType,omitempty"`
}

// UploaderMatch selects resources by their access type and identity. A resource
// matches when its access type matches AccessType (if set) and every specified
// identity constraint (Name, Version, ExtraIdentity) also matches. This lets multiple
// uploaders target the same access type while routing different resources to
// different upload targets; the first matching uploader (in declaration order) wins,
// so more specific rules should be declared before broader ones.
//
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type UploaderMatch struct {
	// AccessType is the resource access type this uploader matches (e.g. Wget/v1).
	// When empty, resources of any access type match; [HTTPUploaderConfig] still
	// requires it.
	AccessType runtime.Type `json:"accessType,omitempty,omitzero"`
	// Name optionally restricts the match to resources with this exact name.
	// When empty, resources of any name match.
	Name string `json:"name,omitempty"`
	// Version optionally restricts the match to resources with this exact
	// version. When empty, resources of any version match.
	Version string `json:"version,omitempty"`
	// ExtraIdentity optionally restricts the match to resources whose identity
	// contains all of these key/value pairs. When empty, no extra-identity
	// constraint is applied.
	ExtraIdentity runtime.Identity `json:"extraIdentity,omitempty"`
}

// Matches reports whether resource satisfies this match. A set access type must match:
// when the rule specifies a version (Wget/v1) it must equal the resource access type
// exactly; an unversioned rule (Wget) matches any version by name. The identity
// constraint (optional Name, Version plus ExtraIdentity) is a subset match against the
// resource identity: every specified key/value must be present and equal.
func (m UploaderMatch) Matches(resource descriptorv2.Resource) bool {
	if resource.Access == nil {
		return false
	}
	if !m.AccessType.IsEmpty() && !accessTypeMatches(m.AccessType, resource.Access.Type) {
		return false
	}
	return runtime.IdentitySubset(m.identity(), resource.ToIdentity())
}

// identity renders the match's identity constraint (Name, Version plus ExtraIdentity)
// as a [runtime.Identity] for subset matching against a resource identity. Name and
// Version map to the reserved name and version attributes; an empty Name or Version is
// omitted.
func (m UploaderMatch) identity() runtime.Identity {
	id := make(runtime.Identity, len(m.ExtraIdentity)+2)
	for k, v := range m.ExtraIdentity {
		id[k] = v
	}
	if m.Name != "" {
		id[descriptorv2.IdentityAttributeName] = m.Name
	}
	if m.Version != "" {
		id[descriptorv2.IdentityAttributeVersion] = m.Version
	}
	return id
}

// accessTypeMatches reports whether a resource access type satisfies the uploader match
// access type. The match is tried against every alias of the access type types knows
// (the access type itself if types is nil or does not know it): a versioned match type
// must equal one of them ([runtime.Type.Equal]), an unversioned one must name one of them.
func accessTypeMatches(match, access runtime.Type, types TypeResolver) bool {
	matches := func(candidate runtime.Type) bool {
		if match.HasVersion() {
			return match.Equal(candidate)
		}
		return match.GetName() == candidate.GetName()
	}
	if matches(access) {
		return true
	}
	if types == nil {
		return false
	}
	canonical, ok := types.ResolveCanonicalType(access)
	if !ok {
		return false
	}
	if matches(canonical) {
		return true
	}
	for alias := range types.AliasesIter(canonical) {
		if matches(alias) {
			return true
		}
	}
	return false
}

// Validate rejects a non-matching [HTTPUploaderConfig.Type], an empty match access
// type, and an empty targetURL. An empty Type is allowed so callers constructing a
// config programmatically (without going through [Scheme.Decode]) do not need to set
// it.
func (u *HTTPUploaderConfig) Validate() error {
	if u == nil {
		return nil
	}
	if !u.Type.IsEmpty() {
		if u.Type.Name != HTTPUploaderConfigType || (u.Type.Version != "" && u.Type.Version != Version) {
			return fmt.Errorf("invalid type %q (must be %q or %q)",
				u.Type, HTTPUploaderConfigType, runtime.NewVersionedType(HTTPUploaderConfigType, Version))
		}
	}
	if u.MatchSpec.AccessType.IsEmpty() {
		return fmt.Errorf("match.accessType is required")
	}
	if u.TargetURL == "" {
		return fmt.Errorf("targetURL is required")
	}
	return nil
}

// GetMatch returns MatchSpec. It implements [UploaderConfig].
func (u *HTTPUploaderConfig) GetMatch() UploaderMatch { return u.MatchSpec }

// LookupUploaderConfigs extracts all uploader configurations from a central generic
// config. It walks the config entries in declaration order and recognizes an entry as
// an uploader purely by whether its registered type implements [UploaderConfig] — no
// separate registration is required beyond registering the type in [Scheme]. Recognized
// entries are decoded and validated; the preserved order lets the first match win.
// Entries of unrelated types (including [Config]) are ignored. Returns nil if cfg is
// nil or contains no uploader entries.
func LookupUploaderConfigs(cfg *genericv1.Config) ([]UploaderConfig, error) {
	if cfg == nil || len(cfg.Configurations) == 0 {
		return nil, nil
	}
	var uploaders []UploaderConfig
	for _, entry := range cfg.Configurations {
		// An entry is an uploader iff its type is registered in Scheme and the
		// prototype implements UploaderConfig. NewObject fails for types unknown to
		// this Scheme (e.g. transfer/s3/oci config entries), which are not uploaders.
		obj, err := Scheme.NewObject(entry.GetType())
		if err != nil {
			continue
		}
		u, ok := obj.(UploaderConfig)
		if !ok {
			continue
		}
		if err := runtime.DecodeStrict(entry, u); err != nil {
			return nil, fmt.Errorf("failed to decode uploader config: %w", err)
		}
		if err := u.Validate(); err != nil {
			return nil, fmt.Errorf("invalid uploader config: %w", err)
		}
		uploaders = append(uploaders, u)
	}
	return uploaders, nil
}
