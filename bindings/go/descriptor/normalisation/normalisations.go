package normalisation

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// Algorithm types and versions the algorithm used for digest generation.
type Algorithm = string

var ErrUnknownNormalisationAlgorithm = errors.New("unknown normalisation algorithm")

type Normalisation interface {
	Normalise(cd *runtime.Descriptor) ([]byte, error)
}

type Algorithms struct {
	sync.RWMutex
	algos map[string]Normalisation
}

func (n *Algorithms) Register(name string, norm Normalisation) {
	n.Lock()
	defer n.Unlock()
	n.algos[name] = norm
}

func (n *Algorithms) Get(algo string) Normalisation {
	n.RLock()
	defer n.RUnlock()
	return n.algos[algo]
}

func (n *Algorithms) Names() []string {
	n.RLock()
	defer n.RUnlock()
	names := []string{}
	for n := range n.algos {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func (n *Algorithms) Normalise(cd *runtime.Descriptor, algo string) ([]byte, error) {
	n.RLock()
	defer n.RUnlock()

	norm := n.algos[algo]
	if norm == nil {
		return nil, fmt.Errorf("%w: %s", ErrUnknownNormalisationAlgorithm, algo)
	}
	return norm.Normalise(cd)
}

var Normalisations = Algorithms{algos: map[string]Normalisation{}}

func Normalise(cd *runtime.Descriptor, normAlgo string) ([]byte, error) {
	return Normalisations.Normalise(cd, normAlgo)
}
