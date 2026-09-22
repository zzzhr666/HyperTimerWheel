package slot

import (
	"testing"

	"HyperTimerWheel/timer"
)

func newTestTimer(id timer.ID) *timer.Timer {
	return &timer.Timer{ID: id}
}

func assertTimers(t *testing.T, got, want []*timer.Timer) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("timer count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("timer[%d] = %p, want %p", i, got[i], want[i])
		}
	}
}

func TestLinkedListSlotAddAndTakeAll(t *testing.T) {
	s := newLinkedListSlot()
	first := newTestTimer(1)
	second := newTestTimer(2)
	third := newTestTimer(3)

	s.Add(first)
	s.Add(second)
	s.Add(third)

	got := s.TakeAll()
	assertTimers(t, got, []*timer.Timer{first, second, third})

	if got := s.TakeAll(); got != nil {
		t.Fatalf("TakeAll() after clear = %v, want nil", got)
	}
}

func TestLinkedListSlotInvalidate(t *testing.T) {
	tests := []struct {
		name   string
		remove int
		want   []*timer.Timer
	}{
		{name: "first", remove: 0, want: []*timer.Timer{}},
		{name: "middle", remove: 1, want: []*timer.Timer{}},
		{name: "last", remove: 2, want: []*timer.Timer{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newLinkedListSlot()
			timers := []*timer.Timer{
				newTestTimer(1),
				newTestTimer(2),
				newTestTimer(3),
			}
			for _, tm := range timers {
				s.Add(tm)
			}

			if !s.Invalidate(timers[tt.remove]) {
				t.Fatal("Invalidate() = false, want true")
			}
			if s.Invalidate(timers[tt.remove]) {
				t.Fatal("second Invalidate() = true, want false")
			}

			want := make([]*timer.Timer, 0, len(timers)-1)
			for i, tm := range timers {
				if i != tt.remove {
					want = append(want, tm)
				}
			}
			assertTimers(t, s.TakeAll(), want)
		})
	}
}

func TestLinkedListSlotInvalidateUnknownTimer(t *testing.T) {
	s := newLinkedListSlot()
	known := newTestTimer(1)
	unknown := newTestTimer(2)
	s.Add(known)

	if s.Invalidate(unknown) {
		t.Fatal("Invalidate(unknown) = true, want false")
	}
	assertTimers(t, s.TakeAll(), []*timer.Timer{known})
}

func TestLinkedListSlotRepeatedAddReplacesExistingNode(t *testing.T) {
	s := newLinkedListSlot()
	first := newTestTimer(1)
	second := newTestTimer(2)

	s.Add(first)
	s.Add(second)
	s.Add(first)

	assertTimers(t, s.TakeAll(), []*timer.Timer{second, first})
}

func TestLinkedListSlotCanBeReusedAfterTakeAll(t *testing.T) {
	s := newLinkedListSlot()
	first := newTestTimer(1)
	second := newTestTimer(2)

	s.Add(first)
	assertTimers(t, s.TakeAll(), []*timer.Timer{first})

	s.Add(second)
	assertTimers(t, s.TakeAll(), []*timer.Timer{second})
}

func TestLinkedListSlotImplementsSlot(t *testing.T) {
	var _ Slot = (*linkedListSlot)(nil)
}
