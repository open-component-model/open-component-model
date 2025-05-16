// Package v1 provides the core data structures for constructing components in the Open Component Model (OCM).
//
// The component constructor defines the structure for creating and managing components, including:
// - Component metadata and identity
// - Resources (delivery artifacts)
// - Sources (artifacts used to generate resources)
// - References to other components
// - Access and input specifications
//
// This package implements the component constructor specification, which is differing
// from the component descriptor specification.
//
// The component constructor is a higher-level abstraction that allows for the definition of components
// in a more flexible and extensible manner, enabling better support for building new components.
//
// An example of a component constructor is as follows:
//
//	 components:
//	- name: github.com/acme.org/helloworld
//	  version: 1.0.0
//	  provider:
//	    name: internal
//	  resources:
//	    - name: testdata
//	      type: blob
//	      relation: local
//	      input:
//	        type: file
//	        path: ./testdata/text.txt
//	    - name: image
//	      type: OCIImage/v1
//	      relation: external
//	      access:
//	        type: OCIImage/v1
//	        imageReference: ghcr.io/stefanprodan/podinfo:6.8.0
//	      version: 6.8.0
package v1
