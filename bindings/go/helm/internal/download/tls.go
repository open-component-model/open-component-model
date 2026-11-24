package download

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"

	"helm.sh/helm/v4/pkg/getter"

	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	ocmhttp "ocm.software/open-component-model/bindings/go/http"
)

type tlsOptions struct {
	CaCertFile  string
	CaCert      string
	Credentials *helmcredsv1.HelmHTTPCredentials
}

type tlOptionsFn func(opt *tlsOptions) *tlsOptions

func withCACertFile(caCertFile string) tlOptionsFn {
	return func(opt *tlsOptions) *tlsOptions {
		opt.CaCertFile = caCertFile
		return opt
	}
}

func withCACert(caCert string) tlOptionsFn {
	return func(opt *tlsOptions) *tlsOptions {
		opt.CaCert = caCert
		return opt
	}
}

func withCredentials(credentials *helmcredsv1.HelmHTTPCredentials) tlOptionsFn {
	return func(opt *tlsOptions) *tlsOptions {
		opt.Credentials = credentials
		return opt
	}
}

// constructTLSOptions sets up the TLS configuration files based on the helm specification.
// It returns the getter option alongside the resolved CA certificate file path so callers
// that bypass the getter (e.g. helm's repo machinery) can reuse the same CA material.
func constructTLSOptions(targetDir string, opts ...tlOptionsFn) (_ getter.Option, caFilePath string, err error) {
	if targetDir == "" {
		return nil, "", errors.New("target directory for TLS files must be specified")
	}

	opt := &tlsOptions{}
	for _, o := range opts {
		o(opt)
	}

	if opt.Credentials == nil {
		opt.Credentials = &helmcredsv1.HelmHTTPCredentials{}
	}

	var caFile *os.File

	if opt.CaCertFile != "" {
		caFilePath = opt.CaCertFile
	} else if opt.CaCert != "" {
		caFile, err = os.CreateTemp(targetDir, "caCert-*.pem")
		if err != nil {
			return nil, "", fmt.Errorf("error creating temporary CA certificate file: %w", err)
		}
		defer func() {
			if cerr := caFile.Close(); cerr != nil {
				err = errors.Join(err, cerr)
			}
		}()
		if _, err = caFile.WriteString(opt.CaCert); err != nil {
			return nil, "", fmt.Errorf("error writing CA certificate to temp file: %w", err)
		}
		caFilePath = caFile.Name()
	}

	// set up certFile and keyFile if they are provided in the credentials
	if opt.Credentials.CertFile != "" {
		if _, err := os.Stat(opt.Credentials.CertFile); err != nil {
			if os.IsNotExist(err) {
				return nil, "", fmt.Errorf("certFile %q does not exist", opt.Credentials.CertFile)
			}
			return nil, "", fmt.Errorf("certFile %q is not accessible: %w", opt.Credentials.CertFile, err)
		}
	}
	if opt.Credentials.KeyFile != "" {
		if _, err := os.Stat(opt.Credentials.KeyFile); err != nil {
			if os.IsNotExist(err) {
				return nil, "", fmt.Errorf("keyFile %q does not exist", opt.Credentials.KeyFile)
			}
			return nil, "", fmt.Errorf("keyFile %q is not accessible: %w", opt.Credentials.KeyFile, err)
		}
	}

	// it's safe to always add this option even with empty values
	// because the default is empty.
	return getter.WithTLSClientConfig(opt.Credentials.CertFile, opt.Credentials.KeyFile, caFilePath), caFilePath, nil
}

// overlayInputTLS returns client with the CA and client certificate of the chart's input or
// credentials in place of those of the HTTP configuration. They are specific to the chart's
// repository and so take precedence over the global configuration, like helm's own getters,
// which replace the system roots with a configured CA file as well.
func overlayInputTLS(client *http.Client, caFile, certFile, keyFile string) (*http.Client, error) {
	var roots *x509.CertPool
	if caFile != "" {
		pemData, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("error reading CA certificate file: %w", err)
		}
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pemData) {
			return nil, fmt.Errorf("CA certificate file %q contains no valid PEM certificate", caFile)
		}
	}
	var certs []tls.Certificate
	if certFile != "" && keyFile != "" {
		pair, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("error loading client certificate: %w", err)
		}
		certs = []tls.Certificate{pair}
	}
	if roots == nil && certs == nil {
		return client, nil
	}
	return ocmhttp.CloneTransports(client, func(tr *http.Transport) {
		if roots != nil {
			tr.TLSClientConfig.RootCAs = roots
		}
		if certs != nil {
			tr.TLSClientConfig.Certificates = certs
		}
	})
}
