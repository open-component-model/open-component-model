package internal

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"slices"
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
// Declaration order is significant — the first recognized match wins, so more specific
// rules should precede broader ones. The per-uploader matching semantics are defined by
// [transferv1alpha1.UploaderConfig.Match].
func matchUploader(uploaders []transferv1alpha1.UploaderConfig, resource descriptorv2.Resource) transferv1alpha1.UploaderConfig {
	for _, u := range uploaders {
		if u == nil {
			continue
		}
		if u.Match(resource) {
			return u
		}
	}
	return nil
}

// resourceAlias is the identifier an uploader's targetURL CEL expression uses to
// reference the source resource. processUploader rewrites it to the concrete
// environment node path before the graph runtime evaluates the expression.
const resourceAlias = "resource"

// resourceFilterVar is the bound variable used inside the CEL filter macro that selects
// the source resource within the descriptor environment node. It is local to the filter
// predicate, so it cannot collide with the user's `resource` alias (already rewritten to
// the whole selector path before the predicate is built).
const resourceFilterVar = "r"

// resourceNodePath returns the CEL path the `resource` alias is rewritten to. Instead
// of injecting a second copy of the resource into the environment, it points at the
// resource already present in the descriptor environment node (keyed by baseID, see
// addDescriptorToEnvironment), selecting it out of component.resources with a filter on
// its full identity (name, version and every extraIdentity key). This keeps a single
// source of truth and stays index-independent. Every v2 resource field is addressable
// by appending to the returned path: <path>.access.url, <path>.digest.value,
// <path>.extraIdentity.<key>, <path>.labels, and so on.
func resourceNodePath(baseID string, resource descriptorv2.Resource) string {
	identity := resource.ToIdentity()
	keys := slices.Sorted(maps.Keys(identity))
	predicates := make([]string, 0, len(keys))
	for _, k := range keys {
		switch k {
		case descriptorv2.IdentityAttributeName:
			predicates = append(predicates, fmt.Sprintf("%s.name == %s", resourceFilterVar, celStringLiteral(identity[k])))
		case descriptorv2.IdentityAttributeVersion:
			predicates = append(predicates, fmt.Sprintf("%s.version == %s", resourceFilterVar, celStringLiteral(identity[k])))
		default:
			predicates = append(predicates, fmt.Sprintf("%s.extraIdentity[%s] == %s", resourceFilterVar, celStringLiteral(k), celStringLiteral(identity[k])))
		}
	}
	return fmt.Sprintf("environment.%s.component.resources.filter(%s, %s)[0]",
		baseID, resourceFilterVar, strings.Join(predicates, " && "))
}

// celStringLiteral renders s as a double-quoted CEL string literal, escaping the
// backslash and double-quote characters so the selector parses safely.
func celStringLiteral(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// mediaTypeFromAccess extracts the source access media type (if any) from the resource
// access, used as the default target media type. Returns "" when absent.
func mediaTypeFromAccess(resource descriptorv2.Resource) string {
	if resource.Access == nil || len(resource.Access.Data) == 0 {
		return ""
	}
	var access struct {
		MediaType string `json:"mediaType"`
	}
	if err := json.Unmarshal(resource.Access.Data, &access); err != nil {
		return ""
	}
	return access.MediaType
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

// processUploader emits a single HTTPStreaming transformation for resource. It resolves
// the target Wget access URL from a CEL expression derived from the uploader's
// targetURL: the `resource` alias is rewritten to the resource's path inside the shared
// descriptor environment node (see resourceNodePath), so no second copy of the resource
// is injected. The remaining request fields map onto the target Wget access
// field-for-field.
func processUploader(resource descriptorv2.Resource, u *transferv1alpha1.HTTPUploaderConfig, baseID, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, resourceTransformIDs map[int]string, i int) error {
	if strings.TrimSpace(u.TargetURL) == "" {
		return fmt.Errorf("uploader targetURL is required")
	}

	if resource.Access == nil {
		return fmt.Errorf("resource access is required")
	}

	resourceID := identityToTransformationID(resource.ToIdentity())
	uploadID := fmt.Sprintf("%sUpload%s", id, resourceID)

	// Point the `resource` alias at the resource already present in the descriptor
	// environment node, selected by identity, rather than injecting a duplicate. The
	// node exposes every access field generically under resource.access.<field>, so an
	// uploader works with any source access type, not only wget.
	nodePath := resourceNodePath(baseID, resource)
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
		mediaType = mediaTypeFromAccess(resource)
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
