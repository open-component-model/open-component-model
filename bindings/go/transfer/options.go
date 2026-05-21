package transfer

import (
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1"
)

// Options configures the behavior of a transfer operation.
//
// It embeds the wire-format [transferv1alpha1.Config] (so the declarative knobs
// are accessed as promoted fields) and adds [Mappings], which carries the live
// runtime objects (resolvers, repository specs) that the wire format cannot
// serialize.
//
// The embedded [transferv1alpha1.Config.Type] is wire-only and not consulted
// by the runtime; it is exposed only because Go's struct-embedding promotes
// every field. Callers should not set it on Options.
type Options struct {
	transferv1alpha1.Config

	Mappings []Mapping
}

type Option func(*Options)

func WithCopyMode(mode transferv1alpha1.CopyMode) Option {
	return func(o *Options) {
		o.CopyMode = mode
	}
}

func WithRecursive(recursive bool) Option {
	return func(o *Options) {
		o.Recursive = &recursive
	}
}

func WithUploadType(upload transferv1alpha1.UploadType) Option {
	return func(o *Options) {
		o.UploadType = upload
	}
}

// WithTransfer adds a transfer mapping that routes source components to a target repository.
//
//	transfer.WithTransfer(
//	    transfer.Component("ocm.software/frontend", "1.0.0"),
//	    transfer.ToRepositorySpec(targetRepo),
//	    transfer.FromResolver(sourceResolver),
//	)
func WithTransfer(transferOpts ...TransferOption) Option {
	return func(o *Options) {
		m := Mapping{}
		for _, opt := range transferOpts {
			opt(&m)
		}
		o.Mappings = append(o.Mappings, m)
	}
}

func FromConfig(cfg *transferv1alpha1.Config) []Option {
	if cfg == nil {
		return nil
	}
	var opts []Option
	if cfg.Recursive != nil {
		opts = append(opts, WithRecursive(*cfg.Recursive))
	}
	if cfg.CopyMode != "" {
		opts = append(opts, WithCopyMode(cfg.CopyMode))
	}
	if cfg.UploadType != "" {
		opts = append(opts, WithUploadType(cfg.UploadType))
	}
	return opts
}
