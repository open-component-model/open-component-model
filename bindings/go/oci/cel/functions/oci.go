// Package functions provides CEL functions for working with OCI references in OCM
// expressions. They are shared by every CEL environment that evaluates OCM resources,
// e.g. the transfer graph and the Kubernetes controller.
package functions

import (
	"fmt"
	"log/slog"
	"strings"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"

	"ocm.software/open-component-model/bindings/go/oci/looseref"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	ociaccessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ToOCIFunctionName is the name of the CEL function that splits an OCI reference into
// its components.
const ToOCIFunctionName = "toOCI"

// Reference is an OCI reference split into its components. Host is empty when the
// reference names no registry.
type Reference struct {
	Host       string
	Repository string
	Tag        string
	Digest     string
}

// ParseReference splits reference into its components. It accepts loose references
// (with or without a scheme such as http://) and keeps both tag and digest when both
// are present.
func ParseReference(reference string) (Reference, error) {
	r, err := looseref.ParseReference(reference)
	if err != nil {
		return Reference{}, err
	}
	var digest string
	// A validation error means the reference carries no digest.
	if d, err := r.Digest(); err == nil {
		digest = d.String()
	}
	return Reference{
		Host:       r.Host(),
		Repository: strings.TrimLeft(r.Repository, "/"),
		Tag:        r.Tag,
		Digest:     digest,
	}, nil
}

// Map renders r as the value toOCI returns: host, registry (an alias of host),
// repository, tag, digest and reference (tag@digest, tag or digest).
func (r Reference) Map() map[string]string {
	var reference string
	switch {
	case r.Tag != "" && r.Digest != "":
		reference = r.Tag + "@" + r.Digest
	case r.Tag != "":
		reference = r.Tag
	case r.Digest != "":
		reference = r.Digest
	}
	return map[string]string{
		"host":       r.Host,
		"registry":   r.Host,
		"repository": r.Repository,
		"tag":        r.Tag,
		"digest":     r.Digest,
		"reference":  reference,
	}
}

// ReferenceResolver resolves an access that toOCI does not handle itself (anything but
// an OCIImage access) into an OCI reference. It reports ok=false when it does not
// handle the access type, so the next resolver is consulted.
type ReferenceResolver func(access *runtime.Raw) (ref Reference, ok bool, err error)

// Option configures [ToOCI].
type Option func(*options)

type options struct {
	resolvers []ReferenceResolver
}

// WithReferenceResolver registers a resolver for access types beyond OCIImage, e.g.
// local blobs, whose OCI reference depends on the caller's context. Resolvers are
// consulted in registration order.
func WithReferenceResolver(resolver ReferenceResolver) Option {
	return func(o *options) {
		o.resolvers = append(o.resolvers, resolver)
	}
}

// ToOCI returns a CEL environment option that registers the "toOCI" function, callable
// as <value>.toOCI() or toOCI(<value>). The value is either an OCI reference string or
// an access map; OCIImage accesses are resolved from their imageReference, other access
// types through the resolvers registered with [WithReferenceResolver]. The function
// returns the map described by [Reference.Map].
func ToOCI(opts ...Option) cel.EnvOption {
	return cel.Function(
		ToOCIFunctionName,
		cel.MemberOverload(
			"toOCI_dyn_member",
			[]*cel.Type{cel.DynType},
			types.NewMapType(types.StringType, types.StringType),
		),
		cel.Overload(
			"toOCI_dyn",
			[]*cel.Type{cel.DynType},
			types.NewMapType(types.StringType, types.StringType),
		),
		cel.SingletonUnaryBinding(BindingToOCI(opts...)),
	)
}

// BindingToOCI is the implementation of the toOCI function; see [ToOCI].
func BindingToOCI(opts ...Option) func(lhs ref.Val) ref.Val {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}
	return func(lhs ref.Val) ref.Val {
		var r Reference
		switch v := lhs.Value().(type) {
		case string:
			parsed, err := ParseReference(v)
			if err != nil {
				return types.WrapErr(err)
			}
			r = parsed
		case map[string]any:
			resolved, err := o.resolveMap(v)
			if err != nil {
				return types.NewErr("%s", err)
			}
			r = resolved
		default:
			return types.NewErr("expected string or map with an OCI access, got %T", lhs.Value())
		}
		// Return a standard CEL map. We intentionally avoid k8s apiserver's lazy.MapValue
		// because its Iterator() yields values (not keys), which breaks generic map
		// iteration.
		return types.DefaultTypeAdapter.NativeToValue(r.Map())
	}
}

// resolveMap resolves an access map into its OCI reference: OCIImage directly, other
// access types through the registered resolvers.
func (o *options) resolveMap(m map[string]any) (Reference, error) {
	r, err := o.resolveAccess(m)
	if err == nil {
		return r, nil
	}

	// TODO(matthiasbruns): drop the untyped fallback - https://github.com/open-component-model/ocm-project/issues/960
	if imgRef, ok := m["imageReference"].(string); ok {
		slog.Warn(
			"toOCI(): falling back to untyped 'imageReference' field, please use a proper OCM access type (e.g. OCIImage/v1 or localBlob/v1). "+
				"This feature is deprecated and will be removed in a future release. "+
				"You can track the progress here https://github.com/open-component-model/ocm-project/issues/960",
			"imageReference", imgRef, "error", err)
		return ParseReference(imgRef)
	}
	return Reference{}, err
}

// resolveAccess resolves a typed access map: an OCIImage access through its image
// reference, any other type through the first registered resolver that accepts it.
// Unlike resolveMap it has no untyped fallback.
func (o *options) resolveAccess(m map[string]any) (Reference, error) {
	unstructured, err := runtime.UnstructuredFromMixedData(m)
	if err != nil {
		return Reference{}, fmt.Errorf("converting map to unstructured failed: %w", err)
	}
	// runtime.Scheme.Convert does not support runtime.Unstructured as a conversion source.
	// TODO(matthiasbruns) https://github.com/open-component-model/ocm-project/issues/944
	var raw runtime.Raw
	if err := runtime.NewScheme(runtime.WithAllowUnknown()).Convert(unstructured, &raw); err != nil {
		return Reference{}, fmt.Errorf("converting unstructured to raw failed: %w", err)
	}
	if raw.GetType().IsEmpty() {
		return Reference{}, fmt.Errorf("expected an access with a type, got an untyped map")
	}

	if obj, err := ociaccess.Scheme.NewObject(raw.GetType()); err == nil {
		if err := ociaccess.Scheme.Convert(&raw, obj); err != nil {
			return Reference{}, fmt.Errorf("converting access of type %s failed: %w", raw.GetType(), err)
		}
		if image, ok := obj.(*ociaccessv1.OCIImage); ok {
			return ParseReference(image.ImageReference)
		}
	}

	for _, resolve := range o.resolvers {
		r, ok, err := resolve(&raw)
		if err != nil {
			return Reference{}, err
		}
		if ok {
			return r, nil
		}
	}
	return Reference{}, fmt.Errorf("no OCI reference can be derived from access type %s", raw.GetType())
}
