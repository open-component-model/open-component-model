package handler

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"

	"ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
)

// signRSA signs dig using the requested RSA algorithm and hash.
func signRSA(algorithm string, priv *rsa.PrivateKey, h crypto.Hash, dig []byte) ([]byte, error) {
	switch algorithm {
	case v1alpha1.AlgorithmRSASSAPSS:
		return rsa.SignPSS(rand.Reader, priv, h, dig, nil)
	case v1alpha1.AlgorithmRSASSAPKCS1V15:
		return rsa.SignPKCS1v15(rand.Reader, priv, h, dig)
	default:
		return nil, ErrInvalidAlgorithm
	}
}

// verifyRSA verifies sig over dig using the requested RSA algorithm and hash.
func verifyRSA(algorithm string, pub *rsa.PublicKey, h crypto.Hash, dig, sig []byte) error {
	switch algorithm {
	case v1alpha1.AlgorithmRSASSAPSS:
		return rsa.VerifyPSS(pub, h, dig, sig, nil)
	case v1alpha1.AlgorithmRSASSAPKCS1V15:
		return rsa.VerifyPKCS1v15(pub, h, dig, sig)
	default:
		return ErrInvalidAlgorithm
	}
}
