package fieldpath

import "strings"

func NamedSegment(name string) Segment {
	return Segment{Name: name}
}

func IndexedSegment(index int) Segment {
	return Segment{Index: &index}
}

type Segment struct {
	Name  string
	Index *int
}

func New(segments ...Segment) Path {
	return segments
}

type Path []Segment

func (p Path) String() string {
	return Build(p)
}

func (p Path) Add(segment ...Segment) Path {
	return append(p, segment...)
}

func (p Path) AddNamed(name string) Path {
	return append(p, NamedSegment(name))
}

func (p Path) AddIndexed(index int) Path {
	return append(p, IndexedSegment(index))
}

func Compare(p1, p2 Path) int {
	return strings.Compare(p1.String(), p2.String())
}

func (p Path) Equals(other Path) bool {
	if len(p) != len(other) {
		return false
	}
	for i := range p {
		if p[i].Name != other[i].Name {
			return false
		}
		x, y := p[i].Index, other[i].Index
		if x == y { // covers same address and both nil
			return true
		}
		if x == nil || y == nil {
			return false
		}
		if *x != *y {
			return false
		}
	}
	return true
}
