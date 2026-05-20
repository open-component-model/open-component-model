// Package transfer provides functionality for transferring OCM component versions
// between repositories.
//
// It builds transformation graph definitions that describe how to move component
// versions (and optionally their resources) from source repositories to target
// repositories. The graph is then executed using the transform/graph/runtime package.
//
// Supported repository types include OCI registries and Common Transport Format (CTF)
// archives. Resources with OCI artifact, local blob, and Helm chart access types are
// handled during transfer.
//
// # Basic Usage
//
// Use [BuildGraphDefinition] to construct a transformation graph, and [NewDefaultBuilder]
// to create a pre-configured graph builder that can execute it:
//
//	tgd, err := transfer.BuildGraphDefinition(ctx,
//	    transfer.WithTransfer(
//	        transfer.Component("ocm.software/mycomponent", "1.0.0"),
//	        transfer.ToRepositorySpec(targetSpec),
//	        transfer.FromResolver(repoResolver),
//	    ),
//	    transfer.WithCopyMode(transferv1alpha1.CopyModeAllResources),
//	)
//	if err != nil {
//	    return err
//	}
//
//	b := transfer.NewDefaultBuilder(repoProvider, resourceRepo, credentialProvider)
//	graph, err := b.BuildAndCheck(tgd)
//	if err != nil {
//	    return err
//	}
//	if err := graph.Process(ctx); err != nil {
//	    return err
//	}
//
// # Simple Case — FromRepository
//
// For simple transfers from a single repository, use [FromRepository] instead of
// a full resolver:
//
//	tgd, err := transfer.BuildGraphDefinition(ctx,
//	    transfer.WithTransfer(
//	        transfer.Component("ocm.software/app", "1.0.0"),
//	        transfer.ToRepositorySpec(targetSpec),
//	        transfer.FromRepository(sourceRepo, sourceSpec),
//	    ),
//	)
//
// # Routing Components to Different Sources and Targets
//
// Different components can come from different sources and go to different targets:
//
//	tgd, err := transfer.BuildGraphDefinition(ctx,
//	    transfer.WithTransfer(
//	        transfer.Component("ocm.software/frontend", "1.0.0"),
//	        transfer.ToRepositorySpec(registryA),
//	        transfer.FromResolver(sourceResolverA),
//	    ),
//	    transfer.WithTransfer(
//	        transfer.Component("ocm.software/backend", "2.0.0"),
//	        transfer.ToRepositorySpec(registryB),
//	        transfer.FromResolver(sourceResolverB),
//	    ),
//	)
//
// # Multiple Components to Same Target
//
//	tgd, err := transfer.BuildGraphDefinition(ctx,
//	    transfer.WithTransfer(
//	        transfer.Component("ocm.software/a", "1.0.0"),
//	        transfer.Component("ocm.software/b", "2.0.0"),
//	        transfer.ToRepositorySpec(targetRepo),
//	        transfer.FromResolver(repoResolver),
//	    ),
//	)
//
// # Driving the Transfer from a Wire-Format Config
//
// The transfer knobs (recursive, copy mode, upload type) are also expressed by
// [transferv1alpha1.Config] - the same wire format the CLI loads via
// `ocm transfer component-version --transfer-config <file>` and that the
// replication controller will consume from a CRD spec. Use [FromConfig] to feed
// a loaded config into [BuildGraphDefinition] alongside the runtime-only mappings:
//
//	cfg := &transferv1alpha1.Config{
//	    Recursive:  true,
//	    CopyMode:   transferv1alpha1.CopyModeAllResources,
//	    UploadType: transferv1alpha1.UploadAsOciArtifact,
//	}
//	if err := cfg.Validate(); err != nil {
//	    return err
//	}
//	tgd, err := transfer.BuildGraphDefinition(ctx,
//	    append(transfer.FromConfig(cfg),
//	        transfer.WithTransfer(
//	            transfer.Component("ocm.software/app", "1.0.0"),
//	            transfer.ToRepositorySpec(targetSpec),
//	            transfer.FromResolver(repoResolver),
//	        ),
//	    )...,
//	)
//
// [FromConfig] skips empty fields - so callers can overlay explicit overrides
// on top of a partial config without the zero values clobbering them. The
// resulting [Options] therefore still carries empty enum strings until
// [BuildGraphDefinition] runs; that's where the empties are resolved to their
// canonical defaults via [transferv1alpha1.Config.GetCopyMode] and
// [transferv1alpha1.Config.GetUploadType].
package transfer
