package handler

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"fmt"

	"ocm.software/open-component-model/bindings/go/ecdsa/signing/v1alpha1"
)

// signECDSA signs dig using the ECDSA algorithm, validating that the key's curve
// matches the requested algorithm.
func signECDSA(algorithm v1alpha1.SignatureAlgorithm, priv *ecdsa.PrivateKey, dig []byte) ([]byte, error) {
	if err := validateCurve(algorithm, priv.Curve); err != nil {
		return nil, err
	}
	return ecdsa.SignASN1(rand.Reader, priv, dig)
}

// verifyECDSA verifies sig over dig using the ECDSA algorithm, validating that
// the key's curve matches the requested algorithm.
func verifyECDSA(algorithm v1alpha1.SignatureAlgorithm, pub *ecdsa.PublicKey, dig, sig []byte) error {
	if err := validateCurve(algorithm, pub.Curve); err != nil {
		return err
	}
	if !ecdsa.VerifyASN1(pub, dig, sig) {
		return errors.New("ecdsa: verification error")
	}
	return nil
}

// validateCurve checks that the key's curve matches the algorithm's expected curve.
func validateCurve(algorithm v1alpha1.SignatureAlgorithm, curve elliptic.Curve) error {
	expected, err := curveForAlgorithm(algorithm)
	if err != nil {
		return err
	}
	if curve != expected {
		return fmt.Errorf("key curve %s does not match algorithm %s (expected %s)",
			curve.Params().Name, algorithm, expected.Params().Name)
	}
	return nil
}

// curveForAlgorithm returns the elliptic curve for the given algorithm.
func curveForAlgorithm(algorithm v1alpha1.SignatureAlgorithm) (elliptic.Curve, error) {
	switch algorithm {
	case v1alpha1.AlgorithmECDSAP256:
		return elliptic.P256(), nil
	case v1alpha1.AlgorithmECDSAP384:
		return elliptic.P384(), nil
	case v1alpha1.AlgorithmECDSAP521:
		return elliptic.P521(), nil
	default:
		return nil, ErrInvalidAlgorithm
	}
}
