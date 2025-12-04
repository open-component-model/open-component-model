package ast

import (
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/google/cel-go/cel"
	"ocm.software/open-component-model/bindings/go/cel/expression/fieldpath"
)

func TestInspector_InspectionResults(t *testing.T) {
	tests := []struct {
		name          string
		resources     []string
		functions     []string
		expression    string
		wantResources []ResourceDependency
		wantFunctions []FunctionCall
	}{
		{
			name:       "simple eks cluster state check",
			resources:  []string{"eksCluster"},
			expression: `eksCluster.status.state == "ACTIVE"`,
			wantResources: []ResourceDependency{
				{ID: "eksCluster", Path: fieldpath.MustParse("eksCluster.status.state")},
			},
		},
		{
			name:       "simple bucket name check",
			resources:  []string{"bucket"},
			expression: `bucket.spec.name == "my-bucket" && bucket.metadata.name == bucket.spec.name`,
			wantResources: []ResourceDependency{
				{ID: "bucket", Path: fieldpath.MustParse("bucket.metadata.name")},
				{ID: "bucket", Path: fieldpath.MustParse("bucket.spec.name")},
				{ID: "bucket", Path: fieldpath.MustParse("bucket.spec.name")},
			},
		},

		{
			name:       "bucket name with function",
			resources:  []string{"bucket"},
			functions:  []string{"toLower"},
			expression: `toLower(bucket.name)`,
			wantResources: []ResourceDependency{
				{ID: "bucket", Path: fieldpath.MustParse("bucket.name")},
			},
			wantFunctions: []FunctionCall{
				{Name: "toLower"},
			},
		},
		{
			name:       "deployment replicas with function",
			resources:  []string{"deployment"},
			functions:  []string{"max"},
			expression: `max(deployment.spec.replicas, 5)`,
			wantResources: []ResourceDependency{
				{ID: "deployment", Path: fieldpath.MustParse("deployment.spec.replicas")},
			},
			wantFunctions: []FunctionCall{
				{Name: "max"},
			},
		},
		{
			name:       "OR and index operators simple",
			resources:  []string{"list", "flags"},
			functions:  []string{},
			expression: `list[0] || flags["enabled"]`,
			wantResources: []ResourceDependency{
				{ID: "list", Path: fieldpath.MustParse("list")},
				{ID: "flags", Path: fieldpath.MustParse("flags")},
			},
			wantFunctions: []FunctionCall{},
		},
		{
			name:      "mixed constant types",
			resources: []string{},
			functions: []string{"process"},
			expression: `process(
				b"bytes123",         // BytesValue
				3.14,               // DoubleValue
				42u,                // Uint64Value
				null               // NullValue
			)`,
			wantResources: nil,
			wantFunctions: []FunctionCall{
				{Name: "process"},
			},
		},
		{
			name:       "test operator string conversion",
			resources:  []string{"list", "conditions"},
			functions:  []string{"validate"},
			expression: `validate(conditions.ready || conditions.initialized && list[3])`,
			wantResources: []ResourceDependency{
				{ID: "list", Path: fieldpath.MustParse("list")},
				{ID: "conditions", Path: fieldpath.MustParse("conditions.ready")},
				{ID: "conditions", Path: fieldpath.MustParse("conditions.initialized")},
			},
			wantFunctions: []FunctionCall{
				{Name: "validate", Arguments: []string{
					"(conditions.ready || conditions.initialized) && list[3]",
				}},
			},
		},
		{
			name:       "eks and nodegroup check",
			resources:  []string{"eksCluster", "nodeGroup"},
			expression: `eksCluster.spec.version == nodeGroup.spec.version`,
			wantResources: []ResourceDependency{
				{ID: "eksCluster", Path: fieldpath.MustParse("eksCluster.spec.version")},
				{ID: "nodeGroup", Path: fieldpath.MustParse("nodeGroup.spec.version")},
			},
		},
		{
			name:       "deployment and cluster version",
			resources:  []string{"deployment", "eksCluster"},
			expression: `deployment.metadata.namespace == "default" && eksCluster.spec.version == "1.31"`,
			wantResources: []ResourceDependency{
				{ID: "deployment", Path: fieldpath.MustParse("deployment.metadata.namespace")},
				{ID: "eksCluster", Path: fieldpath.MustParse("eksCluster.spec.version")},
			},
		},
		{
			name:       "eks name and bucket prefix",
			resources:  []string{"eksCluster", "bucket"},
			functions:  []string{"concat", "toLower"},
			expression: `concat(toLower(eksCluster.spec.name), "-", bucket.spec.name)`,
			wantResources: []ResourceDependency{
				{ID: "eksCluster", Path: fieldpath.MustParse("eksCluster.spec.name")},
				{ID: "bucket", Path: fieldpath.MustParse("bucket.spec.name")},
			},
			wantFunctions: []FunctionCall{
				{Name: "concat"},
				{Name: "toLower"},
			},
		},
		{
			name:       "instances count",
			resources:  []string{"instances"},
			functions:  []string{"count"},
			expression: `count(instances) > 0`,
			wantResources: []ResourceDependency{
				{ID: "instances", Path: fieldpath.MustParse("instances")},
			},
			wantFunctions: []FunctionCall{
				{Name: "count"},
			},
		},
		{
			name:      "complex expressions",
			resources: []string{"fargateProfile", "eksCluster"},
			functions: []string{"contains", "count"},
			expression: `contains(fargateProfile.spec.subnets, "subnet-123") &&
                count(fargateProfile.spec.selectors) <= 5 &&
                eksCluster.status.state == "ACTIVE"`,
			wantResources: []ResourceDependency{
				{ID: "fargateProfile", Path: fieldpath.MustParse("fargateProfile.spec.subnets")},
				{ID: "fargateProfile", Path: fieldpath.MustParse("fargateProfile.spec.selectors")},
				{ID: "eksCluster", Path: fieldpath.MustParse("eksCluster.status.state")},
			},
			wantFunctions: []FunctionCall{
				{Name: "contains"},
				{Name: "count"},
			},
		},
		{
			name:      "complex security group validation",
			resources: []string{"securityGroup", "vpc"},
			functions: []string{"concat", "contains", "map"},
			expression: `securityGroup.spec.vpcID == vpc.status.vpcID &&
                securityGroup.spec.rules.all(r,
                    contains(map(r.ipRanges, range, concat(range.cidr, "/", range.description)),
                        "0.0.0.0/0"))`,
			wantResources: []ResourceDependency{
				{ID: "securityGroup", Path: fieldpath.MustParse("securityGroup.spec.vpcID")},
				{ID: "securityGroup", Path: fieldpath.MustParse("securityGroup.spec.rules")},
				{ID: "vpc", Path: fieldpath.MustParse("vpc.status.vpcID")},
			},
			wantFunctions: []FunctionCall{
				{Name: "concat"},
				{Name: "contains"},
				{Name: "map"}, // first map is for rules
				{Name: "map"}, // second map is for ipRanges
			},
		},
		{
			name:      "eks cluster validation",
			resources: []string{"eksCluster", "nodeGroups", "iamRole", "vpc"},
			functions: []string{"filter", "contains", "timeAgo"}, // duration and size are a built-in function
			expression: `eksCluster.status.state == "ACTIVE" &&
				duration(timeAgo(eksCluster.status.createdAt)) > duration("24h") &&
				size(nodeGroups.filter(ng,
					ng.status.state == "ACTIVE" &&
					contains(ng.labels, "environment"))) >= 1 &&
				contains(map(iamRole.policies, p, p.actions), "eks:*") &&
				size(vpc.subnets.filter(s, s.isPrivate)) >= 2`,
			wantResources: []ResourceDependency{
				{ID: "eksCluster", Path: fieldpath.MustParse("eksCluster.status.state")},
				{ID: "eksCluster", Path: fieldpath.MustParse("eksCluster.status.createdAt")},
				{ID: "nodeGroups", Path: fieldpath.MustParse("nodeGroups")},
				{ID: "iamRole", Path: fieldpath.MustParse("iamRole.policies")},
				{ID: "vpc", Path: fieldpath.MustParse("vpc.subnets")},
			},
			wantFunctions: []FunctionCall{
				{Name: "contains"},
				{Name: "contains"},
				{Name: "map"},
				{Name: "map"},
				{Name: "timeAgo"},
				// built-in functions don't appear in the function list
			},
		},
		{
			name:      "validate order and inventory",
			resources: []string{"order", "product", "customer", "inventory"},
			functions: []string{"validateAddress", "calculateTax"},
			expression: `order.total > 0 &&
				order.items.all(item,
					product.id == item.productId &&
					inventory.stock[item.productId] >= item.quantity
				) &&
				validateAddress(customer.shippingAddress) &&
				calculateTax(order.total, customer.address.zipCode) > 0 || true`,
			wantResources: []ResourceDependency{
				{ID: "order", Path: fieldpath.MustParse("order.total")},
				{ID: "order", Path: fieldpath.MustParse("order.total")},
				{ID: "order", Path: fieldpath.MustParse("order.items")},
				{ID: "product", Path: fieldpath.MustParse("product.id")},
				{ID: "inventory", Path: fieldpath.MustParse("inventory.stock")},
				{ID: "customer", Path: fieldpath.MustParse("customer.shippingAddress")},
				{ID: "customer", Path: fieldpath.MustParse("customer.address.zipCode")},
			},
			wantFunctions: []FunctionCall{
				{Name: "validateAddress"},
				{Name: "map"},
				{Name: "calculateTax"},
			},
		},
		{
			name:       "filter with explicit condition",
			resources:  []string{"pods"},
			functions:  []string{},
			expression: `pods.filter(p, p.status == "Running")`,
			wantResources: []ResourceDependency{
				{ID: "pods", Path: fieldpath.MustParse("pods")},
			},
			wantFunctions: []FunctionCall{
				{Name: "map"},
			},
		},
		{
			name:          "create message struct",
			resources:     []string{},
			functions:     []string{"createPod"},
			expression:    `createPod(Pod{metadata: {name: "test", labels: {"app": "web"}}, spec: {containers: [{name: "main", image: "nginx"}]}})`,
			wantResources: nil,
			wantFunctions: []FunctionCall{
				{Name: "createPod"},
			},
		},
		{
			name:      "create map with different key types",
			resources: []string{},
			functions: []string{"processMap"},
			expression: `processMap({
				"string-key": 123,
				42: "number-key",
				true: "bool-key"
			})`,
			wantResources: nil,
			wantFunctions: []FunctionCall{
				{Name: "processMap"},
			},
		},
		{
			name:      "message with nested structs",
			resources: []string{},
			functions: []string{"validate"},
			expression: `validate(Container{
				resource: Resource{cpu: "100m", memory: "256Mi"},
				env: {
					"DB_HOST": "localhost",
					"DB_PORT": "5432"
				}
			})`,
			wantResources: nil,
			wantFunctions: []FunctionCall{
				{Name: "validate"},
			},
		},
		{
			name:       "simple optional check",
			resources:  []string{"bucket"},
			expression: `bucket.?spec.name == "my-bucket"`,
			wantResources: []ResourceDependency{
				// for optionals, we can only depend on the known object, not on the path thereafter (as its optional)
				{ID: "bucket", Path: fieldpath.MustParse("bucket")},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var opts []cel.EnvOption
			for _, function := range tt.functions {
				opts = append(opts, cel.Function(function))
			}
			for _, resource := range tt.resources {
				opts = append(opts, cel.Variable(resource, cel.DynType))
			}

			env, err := cel.NewEnv(cel.OptionalTypes())
			if err != nil {
				t.Fatalf("Failed to create CEL environment: %v", err)
			}

			inspector := NewInspectorWithEnv(env, tt.resources, tt.functions)

			got, err := inspector.Inspect(tt.expression)
			if err != nil {
				t.Fatalf("Inspect() error = %v", err)
			}

			// Sort for stable comparison
			sortDependencies := func(i, j ResourceDependency) int {
				return fieldpath.Compare(i.Path, j.Path)
			}

			sortFunctions := func(funcs []FunctionCall) {
				sort.Slice(funcs, func(i, j int) bool {
					return funcs[i].Name < funcs[j].Name
				})
			}

			slices.SortFunc(got.ResourceDependencies, sortDependencies)
			slices.SortFunc(tt.wantResources, sortDependencies)
			sortFunctions(got.FunctionCalls)
			sortFunctions(tt.wantFunctions)

			if !reflect.DeepEqual(got.ResourceDependencies, tt.wantResources) {
				t.Errorf("ResourceDependencies = %v, want %v", got.ResourceDependencies, tt.wantResources)
			}

			// Only check function names, not arguments
			gotFuncNames := make([]string, len(got.FunctionCalls))
			wantFuncNames := make([]string, len(tt.wantFunctions))
			for i, f := range got.FunctionCalls {
				gotFuncNames[i] = f.Name
			}
			for i, f := range tt.wantFunctions {
				wantFuncNames[i] = f.Name
			}
			sort.Strings(gotFuncNames)
			sort.Strings(wantFuncNames)

			if !reflect.DeepEqual(gotFuncNames, wantFuncNames) {
				t.Errorf("Function names = %v, want %v", gotFuncNames, wantFuncNames)
			}
		})
	}
}

func TestInspector_UnknownResourcesAndCalls(t *testing.T) {
	tests := []struct {
		name           string
		resources      []string
		functions      []string
		expression     string
		wantResources  []ResourceDependency
		wantFunctions  []FunctionCall
		wantUnknownRes []UnknownResource
	}{
		{
			name:          "method call on unknown resource",
			resources:     []string{"list"},
			expression:    `unknownResource.someMethod(42)`,
			wantResources: nil,
			wantFunctions: []FunctionCall{
				{Name: "unknownResource.someMethod"},
			},
			wantUnknownRes: []UnknownResource{
				{ID: "unknownResource", Path: fieldpath.MustParse("unknownResource")},
			},
		},
		{
			name:          "chained method calls on unknown resource",
			resources:     []string{},
			expression:    `unknown.method1().method2(123)`,
			wantResources: nil,
			wantFunctions: []FunctionCall{
				{Name: "unknown.method1"},
				{Name: "unknown.method1().method2"},
			},
			wantUnknownRes: []UnknownResource{
				{ID: "unknown", Path: fieldpath.MustParse("unknown")},
			},
		},
		{
			name:      "filter with multiple conditions",
			resources: []string{"instances"},
			// note that `i` is not declared as a resource, but it's not an unknown resource
			// either, it's a loop variable.
			expression: `instances.filter(i,
                i.state == 'running' &&
                i.type == 't2.micro'
            )`,
			wantResources: []ResourceDependency{
				{ID: "instances", Path: fieldpath.MustParse("instances")},
			},
			wantFunctions: []FunctionCall{
				{Name: "map"},
			},
		},
		{
			name:      "ambiguous i usage - both resource and loop var",
			resources: []string{"instances", "i"}, // 'i' is a declared resource
			expression: `i.status == "ready" &&
				instances.filter(i,   // reusing 'i' in filter
					i.state == 'running'
				)`,
			wantResources: []ResourceDependency{
				{ID: "i", Path: fieldpath.MustParse("i.status")},
				{ID: "instances", Path: fieldpath.MustParse("instances")},
			},
			wantFunctions: []FunctionCall{
				{Name: "map"},
			},
			wantUnknownRes: nil,
		},
		{
			name:       "test target function chaining",
			resources:  []string{"bucket"},
			functions:  []string{"processItems", "validate"},
			expression: `processItems(bucket).validate()`,
			wantResources: []ResourceDependency{
				{ID: "bucket", Path: fieldpath.MustParse("bucket")},
			},
			wantFunctions: []FunctionCall{
				{Name: "processItems"},
				{Name: "processItems(bucket).validate"},
			},
		},
		{
			name:          "test unknown function with target",
			resources:     []string{},
			functions:     []string{},
			expression:    `result.unknownFn().anotherUnknownFn()`,
			wantResources: nil,
			wantFunctions: []FunctionCall{
				{Name: "result.unknownFn"},
				{Name: "result.unknownFn().anotherUnknownFn"},
			},
			wantUnknownRes: []UnknownResource{
				{ID: "result", Path: fieldpath.MustParse("result")},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var opts []cel.EnvOption
			for _, function := range tt.functions {
				opts = append(opts, cel.Function(function))
			}
			for _, resource := range tt.resources {
				opts = append(opts, cel.Variable(resource, cel.DynType))
			}

			env, err := cel.NewEnv()
			if err != nil {
				t.Fatalf("Failed to create CEL environment: %v", err)
			}

			inspector := NewInspectorWithEnv(env, tt.resources, tt.functions)

			got, err := inspector.Inspect(tt.expression)
			if err != nil {
				t.Fatalf("Inspect() error = %v", err)
			}

			// Sort for stable comparison
			sortDependencies := func(i, j ResourceDependency) int {
				return fieldpath.Compare(i.Path, j.Path)
			}

			sortFunctions := func(i, j FunctionCall) int {
				return strings.Compare(i.Name, j.Name)
			}

			sortUnknownResources := func(i, j UnknownResource) int {
				return fieldpath.Compare(i.Path, j.Path)
			}

			slices.SortFunc(got.ResourceDependencies, sortDependencies)
			slices.SortFunc(tt.wantResources, sortDependencies)
			slices.SortFunc(tt.wantUnknownRes, sortUnknownResources)
			slices.SortFunc(tt.wantFunctions, sortFunctions)
			slices.SortFunc(got.UnknownResources, sortUnknownResources)
			slices.SortFunc(tt.wantUnknownRes, sortUnknownResources)

			if !reflect.DeepEqual(got.ResourceDependencies, tt.wantResources) {
				t.Errorf("ResourceDependencies = %v, want %v", got.ResourceDependencies, tt.wantResources)
			}

			// Only check function names, not arguments
			gotFuncNames := make([]string, len(got.FunctionCalls))
			wantFuncNames := make([]string, len(tt.wantFunctions))
			for i, f := range got.FunctionCalls {
				gotFuncNames[i] = f.Name
			}
			for i, f := range tt.wantFunctions {
				wantFuncNames[i] = f.Name
			}
			sort.Strings(gotFuncNames)
			sort.Strings(wantFuncNames)

			if !reflect.DeepEqual(gotFuncNames, wantFuncNames) {
				t.Errorf("Function names = %v, want %v", gotFuncNames, wantFuncNames)
			}

			if !reflect.DeepEqual(got.UnknownResources, tt.wantUnknownRes) {
				t.Errorf("UnknownResources = %v, want %v", got.UnknownResources, tt.wantUnknownRes)
			}
		})
	}
}

func Test_InvalidExpression(t *testing.T) {
	env, err := cel.NewEnv()
	if err != nil {
		t.Fatalf("Failed to create CEL environment: %v", err)
	}
	inspector := NewInspectorWithEnv(env, nil, nil)
	_, err = inspector.Inspect("invalid expression ######")
	if err == nil {
		t.Errorf("Expected error")
	}
}
