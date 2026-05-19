package download

import (
	"errors"
	"fmt"
	"os"

	"helm.sh/helm/v4/pkg/getter"

	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
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

// constructTLSOptions sets up the TLS configuration files based on the helm specification
func constructTLSOptions(targetDir string, opts ...tlOptionsFn) (_ getter.Option, err error) {
	if targetDir == "" {
		return nil, errors.New("target directory for TLS files must be specified")
	}

	opt := &tlsOptions{}
	for _, o := range opts {
		o(opt)
	}

	if opt.Credentials == nil {
		opt.Credentials = &helmcredsv1.HelmHTTPCredentials{}
	}

	var (
		caFile     *os.File
		caFilePath string
	)

	if opt.CaCertFile != "" {
		caFilePath = opt.CaCertFile
	} else if opt.CaCert != "" {
		caFile, err = os.CreateTemp(targetDir, "caCert-*.pem")
		if err != nil {
			return nil, fmt.Errorf("error creating temporary CA certificate file: %w", err)
		}
		defer func() {
			if cerr := caFile.Close(); cerr != nil {
				err = errors.Join(err, cerr)
			}
		}()
		if _, err = caFile.WriteString(opt.CaCert); err != nil {
			return nil, fmt.Errorf("error writing CA certificate to temp file: %w", err)
		}
		caFilePath = caFile.Name()
	}

	// set up certFile and keyFile if they are provided in the credentials
	if opt.Credentials.CertFile != "" {
		if _, err := os.Stat(opt.Credentials.CertFile); err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("certFile %q does not exist", opt.Credentials.CertFile)
			}
			return nil, fmt.Errorf("certFile %q is not accessible: %w", opt.Credentials.CertFile, err)
		}
	}
	if opt.Credentials.KeyFile != "" {
		if _, err := os.Stat(opt.Credentials.KeyFile); err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("keyFile %q does not exist", opt.Credentials.KeyFile)
			}
			return nil, fmt.Errorf("keyFile %q is not accessible: %w", opt.Credentials.KeyFile, err)
		}
	}

	// it's safe to always add this option even with empty values
	// because the default is empty.
	return getter.WithTLSClientConfig(opt.Credentials.CertFile, opt.Credentials.KeyFile, caFilePath), nil
}
