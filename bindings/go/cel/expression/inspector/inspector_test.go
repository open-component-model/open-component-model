package ast

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/ext"
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
				{Name: "toLower", Arguments: []string{"bucket.name"}},
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
				{Name: "max", Arguments: []string{"deployment.spec.replicas", "5"}},
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
				{Name: "process", Arguments: []string{`b"\142\171\164\145\163\061\062\063"`, "3.14", "42u", "null"}},
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
					"(conditions.ready || (conditions.initialized && list[3]))",
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
				{Name: "concat", Arguments: []string{
					"toLower(eksCluster.spec.name)",
					`"-"`,
					"bucket.spec.name",
				}},
				{Name: "toLower", Arguments: []string{"eksCluster.spec.name"}},
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
				{Name: "count", Arguments: []string{"instances"}},
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
				{Name: "contains", Arguments: []string{"fargateProfile.spec.subnets", `"subnet-123"`}},
				{Name: "count", Arguments: []string{"fargateProfile.spec.selectors"}},
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
				{Name: "concat", Arguments: []string{"range.cidr", `"/"`, "range.description"}},
				{Name: "contains", Arguments: []string{
					"map(r.ipRanges, range, concat(range.cidr, \"/\", range.description))",
					`"0.0.0.0/0"`,
				}},
				{Name: "map", Arguments: []string{"r.ipRanges", "range", "concat(range.cidr, \"/\", range.description)"}},
				{Name: "filter", Arguments: []string{
					"securityGroup.spec.rules",
					"(@result && contains(map(r.ipRanges, range, concat(range.cidr, \"/\", range.description)), \"0.0.0.0/0\"))",
					"@result",
				}},
			},
		},
		{
			name:      "eks cluster validation",
			resources: []string{"eksCluster", "nodeGroups", "iamRole", "vpc"},
			functions: []string{"filter", "contains", "timeAgo"},
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
				{Name: "contains", Arguments: []string{"ng.labels", `"environment"`}},
				{Name: "contains", Arguments: []string{"map(iamRole.policies, p, p.actions)", `"eks:*"`}},
				{Name: "createList", Arguments: []string{"[ng]"}},
				{Name: "createList", Arguments: []string{"[s]"}},
				{Name: "filter", Arguments: []string{
					"nodeGroups",
					"(((ng.status.state == \"ACTIVE\") && contains(ng.labels, \"environment\")) ? (@result + [ng]) : @result)",
					"@result",
				}},
				{Name: "filter", Arguments: []string{
					"vpc.subnets",
					"(s.isPrivate ? (@result + [s]) : @result)",
					"@result",
				}},
				{Name: "timeAgo", Arguments: []string{"eksCluster.status.createdAt"}},
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
				{Name: "calculateTax", Arguments: []string{"order.total", "customer.address.zipCode"}},
				{Name: "filter", Arguments: []string{
					"order.items",
					"(@result && ((product.id == item.productId) && (inventory.stock[item.productId] >= item.quantity)))",
					"@result",
				}},
				{Name: "validateAddress", Arguments: []string{"customer.shippingAddress"}},
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
				{Name: "createList", Arguments: []string{"[p]"}},
				{Name: "filter", Arguments: []string{
					"pods",
					"((p.status == \"Running\") ? (@result + [p]) : @result)",
					"@result",
				}},
			},
		},
		{
			name:          "create message struct",
			resources:     []string{},
			functions:     []string{"createPod"},
			expression:    `createPod(Pod{metadata: {name: "test", labels: {"app": "web"}}, spec: {containers: [{name: "main", image: "nginx"}]}})`,
			wantResources: nil,
			wantFunctions: []FunctionCall{
				{Name: "createList", Arguments: []string{`[{name: "main", image: "nginx"}]`}},
				{Name: "createPod", Arguments: []string{
					`Pod{metadata: {name: "test", labels: {"app": "web"}}, spec: {containers: [{name: "main", image: "nginx"}]}}`,
				}},
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
				{Name: "processMap", Arguments: []string{
					`{"string-key": 123, 42: "number-key", true: "bool-key"}`,
				}},
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
				{Name: "validate", Arguments: []string{
					`Container{resource: Resource{cpu: "100m", memory: "256Mi"}, env: {"DB_HOST": "localhost", "DB_PORT": "5432"}}`,
				}},
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
		{
			name:       "format statement powered by list",
			resources:  []string{"serviceAccount", "configMap", "schema"},
			expression: `"%s:%s".format([schema.metadata.namespace, serviceAccount.metadata.name])`,
			wantFunctions: []FunctionCall{
				{Name: `"%s:%s".format`},
				{Name: "createList", Arguments: []string{"[schema.metadata.namespace, serviceAccount.metadata.name]"}},
			},
			wantResources: []ResourceDependency{
				{ID: "serviceAccount", Path: fieldpath.MustParse("serviceAccount.metadata.name")},
				{ID: "schema", Path: fieldpath.MustParse("schema.metadata.namespace")},
			},
		},
		{
			name:       "simple list literal",
			resources:  []string{},
			expression: `[1, 2, 3]`,
			wantFunctions: []FunctionCall{
				{Name: "createList", Arguments: []string{"[1, 2, 3]"}},
			},
		},

		{
			name:       "nested list literal",
			resources:  []string{},
			expression: `[[1, 2], ["a", "b"]]`,
			wantFunctions: []FunctionCall{
				{Name: "createList", Arguments: []string{"[1, 2]"}},
				{Name: "createList", Arguments: []string{"[\"a\", \"b\"]"}},
				{Name: "createList", Arguments: []string{"[[1, 2], [\"a\", \"b\"]]"}},
			},
		},

		{
			name:       "list with struct elements",
			resources:  []string{},
			expression: `[{a: 1}, {b: 2}]`,
			wantFunctions: []FunctionCall{
				{Name: "createList", Arguments: []string{"[{a: 1}, {b: 2}]"}},
			},
		},

		{
			name:       "list containing function calls",
			resources:  []string{},
			functions:  []string{"toLower", "hash"},
			expression: `[toLower("A"), hash("x")]`,
			wantFunctions: []FunctionCall{
				{Name: "toLower", Arguments: []string{`"A"`}},
				{Name: "hash", Arguments: []string{`"x"`}},
				{Name: "createList", Arguments: []string{"[toLower(\"A\"), hash(\"x\")]"}},
			},
		},
		{
			name:       "list with optional access",
			resources:  []string{"bucket"},
			expression: `[bucket.?spec.name, "x"]`,
			wantResources: []ResourceDependency{
				{ID: "bucket", Path: fieldpath.MustParse("bucket")},
			},
			wantFunctions: []FunctionCall{
				{
					Name:      "createList",
					Arguments: []string{`[_?._(bucket, "spec").name, "x"]`},
				},
			},
		},
		{
			name:       "list inside binary operator",
			resources:  []string{"cfg"},
			expression: `size([cfg.a, cfg.b]) > 1`,
			// no "size" – CEL already provides it
			functions: []string{},
			wantResources: []ResourceDependency{
				{ID: "cfg", Path: fieldpath.MustParse("cfg.a")},
				{ID: "cfg", Path: fieldpath.MustParse("cfg.b")},
			},
			wantFunctions: []FunctionCall{
				// size() will NOT be recorded, because it’s built-in
				{Name: "createList", Arguments: []string{"[cfg.a, cfg.b]"}},
			},
		},
		{
			name:       "map with resources",
			resources:  []string{"pod"},
			expression: `{"name": pod.metadata.name}`,
			wantResources: []ResourceDependency{
				{ID: "pod", Path: fieldpath.MustParse("pod.metadata.name")},
			},
		},
		{
			name:       "struct with resources",
			resources:  []string{"deployment"},
			expression: `Config{replicas: deployment.spec.replicas}`,
			wantResources: []ResourceDependency{
				{ID: "deployment", Path: fieldpath.MustParse("deployment.spec.replicas")},
			},
		},
		{
			name:       "nested maps with resources",
			resources:  []string{"app", "db"},
			expression: `{"app": {"name": app.metadata.name, "version": app.spec.version}, "db": {"host": db.spec.host}}`,
			wantResources: []ResourceDependency{
				{ID: "app", Path: fieldpath.MustParse("app.metadata.name")},
				{ID: "app", Path: fieldpath.MustParse("app.spec.version")},
				{ID: "db", Path: fieldpath.MustParse("db.spec.host")},
			},
		},
		{
			name:       "map with dynamic key from resource",
			resources:  []string{"config"},
			expression: `{config.metadata.name: config.spec.value}`,
			wantResources: []ResourceDependency{
				{ID: "config", Path: fieldpath.MustParse("config.metadata.name")},
				{ID: "config", Path: fieldpath.MustParse("config.spec.value")},
			},
		},
		{
			name:       "map inside list",
			resources:  []string{"svc"},
			expression: `[{"port": svc.spec.ports[0].port}]`,
			wantResources: []ResourceDependency{
				{ID: "svc", Path: fieldpath.MustParse("svc.spec.ports")},
			},
			wantFunctions: []FunctionCall{
				{Name: "createList", Arguments: []string{`[{"port": svc.spec.ports[0].port}]`}},
			},
		},
		{
			name:       "map with function calls in values",
			resources:  []string{"user"},
			functions:  []string{"toLower", "hash"},
			expression: `{"username": toLower(user.name), "hash": hash(user.password)}`,
			wantResources: []ResourceDependency{
				{ID: "user", Path: fieldpath.MustParse("user.name")},
				{ID: "user", Path: fieldpath.MustParse("user.password")},
			},
			wantFunctions: []FunctionCall{
				{Name: "hash", Arguments: []string{"user.password"}},
				{Name: "toLower", Arguments: []string{"user.name"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inspector, err := testInspector(tt.resources, tt.functions)
			if err != nil {
				t.Fatalf("Failed to create inspector: %v", err)
			}

			got, err := inspector.Inspect(tt.expression)
			if err != nil {
				t.Fatalf("Inspect() error = %v", err)
			}

			sortDependencies := func(a, b ResourceDependency) int {
				return fieldpath.Compare(a.Path, b.Path)
			}
			sortFunctions := func(a, b FunctionCall) int {
				return strings.Compare(a.Name, b.Name)
			}

			slices.SortFunc(got.ResourceDependencies, sortDependencies)
			slices.SortFunc(tt.wantResources, sortDependencies)
			if !reflect.DeepEqual(got.ResourceDependencies, tt.wantResources) {
				t.Errorf("ResourceDependencies = %v, want %v", got.ResourceDependencies, tt.wantResources)
			}

			slices.SortFunc(got.FunctionCalls, sortFunctions)
			slices.SortFunc(tt.wantFunctions, sortFunctions)
			if !reflect.DeepEqual(got.FunctionCalls, tt.wantFunctions) {
				t.Errorf("FunctionCalls = %v, want %v", got.FunctionCalls, tt.wantFunctions)
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
				{Name: "createList"},
				{Name: "filter"},
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
				{Name: "createList"},
				{Name: "filter"},
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
			var opts = []cel.EnvOption{
				ext.Lists(),
				ext.Strings(),
				cel.OptionalTypes(),
				ext.Encoders(),
			}
			for _, resource := range tt.resources {
				opts = append(opts, cel.Variable(resource, cel.DynType))
			}

			for _, fn := range tt.functions {
				opts = append(opts, cel.Function(fn, cel.Overload(fn+"_any", []*cel.Type{cel.AnyType}, cel.AnyType)))
			}

			env, err := cel.NewEnv(opts...)
			if err != nil {
				t.Fatalf("Failed to create CEL environment: %v", err)
			}

			inspector := NewInspectorWithEnv(env, tt.resources)

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
	inspector := NewInspectorWithEnv(env, nil)
	_, err = inspector.Inspect("invalid expression ######")
	if err == nil {
		t.Errorf("Expected error")
	}
}

func testInspector(resources []string, functions []string) (*Inspector, error) {
	decls := make([]cel.EnvOption, 0, len(resources)+len(functions))
	decls = append(decls, cel.OptionalTypes())
	resourceMap := make(map[string]struct{})
	functionMap := make(map[string]struct{})

	for _, r := range resources {
		decls = append(decls, cel.Variable(r, cel.AnyType))
		resourceMap[r] = struct{}{}
	}

	for _, fn := range functions {
		decls = append(decls, cel.Function(fn,
			cel.Overload(fn+"_any", []*cel.Type{cel.AnyType}, cel.AnyType)))
		functionMap[fn] = struct{}{}
	}

	env, err := cel.NewEnv(decls...)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %v", err)
	}

	return &Inspector{
		env:       env,
		resources: resourceMap,
		functions: functionMap,
		loopVars:  make(map[string]struct{}),
	}, nil
}
