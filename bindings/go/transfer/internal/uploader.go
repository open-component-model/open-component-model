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
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/transform/graph/runtime/resolver"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
	wgetaccess "ocm.software/open-component-model/bindings/go/wget/spec/access"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
	wgettransformv1alpha1 "ocm.software/open-component-model/bindings/go/wget/transformation/spec/v1alpha1"
)

// resourceAlias is the identifier an uploader's CEL expressions (HTTP targetURL, OCI
// imageReference) use to reference the source resource. It is rewritten to the concrete
// environment node path before the graph runtime evaluates the expression.
const resourceAlias = "resource"

// resourceNodePath returns the CEL path the `resource` alias is rewritten to. Instead
// of injecting a second copy of the resource into the environment, it points at the
// resource already present in the descriptor environment node (keyed by baseID, see
// addDescriptorToEnvironment) by its index in component.resources. The index is the
// resource's position in the descriptor the environment node was built from, so it is
// exact and stable for the duration of the graph build.
//
// Index selection avoids a CEL filter predicate over the whole resource list: the
// environment node's element type is inferred from the concrete JSON, so an optional
// field such as extraIdentity (omitempty) is absent from the inferred type whenever any
// resource lacks it, and a filter predicate that reads r.extraIdentity then fails type
// checking with "undefined field 'extraIdentity'". Selecting by index never references
// a field that a sibling resource omits.
//
// Every v2 resource field is addressable by appending to the returned path:
// <path>.access.url, <path>.digest.value, <path>.extraIdentity.<key>, <path>.labels,
// and so on.
func resourceNodePath(baseID string, index int) string {
	return fmt.Sprintf("environment.%s.component.resources[%d]", baseID, index)
}

// componentNodePath returns the CEL path the `component` alias is rewritten to: the component
// of the descriptor environment node keyed by baseID (see addDescriptorToEnvironment).
func componentNodePath(baseID string) string {
	return fmt.Sprintf("environment.%s.component", baseID)
}

// templateExpressions rewrites the aliases in every ${...} expression across the entire
// JSON object held by raw, in place. It does not hardcode which fields may carry
// expressions: it reuses the graph's own expression pipeline — [celparser.ParseSchemaless]
// discovers every expression field (standalone ${expr} and embedded "pre-${expr}"
// templates alike), each expression's aliases are rewritten (see rewriteAliases), and
// [resolver.Resolver.UpsertValueAtPath] splices the result back at the field's path.
// Strings without ${...} carry no expressions and pass through unchanged.
func templateExpressions(raw *runtime.Raw, aliases map[string]string) error {
	var obj map[string]any
	if err := json.Unmarshal(raw.Data, &obj); err != nil {
		return fmt.Errorf("cannot decode target access: %w", err)
	}

	fields, err := celparser.ParseSchemaless(obj)
	if err != nil {
		return fmt.Errorf("cannot parse target access expressions: %w", err)
	}

	res := resolver.NewResolver(obj, nil, nil)
	for _, field := range fields {
		current, err := res.GetValueFromPath(field.Path)
		if err != nil {
			return fmt.Errorf("cannot read field %s: %w", field.Path, err)
		}
		value, ok := current.(string)
		if !ok {
			continue
		}
		exprValues := make([]string, len(field.Expressions))
		for j, expr := range field.Expressions {
			exprValues[j] = expr.Value
		}
		rewritten := rewriteAliases(value, exprValues, aliases)
		if err := res.UpsertValueAtPath(field.Path, rewritten); err != nil {
			return fmt.Errorf("cannot rewrite field %s: %w", field.Path, err)
		}
	}

	data, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("cannot re-encode target access: %w", err)
	}
	raw.Data = data
	return nil
}

// templateString rewrites the aliases in every ${...} expression of a single string
// value, like templateExpressions does for a whole object. It returns the rewritten
// value and the original (unrewritten) expression sources; a plain literal without
// ${...} yields no expressions and is returned unchanged.
func templateString(value string, aliases map[string]string) (string, []string, error) {
	fields, err := celparser.ParseSchemaless(map[string]any{"value": value})
	if err != nil {
		return "", nil, fmt.Errorf("cannot parse expressions: %w", err)
	}
	var exprValues []string
	for _, field := range fields {
		for _, expr := range field.Expressions {
			exprValues = append(exprValues, expr.Value)
		}
	}
	return rewriteAliases(value, exprValues, aliases), exprValues, nil
}

// rewriteAliases rewrites every alias (map key) to its replacement CEL source (map value)
// inside each ${expr} of value, then substitutes the rewritten expression back into
// value. For a standalone ${expr} this replaces the whole value; for an embedded template
// it rewrites each ${expr} in place. Aliases are applied in sorted order so the result is
// deterministic.
func rewriteAliases(value string, exprValues []string, aliases map[string]string) string {
	names := slices.Sorted(maps.Keys(aliases))
	rewritten := value
	for _, expr := range exprValues {
		replaced := expr
		for _, name := range names {
			replaced = celparser.RewriteIdentifier(replaced, name, aliases[name])
		}
		rewritten = strings.ReplaceAll(rewritten, "${"+expr+"}", "${"+replaced+"}")
	}
	return rewritten
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
		mediaType = repositoryupload.MediaTypeFromAccess(resource)
	}
	// Build the upload request access from the raw user strings; expression templating is
	// applied generically to the whole object below rather than to hand-picked fields.
	requestAccess := &wgetaccessv1.Wget{
		Type:       wgetaccess.V1VersionedType,
		URL:        u.TargetURL,
		Verb:       u.Method,
		Header:     u.Header,
		NoRedirect: u.NoRedirect,
		MediaType:  mediaType,
	}
	// The published (download) access is the target URL as a plain read access. It
	// deliberately omits the upload-only request fields (write verb, body, request
	// headers, redirect handling) so a later `ocm download` does not re-issue the write
	// request and overwrite the uploaded object.
	publishedAccess := &wgetaccessv1.Wget{
		Type:      wgetaccess.V1VersionedType,
		URL:       u.TargetURL,
		MediaType: mediaType,
	}

	// Rewrite the `resource` and `component` aliases in every ${...} string across each
	// access object, rather than templating hand-picked fields. They point at the resource
	// and component already present in the descriptor environment node (see resourceNodePath)
	// so no second copy is injected; every access field stays addressable generically under
	// resource.access.<field>, so an uploader works with any source access type, not only
	// wget. Both objects share the same targetURL expression, so the request and the
	// published read access resolve to the same URL at runtime.

	requestRaw := &runtime.Raw{}
	if err := wgetaccess.Scheme.Convert(requestAccess, requestRaw); err != nil {
		return fmt.Errorf("cannot convert uploader request access: %w", err)
	}
	if err := templateExpressions(requestRaw, map[string]string{resourceAlias: nodePath}); err != nil {
		return fmt.Errorf("cannot template uploader request access: %w", err)
	}

	publishedRaw := &runtime.Raw{}
	if err := wgetaccess.Scheme.Convert(publishedAccess, publishedRaw); err != nil {
		return fmt.Errorf("cannot convert uploader published access: %w", err)
	}
	if err := templateExpressions(publishedRaw, map[string]string{resourceAlias: nodePath}); err != nil {
		return fmt.Errorf("cannot template uploader published access: %w", err)
	}

	targetResource := *resource.DeepCopy()
	targetResource.Access = publishedRaw

	spec, err := runtime.UnstructuredFromMixedData(map[string]any{
		"resource":       resource,
		"request":        requestRaw,
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
