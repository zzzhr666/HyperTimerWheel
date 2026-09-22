package list

import (
	"reflect"
	"testing"
)

func valuesForward[T any](l *List[T]) []T {
	values := make([]T, 0, l.Len())
	for it := l.Begin(); !it.Equal(l.End()); it = it.Next() {
		values = append(values, it.Value())
	}
	return values
}

func valuesBackward[T any](l *List[T]) []T {
	values := make([]T, 0, l.Len())
	for it := l.Back(); !it.Equal(l.End()); it = it.Prev() {
		values = append(values, it.Value())
	}
	return values
}

func assertValues[T any](t *testing.T, l *List[T], wantForward, wantBackward []T) {
	t.Helper()

	if got := valuesForward(l); !reflect.DeepEqual(got, wantForward) {
		t.Fatalf("forward values = %v, want %v", got, wantForward)
	}
	if got := valuesBackward(l); !reflect.DeepEqual(got, wantBackward) {
		t.Fatalf("backward values = %v, want %v", got, wantBackward)
	}
}

func TestNewListIsEmpty(t *testing.T) {
	l := NewList[int]()

	if !l.Empty() {
		t.Fatal("new list Empty() = false, want true")
	}
	if l.Len() != 0 {
		t.Fatalf("new list Len() = %d, want 0", l.Len())
	}
	if !l.Begin().Equal(l.End()) {
		t.Fatal("empty list Begin() != End()")
	}
}

func TestPushFrontAndPushBack(t *testing.T) {
	l := NewList[int]()

	front := l.PushFront(2)
	back := l.PushBack(3)
	l.PushFront(1)

	if front.Value() != 2 {
		t.Fatalf("PushFront iterator value = %d, want 2", front.Value())
	}
	if back.Value() != 3 {
		t.Fatalf("PushBack iterator value = %d, want 3", back.Value())
	}

	assertValues(t, l, []int{1, 2, 3}, []int{3, 2, 1})
	if l.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", l.Len())
	}
}

func TestInsertBeforeTarget(t *testing.T) {
	l := NewList[int]()

	l.PushBack(1)
	target := l.PushBack(3)
	inserted := l.Insert(Iterator[int]{Node: NewNode(2)}, target)

	if inserted.Value() != 2 {
		t.Fatalf("inserted value = %d, want 2", inserted.Value())
	}
	assertValues(t, l, []int{1, 2, 3}, []int{3, 2, 1})
}

func TestRemoveReturnsNextIterator(t *testing.T) {
	l := NewList[int]()

	l.PushBack(1)
	middle := l.PushBack(2)
	l.PushBack(3)

	next := l.Remove(middle)
	if next.Value() != 3 {
		t.Fatalf("Remove() returned value = %d, want 3", next.Value())
	}

	assertValues(t, l, []int{1, 3}, []int{3, 1})
	if l.Len() != 2 {
		t.Fatalf("Len() after Remove() = %d, want 2", l.Len())
	}
}

func TestRemoveFrontBackAndOnlyNode(t *testing.T) {
	t.Run("only node", func(t *testing.T) {
		l := NewList[int]()
		l.PushBack(1)

		next := l.Remove(l.Front())
		if !next.Equal(l.End()) {
			t.Fatal("Remove(only node) did not return End()")
		}
		if !l.Empty() {
			t.Fatal("list is not empty after removing only node")
		}
	})

	t.Run("front", func(t *testing.T) {
		l := NewList[int]()
		l.PushBack(1)
		l.PushBack(2)
		l.PushBack(3)

		next := l.Remove(l.Front())
		if next.Value() != 2 {
			t.Fatalf("Remove(front) returned value = %d, want 2", next.Value())
		}
		assertValues(t, l, []int{2, 3}, []int{3, 2})
	})

	t.Run("back", func(t *testing.T) {
		l := NewList[int]()
		l.PushBack(1)
		l.PushBack(2)
		l.PushBack(3)

		next := l.Remove(l.Back())
		if !next.Equal(l.End()) {
			t.Fatal("Remove(back) did not return End()")
		}
		assertValues(t, l, []int{1, 2}, []int{2, 1})
	})
}

func TestPopFrontAndPopBack(t *testing.T) {
	l := NewList[int]()
	l.PushBack(1)
	l.PushBack(2)
	l.PushBack(3)

	next := l.PopFront()
	if next.Value() != 2 {
		t.Fatalf("PopFront() returned value = %d, want 2", next.Value())
	}
	next = l.PopBack()
	if !next.Equal(l.End()) {
		t.Fatal("PopBack() did not return End()")
	}

	assertValues(t, l, []int{2}, []int{2})
}

func TestClear(t *testing.T) {
	l := NewList[*int]()
	value1 := 1
	value2 := 2
	l.PushBack(&value1)
	l.PushBack(&value2)

	l.Clear()

	if !l.Empty() {
		t.Fatal("list is not empty after Clear()")
	}
	if l.Len() != 0 {
		t.Fatalf("Len() after Clear() = %d, want 0", l.Len())
	}
	if !l.Begin().Equal(l.End()) {
		t.Fatal("Begin() != End() after Clear()")
	}
}
