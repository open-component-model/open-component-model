package variable

import (
	"slices"

	"github.com/google/cel-go/cel"
	"ocm.software/open-component-model/bindings/go/cel/expression/fieldpath"
)

// FieldDescriptor represents a field that contains CEL expressions in it. It
// contains the path of the field in the resource, the CEL expressions
// and the expected type of the field. The field may contain multiple
// expressions.
type FieldDescriptor struct {
	// Path is the path to the field in a nested object.
	Path fieldpath.Path

	// Expressions is a list of CEL expressions in the field.
	Expressions []Expression

	// ExpectedType is the expected CEL type of the field.
	// Set by: builder.setExpectedTypeOnDescriptor() - the single place where types are determined.
	// Parser leaves this nil, builder sets it based on StandaloneExpression:
	//   - For string templates (StandaloneExpression=false): always cel.StringType
	//   - For standalone expressions (StandaloneExpression=true): derived from a schema
	ExpectedType *cel.Type

	// StandaloneExpression indicates if this is a single CEL expression vs a string template.
	// Set by: parser (both parser.go and schemaless.go)
	// Used by: builder.setExpectedTypeOnDescriptor() to determine how to set ExpectedType
	// Examples:
	//   true:  "${foo}" - single expression, type derived from schema
	//   false: "hello-${foo}" or "${foo}-${bar}" - string template, always produces string
	StandaloneExpression bool
}

// Expression represents a CEL expression string and its potentially parsed AST.
type Expression struct {
	// Value is the CEL expression string.
	Value string
	// AST is the parsed CEL AST of the expression.
	// This can be nil if the expression has not been parsed yet.
	// If the expression is parsed, this AST can be used for further analysis
	AST *cel.Ast
}

func (e Expression) String() string {
	return e.Value
}

// Variable is any field that is not a constant
// value a.k.a contains one or multiple expressions. For example
//
//	spec:
//	  replicas: ${schema.spec.mycustomReplicasField + 5}
//
// Contains a variable named "spec.mycustomReplicasField". Variables can be
// static or dynamic. Static variables are resolved at the beginning of the
// execution and their value is constant. Dynamic variables are resolved at
// runtime and their value can change during the execution.
type Variable struct {
	// FieldDescriptor is the object that contains the expression, the path, and
	// the expected type.
	FieldDescriptor
	// Kind is the kind of the variable (static or dynamic).
	Kind Kind
	// Dependencies is a list of resources this variable depends on. We need
	// this information to wait for the dependencies to be resolved before
	// evaluating the variable.
	Dependencies []string
}

// AddDependencies adds dependencies to the Variable.
func (rv *Variable) AddDependencies(dep ...string) {
	for _, d := range dep {
		if !slices.Contains(rv.Dependencies, d) {
			rv.Dependencies = append(rv.Dependencies, d)
		}
	}
}

// Kind represents the kind of variable.
type Kind string

const (
	// KindStatic represents a static variable. Static variables
	// are resolved at the beginning of the execution and their value is constant.
	// Static variables are easy to find, they always start with 'spec'. Referring
	// to the instance spec.
	//
	// For example:
	//   spec:
	//      replicas: ${schema.spec.replicas + 5}
	KindStatic Kind = "static"
	// KindDynamic represents a dynamic variable. Dynamic variables
	// are resolved at runtime and their value can change during the execution. Dynamic
	// cannot start with 'spec' and they must refer to another resource in the
	// ResourceGraphDefinition.
	//
	// For example:
	//    spec:
	//	    vpcID: ${vpc.status.vpcID}
	KindDynamic Kind = "dynamic"
)

// String returns the string representation of a Kind.
func (r Kind) String() string {
	return string(r)
}

// IsStatic returns true if the Kind is static
func (r Kind) IsStatic() bool {
	return r == KindStatic
}

// IsDynamic returns true if the Kind is dynamic
func (r Kind) IsDynamic() bool {
	return r == KindDynamic
}
