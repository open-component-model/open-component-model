package internal

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	celparser "ocm.software/open-component-model/bindings/go/cel/expression/parser"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
	wgetaccess "ocm.software/open-component-model/bindings/go/wget/spec/access"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
	wgettransformv1alpha1 "ocm.software/open-component-model/bindings/go/wget/transformation/spec/v1alpha1"
)

// matchUploader returns the first uploader whose match applies to resource, or nil.
// The access type must match: when the rule specifies a version (Wget/v1) it must
// equal the resource access type exactly; an unversioned rule (Wget) matches any
// version by name. The identity constraint (optional Name, Version plus
// ExtraIdentity) is a subset match against the resource identity via
// [runtime.IdentitySubset]: every specified key/value must be present and equal.
// Declaration order is significant —
// the first match wins, so more specific rules should precede broader ones.
func matchUploader(uploaders []*transferv1alpha1.HTTPUploaderConfig, resource descriptorv2.Resource) *transferv1alpha1.HTTPUploaderConfig {
	accessType := resource.Access.Type
	identity := resource.ToIdentity()
	for _, u := range uploaders {
		if u == nil {
			continue
		}
		if !accessTypeMatches(u.Match.AccessType, accessType) {
			continue
		}
		if !runtime.IdentitySubset(matchIdentity(u.Match), identity) {
			continue
		}
		return u
	}
	return nil
}

// accessTypeMatches reports whether a resource access type satisfies the uploader
// match access type. A versioned match type must equal the access type exactly
// ([runtime.Type.Equal]); an unversioned match type matches any version by name.
func accessTypeMatches(match, access runtime.Type) bool {
	if match.HasVersion() {
		return match.Equal(access)
	}
	return match.GetName() == access.GetName()
}

// matchIdentity renders an uploader match's identity constraint (Name, Version
// plus ExtraIdentity) as a [runtime.Identity] for subset matching against a
// resource identity. Name and Version map to the reserved name and version
// attributes; an empty Name or Version is omitted.
func matchIdentity(m transferv1alpha1.UploaderMatch) runtime.Identity {
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

// resourceAlias is the identifier an uploader's targetURL CEL expression uses to
// reference the source resource. processUploader rewrites it to the concrete
// environment node path before the graph runtime evaluates the expression.
const resourceAlias = "resource"

// uploadsEnvKey is the environment map key under which source-resource nodes are
// injected, addressable in CEL as environment.uploads.<uploadID>.
const uploadsEnvKey = "uploads"

// buildResourceNode builds the CEL node value exposed to a targetURL/header expression
// as `resource`. The node is the JSON representation of the v2 resource — the same
// schema-driven approach the graph uses for the descriptor environment node (see
// addDescriptorToEnvironment) — so every resource field is addressable under its v2
// JSON name without hand-maintaining a field list: resource.name/version/type,
// resource.access.<field> (e.g. resource.access.url for wget,
// resource.access.imageReference for OCI), resource.extraIdentity.<key>, and, when the
// source carries one, resource.digest.{hashAlgorithm,normalisationAlgorithm,value} for
// checksum header templating (RFC 9530 Repr-Digest, x-checksum-*). A targetURL
// expression that needs the individual URL parts (scheme/host/path/...) parses them
// with the inbuilt url() CEL function, e.g. url(resource.access.url).path.
func buildResourceNode(resource descriptorv2.Resource) (map[string]any, error) {
	if resource.Access == nil {
		return nil, fmt.Errorf("resource access is required")
	}
	raw, err := json.Marshal(resource)
	if err != nil {
		return nil, fmt.Errorf("cannot marshal resource: %w", err)
	}
	node := map[string]any{}
	if err := json.Unmarshal(raw, &node); err != nil {
		return nil, fmt.Errorf("cannot decode resource fields: %w", err)
	}
	return node, nil
}

// celTargetURLField validates that the user-supplied targetURL is a single
// standalone CEL expression wrapped in ${...} and rewrites the `resource` alias
// to the concrete environment node path. The result is a standalone ${...} field
// value the graph runtime evaluates.
func celTargetURLField(rawTargetURL, nodePath string) (string, error) {
	if strings.TrimSpace(rawTargetURL) == "" {
		return "", fmt.Errorf("targetURL is required")
	}
	return celExpressionField("targetURL", rawTargetURL, nodePath)
}

// templateHeader rewrites every value of an uploader's header map through celHeaderValue,
// so ${...} values template the source resource and plain values pass through unchanged.
// Returns nil for an empty header map, preserving the omitempty target access field.
func templateHeader(header map[string][]string, nodePath string) (map[string][]string, error) {
	if len(header) == 0 {
		return nil, nil
	}
	out := make(map[string][]string, len(header))
	for name, vals := range header {
		rewritten := make([]string, len(vals))
		for i, v := range vals {
			field, err := celHeaderValue(name, v, nodePath)
			if err != nil {
				return nil, err
			}
			rewritten[i] = field
		}
		out[name] = rewritten
	}
	return out, nil
}

// celHeaderValue rewrites a single upload header value. A value wrapped in ${...}
// is treated as a CEL expression (same rewrite as targetURL), so header values can
// template the source resource — e.g. RFC 9530 Repr-Digest or x-checksum-* headers
// derived from resource.digest.value. A plain value without ${...} is a literal and
// is passed through unchanged.
func celHeaderValue(name, rawValue, nodePath string) (string, error) {
	if !strings.Contains(rawValue, "${") {
		return rawValue, nil
	}
	field, err := celExpressionField(fmt.Sprintf("header %q", name), rawValue, nodePath)
	if err != nil {
		return "", err
	}
	return field, nil
}

// celExpressionField validates that raw is a single standalone CEL expression wrapped
// in ${...} and rewrites the `resource` alias to the concrete environment node path.
// fieldName is used only for error messages.
func celExpressionField(fieldName, raw, nodePath string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	standalone, err := celparser.IsStandaloneExpression(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid %s CEL expression %q: %w", fieldName, raw, err)
	}
	if !standalone {
		return "", fmt.Errorf("%s must be a single CEL expression wrapped in ${...}, got %q", fieldName, raw)
	}
	return "${" + rewriteAlias(trimmed[len("${"):len(trimmed)-len("}")], resourceAlias, nodePath) + "}", nil
}

// rewriteAlias replaces the bare identifier alias with replacement everywhere it
// appears as an identifier token in the CEL source expr, leaving occurrences
// inside string literals untouched. An identifier match requires that the
// preceding character is not part of an identifier or a member-access dot (so
// `resource` is rewritten but `myresource` and `x.resource` are not) and that
// the following character does not continue the identifier.
func rewriteAlias(expr, alias, replacement string) string {
	var b strings.Builder
	b.Grow(len(expr))
	var inString byte // 0 when outside a string literal, else the opening quote
	escaped := false
	for i := 0; i < len(expr); i++ {
		c := expr[i]
		if inString != 0 {
			b.WriteByte(c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == inString:
				inString = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			inString = c
			b.WriteByte(c)
			continue
		}
		if isIdentifierStart(c) && strings.HasPrefix(expr[i:], alias) {
			end := i + len(alias)
			prev := byte(0)
			if i > 0 {
				prev = expr[i-1]
			}
			next := byte(0)
			if end < len(expr) {
				next = expr[end]
			}
			if !isIdentifierPart(prev) && prev != '.' && !isIdentifierPart(next) {
				b.WriteString(replacement)
				i = end - 1
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

func isIdentifierStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentifierPart(c byte) bool {
	return isIdentifierStart(c) || (c >= '0' && c <= '9')
}

// targetHostFromExpression best-effort extracts a display host from a raw targetURL
// expression for the transformation label. It parses the leading string literal (if
// any); otherwise returns "target".
func targetHostFromExpression(rawTargetURL string) string {
	trimmed := strings.TrimSpace(rawTargetURL)
	// Look past the leading ${ delimiter (and any inner whitespace) to the first
	// token, which is a string literal for the common `"https://host" + ...` form.
	trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "${"))
	for _, quote := range []byte{'"', '\''} {
		if len(trimmed) > 0 && trimmed[0] == quote {
			if end := strings.IndexByte(trimmed[1:], quote); end >= 0 {
				if parsed, err := url.Parse(trimmed[1 : 1+end]); err == nil && parsed.Host != "" {
					return parsed.Host
				}
			}
		}
	}
	return "target"
}

// processUploader emits a single HTTPStreaming transformation for resource. It injects
// the source resource as a CEL node into the graph environment (addressable as
// environment.uploads.<uploadID>) and sets the target Wget access URL to a CEL
// expression derived from the uploader's targetURL, so the graph runtime resolves the
// final URL from the source resource. The remaining request fields map onto the target
// Wget access field-for-field.
func processUploader(resource descriptorv2.Resource, u *transferv1alpha1.HTTPUploaderConfig, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, resourceTransformIDs map[int]string, i int) error {
	if strings.TrimSpace(u.TargetURL) == "" {
		return fmt.Errorf("uploader targetURL is required")
	}

	if resource.Access == nil {
		return fmt.Errorf("resource access is required")
	}

	resourceID := identityToTransformationID(resource.ToIdentity())
	uploadID := fmt.Sprintf("%sUpload%s", id, resourceID)

	// Inject the source resource as a CEL node the targetURL expression can reference.
	// The node exposes every access field generically under resource.access.<field>,
	// so an uploader works with any source access type, not only wget.
	node, err := buildResourceNode(resource)
	if err != nil {
		return err
	}
	uploads, _ := tgd.Environment.Data[uploadsEnvKey].(map[string]any)
	if uploads == nil {
		uploads = map[string]any{}
		tgd.Environment.Data[uploadsEnvKey] = uploads
	}
	uploads[uploadID] = node

	nodePath := fmt.Sprintf("environment.%s.%s", uploadsEnvKey, uploadID)
	targetURLField, err := celTargetURLField(u.TargetURL, nodePath)
	if err != nil {
		return err
	}

	// Header values may be CEL-templated against the source resource (e.g. RFC 9530
	// Repr-Digest or x-checksum-* headers from resource.digest.value). A value wrapped
	// in ${...} is rewritten and resolved by the graph runtime; a plain value is a literal.
	header, err := templateHeader(u.Header, nodePath)
	if err != nil {
		return err
	}

	// The target media type defaults to the uploader's explicit value, then to the
	// source access media type when it exposes one (wget, OCI, ...).
	mediaType := u.MediaType
	if mediaType == "" {
		if access, ok := node["access"].(map[string]any); ok {
			if mt, ok := access["mediaType"].(string); ok {
				mediaType = mt
			}
		}
	}
	targetAccess := &wgetaccessv1.Wget{
		Type:       wgetaccess.V1VersionedType,
		URL:        targetURLField,
		Verb:       u.Method,
		Header:     header,
		Body:       u.Body,
		NoRedirect: u.NoRedirect,
		MediaType:  mediaType,
	}
	targetAccessRaw := &runtime.Raw{}
	if err := wgetaccess.Scheme.Convert(targetAccess, targetAccessRaw); err != nil {
		return fmt.Errorf("cannot convert target wget access: %w", err)
	}

	targetResource := *resource.DeepCopy()
	targetResource.Access = targetAccessRaw

	spec, err := runtime.UnstructuredFromMixedData(map[string]any{
		"resource":       resource,
		"targetResource": targetResource,
	})
	if err != nil {
		return fmt.Errorf("cannot create unstructured spec for uploader transformation: %w", err)
	}

	label := uploaderLabel(&val.Descriptor.Component, resource.Name, targetHostFromExpression(u.TargetURL))
	tgd.Transformations = append(tgd.Transformations, transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type:  wgettransformv1alpha1.HTTPStreamingV1alpha1,
			ID:    uploadID,
			Label: label,
		},
		Spec: spec,
	})
	resourceTransformIDs[i] = uploadID
	return nil
}
