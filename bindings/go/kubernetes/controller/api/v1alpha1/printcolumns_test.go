package v1alpha1

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"
)

type printColumn struct {
	Name     string
	JSONPath string
	Type     string
	Priority int32
}

// statusColumns end every CRD so that `kubectl get` shows the Ready state and
// its message without `-o wide`.
var statusColumns = []printColumn{
	{Name: "Ready", JSONPath: `.status.conditions[?(@.type=="Ready")].status`, Type: "string"},
	{Name: "Age", JSONPath: ".metadata.creationTimestamp", Type: "date"},
	{Name: "Status", JSONPath: `.status.conditions[?(@.type=="Ready")].message`, Type: "string"},
}

// identifyingColumns lists, per generated CRD file, the columns that come
// before statusColumns. Every file under config/crd/bases needs an entry, so a
// new CRD cannot ship without its columns being checked.
var identifyingColumns = map[string][]printColumn{
	"delivery.ocm.software_components.yaml": {
		{Name: "Component", JSONPath: ".spec.component", Type: "string"},
		{Name: "Version", JSONPath: ".status.component.version", Type: "string"},
	},
	"delivery.ocm.software_repositories.yaml": {
		{Name: "Type", JSONPath: ".spec.repositorySpec.type", Type: "string"},
	},
	"delivery.ocm.software_resources.yaml": {
		{Name: "Component Ref", JSONPath: ".spec.componentRef.name", Type: "string"},
		{Name: "Resource", JSONPath: ".status.resource.name", Type: "string"},
		{Name: "Version", JSONPath: ".status.resource.version", Type: "string"},
	},
	"delivery.ocm.software_deployers.yaml": {
		{Name: "Resource Namespace", JSONPath: ".spec.resourceRef.namespace", Type: "string"},
		{Name: "Resource Ref", JSONPath: ".spec.resourceRef.name", Type: "string"},
	},
	"delivery.ocm.software_namespaceddeployers.yaml": {
		{Name: "Resource Ref", JSONPath: ".spec.resourceRef.name", Type: "string"},
	},
	"delivery.ocm.software_replications.yaml": {
		{Name: "Component Ref", JSONPath: ".spec.componentRef.name", Type: "string"},
		{Name: "Target", JSONPath: ".spec.targetRepositoryRef.name", Type: "string"},
		{Name: "Version", JSONPath: ".status.lastTransferredVersion", Type: "string"},
	},
	"delivery.ocm.software_discoveries.yaml": {
		{Name: "Component Ref", JSONPath: ".spec.componentRef.name", Type: "string"},
	},
}

// TestPrintColumns reads every generated CRD under ../../config/crd/bases and
// checks the column names, JSONPaths, types, priorities, and order of the
// served version. Every expected priority is 0, so no column needs `-o wide`.
func TestPrintColumns(t *testing.T) {
	r := require.New(t)

	dir := filepath.Join("..", "..", "config", "crd", "bases")
	entries, err := os.ReadDir(dir)
	r.NoError(err)

	seen := make(map[string]bool, len(identifyingColumns))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		file := entry.Name()
		seen[file] = true

		t.Run(file, func(t *testing.T) {
			r := require.New(t)

			identifying, ok := identifyingColumns[file]
			r.True(ok, "add the identifying columns of %s to identifyingColumns", file)

			data, err := os.ReadFile(filepath.Join(dir, file))
			r.NoError(err)

			var crd apiextensionsv1.CustomResourceDefinition
			r.NoError(yaml.Unmarshal(data, &crd))

			var columns []apiextensionsv1.CustomResourceColumnDefinition
			for _, v := range crd.Spec.Versions {
				if v.Name == GroupVersion.Version {
					columns = v.AdditionalPrinterColumns
				}
			}
			r.NotEmpty(columns, "no printer columns for version %s", GroupVersion.Version)

			got := make([]printColumn, 0, len(columns))
			for _, c := range columns {
				got = append(got, printColumn{Name: c.Name, JSONPath: c.JSONPath, Type: c.Type, Priority: c.Priority})
			}

			want := append(append([]printColumn{}, identifying...), statusColumns...)
			r.Equal(want, got)
		})
	}

	for file := range identifyingColumns {
		r.True(seen[file], "identifyingColumns lists %s, but %s has no such file", file, dir)
	}
}
