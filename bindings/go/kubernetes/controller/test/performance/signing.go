package main

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	rsahandler "ocm.software/open-component-model/bindings/go/rsa/signing/handler"
	rsav1alpha1 "ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
	rsacredentialsv1 "ocm.software/open-component-model/bindings/go/rsa/spec/credentials/v1"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// signatureName is the signature the benchmark signs with and the controller verifies.
	signatureName  = "perf"
	signingKeyBits = 2048
)

// signingHash is the hash of every digest the benchmark writes, as `ocm sign cv` defaults to.
var signingHash = crypto.SHA256.String()

// signer signs component versions with a key generated for one run.
type signer struct {
	handler      *rsahandler.Handler
	config       *rsav1alpha1.Config
	credentials  *rsacredentialsv1.RSACredentials
	publicKeyPEM string
}

func newSigner() (*signer, error) {
	key, err := rsa.GenerateKey(rand.Reader, signingKeyBits)
	if err != nil {
		return nil, fmt.Errorf("generating signing key: %w", err)
	}
	handler, err := rsahandler.New(ocmruntime.NewScheme(), false)
	if err != nil {
		return nil, fmt.Errorf("creating RSA signing handler: %w", err)
	}
	return &signer{
		handler: handler,
		config: &rsav1alpha1.Config{
			Type:               ocmruntime.NewVersionedType(rsav1alpha1.ConfigType, rsav1alpha1.Version),
			SignatureAlgorithm: rsav1alpha1.AlgorithmRSASSAPSS,
		},
		credentials: &rsacredentialsv1.RSACredentials{
			Type: rsacredentialsv1.VersionedType,
			PrivateKeyPEM: string(pem.EncodeToMemory(&pem.Block{
				Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key),
			})),
		},
		publicKeyPEM: string(pem.EncodeToMemory(&pem.Block{
			Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&key.PublicKey),
		})),
	}, nil
}

// sign adds the signature over digest, the normalised digest of desc.
func (s *signer) sign(ctx context.Context, desc *descriptor.Descriptor, digest descriptor.Digest) error {
	info, err := s.handler.Sign(ctx, digest, s.config, s.credentials)
	if err != nil {
		return fmt.Errorf("signing %s:%s: %w", desc.Component.Name, desc.Component.Version, err)
	}
	desc.Signatures = append(desc.Signatures, descriptor.Signature{Name: signatureName, Digest: digest, Signature: info})
	return nil
}

// verificationConfig is the OCM config that makes the controller verify
// signatureName with publicKeyPEM. It carries no private key.
func verificationConfig(publicKeyPEM string) (string, error) {
	cfg := map[string]any{
		"type": "generic.config.ocm.software/v1",
		"configurations": []any{
			map[string]any{
				"type":      "signing.config.ocm.software/v1alpha1",
				"signature": signatureName,
				"verifier":  map[string]any{"type": rsav1alpha1.ConfigType + "/" + rsav1alpha1.Version},
			},
			map[string]any{
				"type": "credentials.config.ocm.software",
				"consumers": []any{map[string]any{
					"identity": map[string]any{
						"type":      "RSA/v1alpha1",
						"algorithm": string(rsav1alpha1.AlgorithmRSASSAPSS),
						"signature": signatureName,
					},
					"credentials": []any{map[string]any{
						"type":       "Credentials/v1",
						"properties": map[string]any{"publicKeyPEM": publicKeyPEM},
					}},
				}},
			},
		},
	}
	// JSON is valid YAML, which is what the controller reads.
	raw, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
