// Package uploadpath derives where a resource is stored in the target repository. All paths it
// returns are path-escaped and relative to the repository root.
package uploadpath

import (
	"fmt"
	"net/url"
	"strings"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	uploadv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/transformation/spec/v1alpha1"
)

// File returns the file name of the resource: <resource name>-<resource version><ext>, with a
// hash of the extra identity appended when the resource has one, so every resource of a
// component version has its own file.
func File(res *descriptor.Resource, ext string) (string, error) {
	if strings.ContainsAny(res.Name+res.Version, "/\\") {
		return "", fmt.Errorf("resource name %q and version %q must not contain path separators", res.Name, res.Version)
	}
	file := res.Name + "-" + res.Version
	if len(res.ExtraIdentity) > 0 {
		file += fmt.Sprintf("-%016x", res.ExtraIdentity.CanonicalHashV1())
	}
	return url.PathEscape(file + ext), nil
}

// Resolve returns the configured path of spec, see [Custom], else the default location
// <component>/<component version>/<resource file>, see [File].
func Resolve(spec *uploadv1alpha1.RepositoryUploadSpec, res *descriptor.Resource, ext string) (string, error) {
	if spec.Path != "" {
		return Custom(spec.Path, ext)
	}
	file, err := File(res, ext)
	if err != nil {
		return "", err
	}
	component, version := spec.ComponentVersion.Component, spec.ComponentVersion.Version
	segments := append(strings.Split(component, "/"), version)
	for i, segment := range segments {
		if !validSegment(segment) {
			return "", fmt.Errorf("component %q version %q cannot be used as a repository path", component, version)
		}
		segments[i] = url.PathEscape(segment)
	}
	return strings.Join(append(segments, file), "/"), nil
}

// Custom validates a configured path. It must be relative, consist of non-empty segments other
// than . and .., and end in requiredSuffix, so it can neither leave the repository nor address a
// folder.
func Custom(path, requiredSuffix string) (string, error) {
	if !strings.HasSuffix(path, requiredSuffix) {
		return "", fmt.Errorf("path %q must end in %s", path, requiredSuffix)
	}
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		if !validSegment(segment) {
			return "", fmt.Errorf("path %q must be relative without empty, \".\" or \"..\" segments", path)
		}
		segments[i] = url.PathEscape(segment)
	}
	return strings.Join(segments, "/"), nil
}

func validSegment(segment string) bool {
	return segment != "" && segment != "." && segment != ".." && !strings.Contains(segment, "\\")
}
