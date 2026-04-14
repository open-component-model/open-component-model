// Package v2 descriptor defines the main objects that compose a component version.
// The following objects make up a descriptor:
//
//	  meta:
//	  component:
//			 name
//			 version
//			 labels
//			 repositoryContexts
//			 provider
//			 resources
//			 sources
//			 componentReferences
//	  signatures: {} # optional
//
// A sample component version looks something like this:
//
//	  meta:
//		schemaVersion: v2
//	  component:
//		name: github.com/open-component-model/open-component-model
//		version: v1.0.0
//		provider: ocm.software
//		labels:
//		  - name: link-to-documentation
//			value: https://github.com/open-component-model/open-component-model
//		repositoryContexts:
//		  - baseUrl: ghcr.io
//			componentNameMapping: urlPath
//			subPath: open-component-model/ocm
//			type: OCIRegistry
//		resources:
//		  - name: image
//			relation: external
//			type: ociImage
//			version: v0.14.1
//			access:
//			  type: ociArtifact
//			  imageReference: ghcr.io/open-component-model/open-component-model:v0.0.1
//			digest:
//			  hashAlgorithm: SHA-256
//			  normalisationAlgorithm: ociArtifactDigest/v1
//			  value: efa2b9980ca2de65dc5a0c8cc05638b1a4b4ce8f6972dc08d0e805e5563ba5bb
//		sources:
//		  - name: ocm
//			type: git
//			version: v0.0.1
//			access:
//			  commit: 727513969553bfcc603e1c0ae1a75d79e4132b58
//			  ref: refs/tags/v0.0.1
//			  repoUrl: github.com/open-component-model/open-component-model
//			  type: gitHub
//		componentReferences:
//		  - name: prometheus
//			version: v1.0.0
//			componentName: cncf.io/prometheus
//			digest:
//			  hashAlgorithm: SHA-256
//			  normalisationAlgorithm: jsonNormalisation/v1
//			  value: 04eb20b6fd942860325caf7f4415d1acf287a1aabd9e4827719328ba25d6f801
//	  signatures:
//		 - name: ww-dev
//		   digest:
//		   hashAlgorithm: SHA-256
//		   normalisationAlgorithm: jsonNormalisation/v1
//		   value: 4faff7822616305ecd09284d7c3e74a64f2269dcc524a9cdf0db4b592b8cee6a
//		   signature:
//		   algorithm: RSASSA-PSS
//		   mediaType: application/vnd.ocm.signature.rsa
//		   value: ...
//
// Additional fields MAY be defined, such as `extraIdentity` or `labels`.
//
// # Label Merge
//
// Labels can carry an optional merge specification ([MergeSpec]) that controls
// how their values are reconciled during component version transfers. When a
// component version is transferred to a repository that already contains it,
// non-signing labels may differ between the source (inbound) and target (local).
//
// The [MergeLabels] function resolves these differences using one of the four
// built-in algorithms:
//
//   - "default" ([MergeAlgorithmDefault]) — binary comparison; on conflict the
//     configured [OverwriteMode] decides (default: inbound wins).
//   - "simpleMapMerge" ([MergeAlgorithmSimpleMapMerge]) — union of JSON object
//     keys with configurable conflict resolution and optional recursive nested
//     merge via the entries field.
//   - "simpleListMerge" ([MergeAlgorithmSimpleListMerge]) — union of JSON array
//     entries; entries present only in local are appended to inbound.
//   - "mapListMerge" ([MergeAlgorithmMapListMerge]) — merge lists of objects by
//     a configurable key field (default: "name").
//
// A label with merge configuration looks like this:
//
//	labels:
//	  - name: routing-slip
//	    value:
//	      destinations:
//	        - name: staging
//	          url: https://staging.example.com
//	    merge:
//	      algorithm: mapListMerge
//	      config:
//	        keyField: name
//	        overwrite: inbound
//
// See https://github.com/open-component-model/ocm-spec/blob/main/doc/04-extensions/04-algorithms/label-merge-algorithms.md
//
// To read more about the specification of a component visit https://github.com/open-component-model/ocm-spec/.
package v2
