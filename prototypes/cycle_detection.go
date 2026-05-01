// cycle_detection.go - Prototype for cycle detection in OCM graphs using Tarjan's algorithm.
//
// Key Idea: Detect strongly connected components (SCCs) to identify cycles.
// This prevents infinite loops during graph traversal.

package prototypes

import (
	"context"
	"fmt"
	"sync"

	"ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// TarjanDiscoverer implements cycle detection using Tarjan's algorithm.
type TarjanDiscoverer struct {
	mu          sync.Mutex
	index       int
	indexMap    map[string]int      // Key: "component:version"
	lowlinkMap  map[string]int      // Key: "component:version"
	onStack     map[string]bool     // Key: "component:version"
	stack       []string
	visited     map[string]bool     // Key: "component:version"
	cycles      [][]string          // List of cycles (each cycle is a list of component keys)
	descriptors map[string]*descriptor.Descriptor // Key: "component:version"
}

// NewTarjanDiscoverer creates a new TarjanDiscoverer.
func NewTarjanDiscoverer() *TarjanDiscoverer {
	return &TarjanDiscoverer{
		indexMap:    make(map[string]int),
		lowlinkMap:  make(map[string]int),
		onStack:     make(map[string]bool),
		visited:     make(map[string]bool),
		descriptors: make(map[string]*descriptor.Descriptor),
	}
}

// Discover resolves a component and its dependencies, detecting cycles.
func (d *TarjanDiscoverer) Discover(
	ctx context.Context,
	key string,
	desc *descriptor.Descriptor,
	recursive bool,
) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Initialize Tarjan's state for this component.
	if _, exists := d.indexMap[key]; !exists {
		d.indexMap[key] = d.index
		d.lowlinkMap[key] = d.index
		d.index++
		d.onStack[key] = true
		d.stack = append(d.stack, key)
		d.descriptors[key] = desc
	}

	// Recursively discover dependencies.
	if recursive {
		for _, ref := range desc.Component.References {
			childKey := ref.Component + ":" + ref.Version
			if err := d.discoverChild(ctx, key, childKey, recursive); err != nil {
				return err
			}
		}
	}

	// If this is a root node, check for SCCs (cycles).
	if d.indexMap[key] == d.lowlinkMap[key] {
		cycle := d.popStack(key)
		if len(cycle) > 1 {
			d.cycles = append(d.cycles, cycle)
		}
	}

	return nil
}

// discoverChild resolves a child component and updates Tarjan's state.
func (d *TarjanDiscoverer) discoverChild(
	ctx context.Context,
	parentKey string,
	childKey string,
	recursive bool,
) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// If the child hasn't been visited, resolve it and recurse.
	if _, exists := d.indexMap[childKey]; !exists {
		// TODO: Resolve the child descriptor (reuse multiResolver from OCM).
		childDesc := &descriptor.Descriptor{}
		d.descriptors[childKey] = childDesc
		d.indexMap[childKey] = d.index
		d.lowlinkMap[childKey] = d.index
		d.index++
		d.onStack[childKey] = true
		d.stack = append(d.stack, childKey)

		// Recurse for the child's dependencies.
		if recursive {
			for _, ref := range childDesc.Component.References {
				grandchildKey := ref.Component + ":" + ref.Version
				if err := d.discoverChild(ctx, childKey, grandchildKey, true); err != nil {
					return err
				}
			}
		}

		// Update lowlink for the parent.
		if d.lowlinkMap[childKey] < d.lowlinkMap[parentKey] {
			d.lowlinkMap[parentKey] = d.lowlinkMap[childKey]
		}
	} else if d.onStack[childKey] {
		// Update lowlink for the parent if the child is on the stack.
		if d.indexMap[childKey] < d.lowlinkMap[parentKey] {
			d.lowlinkMap[parentKey] = d.indexMap[childKey]
		}
	}

	return nil
}

// popStack pops the stack up to and including the given key, returning the cycle.
func (d *TarjanDiscoverer) popStack(key string) []string {
	var cycle []string
	for {
		top := d.stack[len(d.stack)-1]
		d.stack = d.stack[:len(d.stack)-1]
		d.onStack[top] = false
		cycle = append(cycle, top)
		if top == key {
			break
		}
	}
	return cycle
}

// HasCycles returns true if cycles were detected during discovery.
func (d *TarjanDiscoverer) HasCycles() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.cycles) > 0
}

// GetCycles returns the list of detected cycles.
func (d *TarjanDiscoverer) GetCycles() [][]string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.cycles
}

// Clear clears the TarjanDiscoverer's state.
func (d *TarjanDiscoverer) Clear() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.index = 0
	d.indexMap = make(map[string]int)
	d.lowlinkMap = make(map[string]int)
	d.onStack = make(map[string]bool)
	d.stack = nil
	d.visited = make(map[string]bool)
	d.cycles = nil
	d.descriptors = make(map[string]*descriptor.Descriptor)
}