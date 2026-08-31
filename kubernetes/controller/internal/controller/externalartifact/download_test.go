package externalartifact

import (
	"strings"
	"testing"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

func TestSingleFileNameIsPathSafe(t *testing.T) {
	cases := []struct {
		name     string
		resName  string
		resType  string
		wantName string
	}{
		{"plain manifest", "config", "kustomization", "config.yaml"},
		{"helm chart", "podinfo", "helmChart", "podinfo.tgz"},
		{"empty name", "", "kustomization", "resource.yaml"},
		{"path traversal absolute", "/etc/passwd", "kustomization", "passwd.yaml"},
		{"path traversal parent", "../../evil", "kustomization", "evil.yaml"},
		{"nested slashes", "a/b/c", "kustomization", "c.yaml"},
		{"dot dot only", "..", "kustomization", "resource.yaml"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := &descriptor.Resource{Type: tc.resType}
			res.Name = tc.resName

			got := singleFileName(res)
			if got != tc.wantName {
				t.Errorf("singleFileName(%q, %q) = %q, want %q", tc.resName, tc.resType, got, tc.wantName)
			}
			if strings.ContainsAny(got, "/\\") || strings.Contains(got, "..") {
				t.Errorf("singleFileName(%q) = %q is not path-safe", tc.resName, got)
			}
		})
	}
}
