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
//	    transfer.WithCopyMode(transfer.CopyModeAllResources),
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
package transfer
