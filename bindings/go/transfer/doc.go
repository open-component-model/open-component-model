// Package transfer provides functionality for transferring OCM component versions
// between repositories.
//
// It builds transformation graph definitions that describe how to move component
// versions (and optionally their resources) from source repositories to target
// repositories. The graph is then executed using the transform/graph/runtime package.
//
// Transfer settings (recursion) are carried by the wire format
// [transferv1alpha1.Config], typically extracted from the central generic
// configuration with [transferv1alpha1.LookupConfig]. Uploader configurations
// ([transferv1alpha1.UploaderConfig], extracted with
// [transferv1alpha1.LookupUploaderConfigs]) select which resources are copied and
// how; the catch-all [transferv1alpha1.LocalBlobUploaderConfig] below copies every
// resource as a local blob. Mappings route source
// components to target repositories and carry the runtime objects (resolvers,
// repository specs) the wire format cannot serialize:
//
//	cfg := &transferv1alpha1.Config{Recursive: transferv1alpha1.RecursiveInfinite}
//	uploaders := []transferv1alpha1.UploaderConfig{&transferv1alpha1.LocalBlobUploaderConfig{}}
//	tgd, err := transfer.BuildGraphDefinition(ctx, cfg, uploaders,
//	    transfer.Mapping{
//	        Components: []transfer.ComponentID{{Component: "ocm.software/app", Version: "1.0.0"}},
//	        Target:     targetSpec,
//	        Resolver:   transfer.NewRepositoryResolver(sourceRepo, sourceSpec),
//	    },
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
// A nil config uses the defaults (no recursion). Without uploaders, local blobs are
// copied and all other resources stay by reference.
// Multiple mappings enable N:M routing where different components come from
// different sources and go to different targets.
package transfer
