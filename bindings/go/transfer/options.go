package transfer

import (
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1"
)

// Options configures the behavior of a transfer operation.
//
// It embeds the wire-format [transferv1alpha1.Config] (so the declarative knobs
// defined there - recursive, copy mode, upload type - are accessed directly as
// promoted fields) and adds [Mappings], which carries the live runtime objects
// (resolvers, repository specs) that the wire format cannot serialize.
//
// The embedded [transferv1alpha1.Config.Type] is wire-only and not consulted
// by the runtime; it is exposed only because Go's struct-embedding promotes
// every field. Callers should not set it on Options.
//
// Transfer mappings must be specified via [WithTransfer].
// Each mapping must include a resolver via [FromResolver] or [FromRepository],
// components via [Component], and a target via [ToRepositorySpec].
type Options struct {
	transferv1alpha1.Config

	// Mappings defines which components are transferred to which targets.
	Mappings []Mapping
}

// Option is a functional option for configuring transfer operations.
type Option func(*Options)

// WithCopyMode sets the copy mode for the transfer operation.
func WithCopyMode(mode transferv1alpha1.CopyMode) Option {
	return func(o *Options) {
		o.CopyMode = mode
	}
}

// WithRecursive enables or disables recursive transfer of referenced component versions.
// When enabled, the transfer discovers all component references in the source descriptor
// and transfers them (and their transitive references) to the target repository.
func WithRecursive(recursive bool) Option {
	return func(o *Options) {
		o.Recursive = recursive
	}
}

// WithUploadType sets how resources are stored in the target repository.
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

// FromConfig converts a [transferv1alpha1.Config] into a slice of [Option]s.
//
// Empty fields are deliberately skipped so callers can layer explicit overrides on top.
// This lets the CLI overlay flag values onto a partial config without the config's zero
// values clobbering the flag-supplied ones.
func FromConfig(cfg *transferv1alpha1.Config) []Option {
	if cfg == nil {
		return nil
	}
	var opts []Option
	if cfg.Recursive {
		opts = append(opts, WithRecursive(true))
	}
	if cfg.CopyMode != "" {
		opts = append(opts, WithCopyMode(cfg.CopyMode))
	}
	if cfg.UploadType != "" {
		opts = append(opts, WithUploadType(cfg.UploadType))
	}
	return opts
}
