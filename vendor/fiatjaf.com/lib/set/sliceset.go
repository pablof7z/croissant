package set

import (
	"slices"

	"golang.org/x/exp/constraints"
)

type SliceSet[A constraints.Ordered] struct {
	items []A
}

func NewSliceSet[A constraints.Ordered](items ...A) Set[A] {
	slices.Sort(items)
	items = slices.Compact(items)
	return &SliceSet[A]{items: items}
}

func NewEmptySliceSetReusing[A constraints.Ordered](items []A) Set[A] {
	return &SliceSet[A]{items: items[:0]}
}

func (s *SliceSet[A]) Reset() {
	s.items = s.items[:0]
}

func (s *SliceSet[A]) Add(items ...A) {
	for _, a := range items {
		idx, exists := slices.BinarySearch(s.items, a)
		if exists {
			continue
		}
		s.items = append(s.items, a) // bogus append just to increase the capacity
		copy(s.items[idx+1:], s.items[idx:])
		s.items[idx] = a
	}
}

func (s *SliceSet[A]) Has(item A) bool {
	_, exists := slices.BinarySearch(s.items, item)
	return exists
}

func (s *SliceSet[V]) Intersection(other Set[V]) Set[V] {
	// fast path: both are SliceSets - use a two-pointer merge
	if o, ok := other.(*SliceSet[V]); ok {
		inter := SliceSet[V]{items: make([]V, 0, min(len(s.items), len(o.items)))}
		i, j := 0, 0
		for i < len(s.items) && j < len(o.items) {
			switch {
			case s.items[i] == o.items[j]:
				inter.items = append(inter.items, s.items[i])
				i++
				j++
			case s.items[i] < o.items[j]:
				i++
			default:
				j++
			}
		}
		return &inter
	}

	// slow path: other is some other Set implementation
	inter := SliceSet[V]{items: make([]V, 0, len(s.items))}
	for _, k := range s.items {
		if other.Has(k) {
			inter.items = append(inter.items, k)
		}
	}
	return &inter
}

func (s *SliceSet[V]) Union(other Set[V]) Set[V] {
	if o, ok := other.(*SliceSet[V]); ok {
		union := SliceSet[V]{items: make([]V, 0, len(s.items)+len(o.items))}
		i, j := 0, 0
		for i < len(s.items) && j < len(o.items) {
			switch {
			case s.items[i] == o.items[j]:
				union.items = append(union.items, s.items[i])
				i++
				j++
			case s.items[i] < o.items[j]:
				union.items = append(union.items, s.items[i])
				i++
			default:
				union.items = append(union.items, o.items[j])
				j++
			}
		}
		union.items = append(union.items, s.items[i:]...)
		union.items = append(union.items, o.items[j:]...)
		return &union
	}

	union := SliceSet[V]{items: make([]V, 0, len(s.items)+other.Len())}
	union.items = append(union.items, s.items...)
	union.Add(other.Slice()...)
	return &union
}

func (s *SliceSet[V]) Difference(other Set[V]) Set[V] {
	if o, ok := other.(*SliceSet[V]); ok {
		diff := SliceSet[V]{items: make([]V, 0, len(s.items))}
		i, j := 0, 0
		for i < len(s.items) && j < len(o.items) {
			switch {
			case s.items[i] == o.items[j]:
				i++
				j++
			case s.items[i] < o.items[j]:
				diff.items = append(diff.items, s.items[i])
				i++
			default:
				j++
			}
		}
		diff.items = append(diff.items, s.items[i:]...)
		return &diff
	}

	diff := SliceSet[V]{items: make([]V, 0, len(s.items))}
	for _, k := range s.items {
		if !other.Has(k) {
			diff.items = append(diff.items, k)
		}
	}
	return &diff
}

func (s *SliceSet[A]) Remove(items ...A) {
	for _, a := range items {
		idx, exists := slices.BinarySearch(s.items, a)
		if !exists {
			continue
		}
		copy(s.items[idx:], s.items[idx+1:])
		s.items = s.items[0 : len(s.items)-1]
	}
}

func (s *SliceSet[A]) Slice() []A {
	return s.items
}

func (s *SliceSet[A]) Len() int {
	return len(s.items)
}
