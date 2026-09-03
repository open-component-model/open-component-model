package status

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IdentifiableClientObject defines an object which can create an identity for itself.
type IdentifiableClientObject interface {
	client.Object
	ConditionObject
	Mutator

	// GetVID constructs an identifier for an object.
	GetVID() map[string]string
}

// Mutator allows mutating specific status fields of an object.
type Mutator interface {
	// SetObservedGeneration mutates the observed generation field of an object.
	SetObservedGeneration(v int64)
}
