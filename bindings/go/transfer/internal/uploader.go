package internal

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
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

// resourceAlias is the identifier an uploader's targetURL CEL expression uses to
// reference the source resource. processHTTPUploader rewrites it to the concrete
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
			predicates = append(predicates, fmt.Sprintf("%s.name == %s", resourceFilterVar, strconv.Quote(identity[k])))
		case descriptorv2.IdentityAttributeVersion:
			predicates = append(predicates, fmt.Sprintf("%s.version == %s", resourceFilterVar, strconv.Quote(identity[k])))
		default:
			predicates = append(predicates, fmt.Sprintf("%s.extraIdentity[%s] == %s", resourceFilterVar, strconv.Quote(k), strconv.Quote(identity[k])))
		}
	}
	return fmt.Sprintf("environment.%s.component.resources.filter(%s, %s)[0]",
		baseID, resourceFilterVar, strings.Join(predicates, " && "))
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

// templateExpressions rewrites the `resource` alias in every ${...} string across the
// entire JSON object held by raw, in place. It does not hardcode which fields may carry
// expressions: any string value (a target URL, a header value, or any future field)
// wrapped in ${...} is validated as a standalone CEL expression and rewritten to
// reference nodePath; a plain string without ${...} is a literal and passes through
// unchanged. This mirrors how the graph runtime discovers and evaluates expressions
// across the whole transformation spec.
func templateExpressions(raw *runtime.Raw, nodePath string) error {
	var obj any
	if err := json.Unmarshal(raw.Data, &obj); err != nil {
		return fmt.Errorf("cannot decode target access: %w", err)
	}
	rewritten, err := templateValue(obj, nodePath, "")
	if err != nil {
		return err
	}
	data, err := json.Marshal(rewritten)
	if err != nil {
		return fmt.Errorf("cannot re-encode target access: %w", err)
	}
	raw.Data = data
	return nil
}

// templateValue recursively rewrites every ${...} string within v. path is the JSON
// path to v, used only for error messages.
func templateValue(v any, nodePath, path string) (any, error) {
	switch t := v.(type) {
	case string:
		if !strings.Contains(t, "${") {
			return t, nil
		}
		field := path
		if field == "" {
			field = "value"
		}
		return celExpressionField(field, t, nodePath)
	case map[string]any:
		for k, val := range t {
			rewritten, err := templateValue(val, nodePath, joinPath(path, k))
			if err != nil {
				return nil, err
			}
			t[k] = rewritten
		}
		return t, nil
	case []any:
		for i, val := range t {
			rewritten, err := templateValue(val, nodePath, fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return nil, err
			}
			t[i] = rewritten
		}
		return t, nil
	default:
		return v, nil
	}
}

// joinPath appends a field name to a JSON path for error messages.
func joinPath(base, field string) string {
	if base == "" {
		return field
	}
	return base + "." + field
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

// processHTTPUploader emits a single HTTPStreaming transformation for resource from an
// [transferv1alpha1.HTTPUploaderConfig]. It builds the target Wget access from the
// config's request fields and templates the whole object (see templateExpressions):
// every ${...} string has its `resource` alias rewritten to the resource's path inside
// the shared descriptor environment node (see resourceNodePath), so no second copy of
// the resource is injected; strings without ${...} are literals and pass through.
func processHTTPUploader(resource descriptorv2.Resource, u *transferv1alpha1.HTTPUploaderConfig, baseID, id string, val *discoveryValue, tgd *transformv1alpha1.TransformationGraphDefinition, resourceTransformIDs map[int]string, i int) error {
	if u.TargetURL == "" {
		return fmt.Errorf("uploader targetURL is required")
	}
	if resource.Access == nil {
		return fmt.Errorf("resource access is required")
	}

	resourceID := identityToTransformationID(resource.ToIdentity())
	uploadID := fmt.Sprintf("%sUpload%s", id, resourceID)

	// The target media type defaults to the uploader's explicit value, then to the
	// source access media type when it exposes one (wget, OCI, ...).
	mediaType := u.MediaType
	if mediaType == "" {
		mediaType = mediaTypeFromAccess(resource)
	}
	// Build the target access from the raw user strings; expression templating is applied
	// generically to the whole object below rather than to hand-picked fields.
	targetAccess := &wgetaccessv1.Wget{
		Type:       wgetaccess.V1VersionedType,
		URL:        u.TargetURL,
		Verb:       u.Method,
		Header:     u.Header,
		Body:       u.Body,
		NoRedirect: u.NoRedirect,
		MediaType:  mediaType,
	}
	targetAccessRaw := &runtime.Raw{}
	if err := wgetaccess.Scheme.Convert(targetAccess, targetAccessRaw); err != nil {
		return fmt.Errorf("cannot convert target wget access: %w", err)
	}

	// Rewrite the `resource` alias in every ${...} string across the entire target
	// access object, rather than templating hand-picked fields. Point it at the resource
	// already present in the descriptor environment node (selected by identity, see
	// resourceNodePath) so no second copy of the resource is injected; every access field
	// stays addressable generically under resource.access.<field>, so an uploader works
	// with any source access type, not only wget.
	nodePath := resourceNodePath(baseID, resource)
	if err := templateExpressions(targetAccessRaw, nodePath); err != nil {
		return fmt.Errorf("cannot template uploader target access: %w", err)
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
