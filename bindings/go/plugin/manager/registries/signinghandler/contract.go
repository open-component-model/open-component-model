package signinghandler

import (
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
)

type BuiltinSigningHandler interface {
	signing.Handler
	GetSigningHandlerScheme() *runtime.Scheme
}
