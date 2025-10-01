package compref

import (
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
)

type Options struct {
	// CTFAccessMode specifies the access mode for any CTF created from Parse
	CTFAccessMode ctf.AccessMode
}

func (o *Options) Apply(opts *Options) error {
	if o != nil {
		*opts = *o
	}
	return nil
}

func NewOptions(opts ...Option) (*Options, error) {
	var options Options
	for _, opt := range opts {
		if err := opt.Apply(&options); err != nil {
			return nil, err
		}
	}
	return &options, nil
}

type Option interface {
	Apply(*Options) error
}

type OptionFunc func(*Options) error

func (f OptionFunc) Apply(opts *Options) error {
	return f(opts)
}

func WithCTFAccessMode(mode ctf.AccessMode) Option {
	return OptionFunc(func(opts *Options) error {
		opts.CTFAccessMode = mode
		return nil
	})
}
