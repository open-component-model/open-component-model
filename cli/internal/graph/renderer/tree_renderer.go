package displaymanager

import (
	"cmp"
	"context"
	"fmt"
	"io"

	"github.com/jedib0t/go-pretty/v6/list"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
)

type GraphRenderer[T cmp.Ordered] interface {
	Render(ctx context.Context, writer io.Writer, dag *syncdag.DirectedAcyclicGraph[T], roots []T) error
}

func NewTreeRenderer[T cmp.Ordered](vertexSerializer func(*syncdag.Vertex[T]) string) *TreeRenderer[T] {
	if vertexSerializer == nil {
		vertexSerializer = func(v *syncdag.Vertex[T]) string {
			// Default serializer just returns the vertex ID.
			// This can be overridden by the user.
			return fmt.Sprintf("%v", v.ID)
		}
	}
	return &TreeRenderer[T]{
		listWriter:       list.NewWriter(),
		vertexSerializer: vertexSerializer,
	}
}

type TreeRenderer[T cmp.Ordered] struct {
	listWriter       list.Writer
	vertexSerializer func(*syncdag.Vertex[T]) string
}

func (t *TreeRenderer[T]) Render(ctx context.Context, writer io.Writer, dag *syncdag.DirectedAcyclicGraph[T], roots []T) error {
	t.listWriter.SetStyle(list.StyleConnectedRounded)
	defer t.listWriter.Reset()

	if len(roots) == 0 {
		return fmt.Errorf("no roots provided for rendering")
	} else if len(roots) > 1 {
		return fmt.Errorf("multiple roots provided for rendering, only one root is supported")
	}

	root := roots[0]
	_, exists := dag.GetVertex(root)
	if !exists {
		return fmt.Errorf("vertex for rootID %v does not exist", root)
	}

	if err := t.traverseGraph(ctx, dag, root); err != nil {
		return fmt.Errorf("failed to traverse graph: %w", err)
	}
	t.listWriter.SetOutputMirror(writer)
	t.listWriter.Render()
	return nil
}

func (t *TreeRenderer[T]) traverseGraph(ctx context.Context, dag *syncdag.DirectedAcyclicGraph[T], nodeId T) error {
	vertex, ok := dag.GetVertex(nodeId)
	if !ok {
		return fmt.Errorf("vertex for nodeId %v does not exist", nodeId)
	}
	item := t.vertexSerializer(vertex)
	t.listWriter.AppendItem(item)

	// Get children and sort them for stable output
	children := GetNeighborsSorted(vertex)

	for _, child := range children {
		t.listWriter.Indent()
		if err := t.traverseGraph(ctx, dag, child); err != nil {
			return err
		}
		t.listWriter.UnIndent()
	}
	return nil
}
