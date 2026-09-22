package list

type Node[T any] struct {
	prev    *Node[T]
	next    *Node[T]
	element T
}

func NewNode[T any](element T) *Node[T] {
	return &Node[T]{element: element}
}

func (n *Node[T]) Value() T {
	return n.element
}

type Iterator[T any] struct {
	*Node[T]
}

func (it Iterator[T]) Next() Iterator[T] {
	return Iterator[T]{Node: it.next}
}

func (it Iterator[T]) Prev() Iterator[T] {
	return Iterator[T]{Node: it.prev}
}

func (it Iterator[T]) Equal(other Iterator[T]) bool {
	return it.Node == other.Node
}

func (it Iterator[T]) Value() T {
	return it.element
}

type List[T any] struct {
	pivot *Node[T]
	size  int
}

func NewList[T any]() *List[T] {
	l := &List[T]{
		pivot: &Node[T]{},
	}
	l.pivot.next = l.pivot
	l.pivot.prev = l.pivot
	return l
}

func (l *List[T]) Len() int {
	return l.size
}

func (l *List[T]) Empty() bool {
	return l.size == 0
}

func (l *List[T]) Remove(it Iterator[T]) Iterator[T] {
	node := it.Node

	prev := node.prev
	next := node.next

	prev.next = next
	next.prev = prev

	node.prev = nil
	node.next = nil
	var zero T
	node.element = zero

	l.size--

	return Iterator[T]{Node: next}
}

func (l *List[T]) Begin() Iterator[T] {
	return Iterator[T]{Node: l.pivot.next}
}

func (l *List[T]) End() Iterator[T] {
	return Iterator[T]{Node: l.pivot}
}

func (l *List[T]) PushBack(val T) Iterator[T] {
	return l.Insert(Iterator[T]{Node: NewNode(val)}, l.End())
}

func (l *List[T]) PushFront(val T) Iterator[T] {
	return l.Insert(Iterator[T]{Node: NewNode(val)}, l.Begin())
}

func (l *List[T]) Insert(node, target Iterator[T]) Iterator[T] {
	prev := target.prev
	node.prev = prev
	node.next = target.Node
	prev.next = node.Node
	target.prev = node.Node
	l.size++
	return node
}

func (l *List[T]) Front() Iterator[T] {
	if l.Empty() {
		return Iterator[T]{}
	}
	return Iterator[T]{Node: l.pivot.next}
}
func (l *List[T]) Back() Iterator[T] {
	if l.Empty() {
		return Iterator[T]{}
	}
	return Iterator[T]{l.pivot.prev}
}

func (l *List[T]) PopFront() Iterator[T] {
	if l.Empty() {
		return Iterator[T]{}
	}
	return l.Remove(l.Front())
}

func (l *List[T]) PopBack() Iterator[T] {
	if l.Empty() {
		return Iterator[T]{}
	}
	return l.Remove(l.Back())
}
func (l *List[T]) Clear() {
	for node := l.pivot.next; node != l.pivot; {
		next := node.next
		node.prev = nil
		node.next = nil
		var zero T
		node.element = zero
		node = next
	}

	l.size = 0
	l.pivot.prev = l.pivot
	l.pivot.next = l.pivot
}
