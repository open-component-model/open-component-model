package ocm

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"ocm.software/ocm/api/ocm"
	"ocm.software/ocm/api/ocm/resolvers"
	"ocm.software/ocm/api/ocm/tools/signing"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
)

// Verification is an internal representation of v1alpha1.Verification where the public key is already extracted from
// the value or secret.
type Verification struct {
	Signature string
	PublicKey []byte
}

func GetVerifications(ctx context.Context, client ctrl.Reader,
	obj v1alpha1.VerificationProvider,
) ([]Verification, error) {
	verifications := obj.GetVerifications()

	var err error
	var secret corev1.Secret
	v := make([]Verification, 0, len(verifications))
	for _, verification := range verifications {
		internal := Verification{
			Signature: verification.Signature,
		}
		if verification.Value == "" && verification.SecretRef.Name == "" {
			return nil, reconcile.TerminalError(fmt.Errorf("value and secret ref cannot both be empty for signature: %s", verification.Signature))
		}
		if verification.Value != "" && verification.SecretRef.Name != "" {
			return nil, reconcile.TerminalError(fmt.Errorf("value and secret ref cannot both be set for signature: %s", verification.Signature))
		}
		if verification.Value != "" {
			internal.PublicKey, err = base64.StdEncoding.DecodeString(verification.Value)
			if err != nil {
				return nil, err
			}
		}
		if verification.SecretRef.Name != "" {
			err = client.Get(ctx, ctrl.ObjectKey{Namespace: obj.GetNamespace(), Name: verification.SecretRef.Name}, &secret)
			if err != nil {
				return nil, err
			}
			if certBytes, ok := secret.Data[verification.Signature]; ok {
				internal.PublicKey = certBytes
			}
		}

		v = append(v, internal)
	}

	return v, nil
}

func VerifyComponentVersion(ctx context.Context, cv ocm.ComponentVersionAccess, resolver ocm.ComponentVersionResolver, sigs []string) (*Descriptors, error) {
	logger := log.FromContext(ctx).WithName("signature-validation")

	if len(sigs) == 0 || cv == nil {
		logger.V(1).Info("no signatures passed, skipping validation")

		return nil, nil
	}
	opts := signing.NewOptions(
		signing.Resolver(resolver),
		// TODO: Consider configurable options for digest verification (@frewilhelm @fabianburth)
		//   https://github.com/open-component-model/ocm-k8s-toolkit/issues/208
		// do we really want to verify the digests here? isn't it sufficient to verify the signatures since
		// the digest verification can and has to be done anyways by the resource controller?
		// signing.VerifyDigests(),
		signing.VerifySignature(sigs...),
		signing.Recursive(),
	)

	ws := signing.DefaultWalkingState(cv.GetContext())
	_, err := signing.Apply(nil, ws, cv, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to verify component signatures %s: %w", strings.Join(sigs, ", "), err)
	}
	logger.Info("successfully verified component signature")

	return &Descriptors{List: signing.ListComponentDescriptors(cv, ws)}, nil
}

func VerifyComponentVersionAndListDescriptors(ctx context.Context, cv ocm.ComponentVersionAccess, resolver ocm.ComponentVersionResolver, sigs []string) (*Descriptors, error) {
	descriptors, err := VerifyComponentVersion(ctx, cv, resolver, sigs)
	if err != nil {
		return nil, fmt.Errorf("failed to verify component: %w", err)
	}
	return descriptors, nil
}

func NewSessionResolver(ocmctx ocm.Context, session ocm.Session) resolvers.ComponentVersionResolver {
	return &sessionResolver{ocmctx, session}
}

type sessionResolver struct {
	ocm.Context
	ocm.Session
}

func (s sessionResolver) LookupComponentVersion(name string, version string) (ocm.ComponentVersionAccess, error) {
	return s.Session.LookupComponentVersion(s.GetResolver(), name, version)
}

var _ resolvers.ComponentVersionResolver = (*sessionResolver)(nil)
