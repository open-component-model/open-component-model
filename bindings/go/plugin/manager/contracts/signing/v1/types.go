package v1

import (
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type SignRequest[T runtime.Typed] struct {
	// Digest that should be signed.
	Digest *v2.Digest `json:"digest"`
	Config T          `json:"config"`
}
type SignResponse struct {
	Signature *v2.SignatureInfo `json:"signature"`
}

type VerifyRequest[T runtime.Typed] struct {
	Signature *v2.Signature `json:"signature"`
	Config    T             `json:"config"`
}

type VerifyResponse struct{}

type GetSignerIdentityRequest[T runtime.Typed] struct {
	SignRequest[T]
	Name string `json:"name"`
}

type GetVerifierIdentityRequest[T runtime.Typed] VerifyRequest[T]

type IdentityResponse struct {
	Identity map[string]string `json:"identity"`
}
