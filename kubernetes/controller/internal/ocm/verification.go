package ocm

import (
	"context"
	"encoding/base64"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
)

// Verification is an internal representation of v1alpha1.Verification where the public key is already extracted from
// the value or secret.
type Verification struct {
	Signature string `json:"signature"`
	PublicKey []byte `json:"publicKey"`
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
