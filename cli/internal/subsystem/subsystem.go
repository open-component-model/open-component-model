package subsystem

import (
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// Subsystem represents a logical grouping of OCM types with associated documentation.
type Subsystem struct {
	// Name is a computer-readable ID (e.g., "ocm-repository").
	Name string
	// Title is a human-readable title for the subsystem.
	Title string
	// Description is a high-level summary of the subsystem's purpose.
	Description string
	// Scheme is the runtime scheme providing the types for this subsystem.
	Scheme *runtime.Scheme
}

// Registry is a central container for all registered subsystems.
type Registry struct {
	mu         sync.RWMutex
	subsystems map[string]*Subsystem
}

// NewRegistry creates a new, empty SubsystemRegistry.
func NewRegistry() *Registry {
	return &Registry{
		subsystems: make(map[string]*Subsystem),
	}
}

// Register adds a subsystem to the registry. If a subsystem with the same name exists, it is overwritten.
func (r *Registry) Register(s *Subsystem) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.subsystems[s.Name] = s
}

// Get retrieves a subsystem by its name.
func (r *Registry) Get(name string) *Subsystem {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.subsystems[name]
}

// List returns all registered subsystems.
func (r *Registry) List() []*Subsystem {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var list []*Subsystem
	for _, s := range r.subsystems {
		list = append(list, s)
	}
	return list
}

// GlobalRegistry is the default registry instance used by the CLI.
var GlobalRegistry = NewRegistry()

const (
	// Annotation is the Cobra command annotation key used to link commands to subsystems.
	Annotation = "ocm.software/subsystem"
)

// Register adds a subsystem to the GlobalRegistry.
func Register(s *Subsystem) {
	GlobalRegistry.Register(s)
}

// Get retrieves a subsystem from the GlobalRegistry.
func Get(name string) *Subsystem {
	return GlobalRegistry.Get(name)
}

// List returns all subsystems in the GlobalRegistry.
func List() []*Subsystem {
	return GlobalRegistry.List()
}

// FindLinkedCommands searches the command tree for all commands that are linked to the given subsystem name.
func FindLinkedCommands(cmd *cobra.Command, subsystemName string) []*cobra.Command {
	var found []*cobra.Command
	root := cmd.Root()

	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Annotations != nil {
			if val, ok := c.Annotations[Annotation]; ok {
				// Support comma-separated list of subsystems
				parts := strings.Split(val, ",")
				for _, p := range parts {
					if strings.TrimSpace(p) == subsystemName {
						found = append(found, c)
						break
					}
				}
			}
		}
		for _, child := range c.Commands() {
			walk(child)
		}
	}

	walk(root)
	return found
}
