// Package transfer provides functionality for transferring OCM component versions
// between repositories.
//
// It builds transformation graph definitions that describe how to move component
// versions (and optionally their resources) from a source repository to a target
// repository. The graph is then executed using the transform/graph/runtime package.
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
//	tgd, err := transfer.BuildGraphDefinition(ctx, fromSpec, toSpec, repoResolver,
//	    transfer.WithRecursive(true),
//	    transfer.WithCopyMode(transfer.CopyModeAllResources),
//	    transfer.WithUploadType(transfer.UploadAsOciArtifact),
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
package transfer
