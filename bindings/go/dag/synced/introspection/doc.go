// Package introspection provides debugging utilities for the dag package.
//
// IT IS NOT SUPPOSED TO BE USED IN PRODUCTIVE CODE.
//
// As sync.Map cannot be introspected during debugging, this package provides
// convenience functionality to create a map-based snapshot of an actual
// sync.Map-based DirectedAcyclicGraph.
package introspection
