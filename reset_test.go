package timerwheel_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	timerwheel "HyperTimerWheel"
)

func TestResetChangesDeadlineEarlier(t *testing.T) {
	w := newHierarchicalWheel(t, time.Millisecond, 100*time.Millisecond, 8)
	start := time.Unix(0, 0)

	var got time.Time
	h := mustSchedule(t, w, 10*time.Millisecond, func(now time.Time) {
		got = now
	})

	if !w.Reset(h, 3*time.Millisecond) {
		t.Fatal("Reset(h, 3ms) = false, want true")
	}

	if fired := w.Advance(start.Add(3 * time.Millisecond)); fired != 1 {
		t.Fatalf("Advance(3ms) fired = %d, want 1", fired)
	}
	if want := start.Add(3 * time.Millisecond); !got.Equal(want) {
		t.Fatalf("callback now = %v, want %v", got, want)
	}

	if fired := w.Advance(start.Add(10 * time.Millisecond)); fired != 0 {
		t.Fatalf("Advance(10ms) after reset fired = %d, want 0", fired)
	}
}

func TestResetChangesDeadlineLater(t *testing.T) {
	w := newHierarchicalWheel(t, time.Millisecond, 100*time.Millisecond, 8)
	start := time.Unix(0, 0)

	var got time.Time
	h := mustSchedule(t, w, 3*time.Millisecond, func(now time.Time) {
		got = now
	})

	if !w.Reset(h, 10*time.Millisecond) {
		t.Fatal("Reset(h, 10ms) = false, want true")
	}

	if fired := w.Advance(start.Add(3 * time.Millisecond)); fired != 0 {
		t.Fatalf("Advance(3ms) fired = %d, want 0", fired)
	}

	if fired := w.Advance(start.Add(10 * time.Millisecond)); fired != 1 {
		t.Fatalf("Advance(10ms) fired = %d, want 1", fired)
	}
	if want := start.Add(10 * time.Millisecond); !got.Equal(want) {
		t.Fatalf("callback now = %v, want %v", got, want)
	}
}

func TestResetRejectsInvalidDelay(t *testing.T) {
	w := newHierarchicalWheel(t, time.Millisecond, 100*time.Millisecond, 8)

	h := mustSchedule(t, w, 10*time.Millisecond, func(time.Time) {})

	if w.Reset(h, 0) {
		t.Fatal("Reset(h, 0) = true, want false")
	}
	if w.Reset(h, 100*time.Millisecond) {
		t.Fatal("Reset(h, maxDelay) = true, want false")
	}
}

func TestResetAfterCancelReturnsFalse(t *testing.T) {
	w := newHierarchicalWheel(t, time.Millisecond, 100*time.Millisecond, 8)
	start := time.Unix(0, 0)

	fired := false
	h := mustSchedule(t, w, 10*time.Millisecond, func(time.Time) {
		fired = true
	})

	if !w.Cancel(h) {
		t.Fatal("Cancel(h) = false, want true")
	}
	if w.Reset(h, 3*time.Millisecond) {
		t.Fatal("Reset after cancel = true, want false")
	}

	if fired := w.Advance(start.Add(10 * time.Millisecond)); fired != 0 {
		t.Fatalf("Advance after cancel fired = %d, want 0", fired)
	}
	if fired {
		t.Fatal("callback fired after cancel")
	}
}

func TestResetAfterFiredReturnsFalse(t *testing.T) {
	w := newHierarchicalWheel(t, time.Millisecond, 100*time.Millisecond, 8)
	start := time.Unix(0, 0)

	h := mustSchedule(t, w, time.Millisecond, func(time.Time) {})

	if fired := w.Advance(start.Add(time.Millisecond)); fired != 1 {
		t.Fatalf("Advance(1ms) fired = %d, want 1", fired)
	}

	if w.Reset(h, 3*time.Millisecond) {
		t.Fatal("Reset after fired = true, want false")
	}
}

func TestResetMultipleTimesUsesLastDelay(t *testing.T) {
	w := newHierarchicalWheel(t, time.Millisecond, 100*time.Millisecond, 8)
	start := time.Unix(0, 0)

	var got time.Time
	h := mustSchedule(t, w, 10*time.Millisecond, func(now time.Time) {
		got = now
	})

	if !w.Reset(h, 3*time.Millisecond) {
		t.Fatal("first Reset = false, want true")
	}
	if !w.Reset(h, 5*time.Millisecond) {
		t.Fatal("second Reset = false, want true")
	}

	if fired := w.Advance(start.Add(3 * time.Millisecond)); fired != 0 {
		t.Fatalf("Advance(3ms) fired = %d, want 0", fired)
	}

	if fired := w.Advance(start.Add(5 * time.Millisecond)); fired != 1 {
		t.Fatalf("Advance(5ms) fired = %d, want 1", fired)
	}
	if want := start.Add(5 * time.Millisecond); !got.Equal(want) {
		t.Fatalf("callback now = %v, want %v", got, want)
	}

	if fired := w.Advance(start.Add(10 * time.Millisecond)); fired != 0 {
		t.Fatalf("Advance(10ms) fired again = %d, want 0", fired)
	}
}

func TestConcurrentResetsBeforeAdvance(t *testing.T) {
	const total = 500

	w := newHierarchicalWheel(t, time.Millisecond, 100*time.Millisecond, 8)
	start := time.Unix(0, 0)
	handles := make([]timerwheel.Handle, total)

	var fired atomic.Int32
	for i := 0; i < total; i++ {
		h := mustSchedule(t, w, 10*time.Millisecond, func(time.Time) {
			fired.Add(1)
		})
		handles[i] = h
	}

	var wg sync.WaitGroup
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			if !w.Reset(handles[i], 3*time.Millisecond) {
				t.Errorf("Reset(handle %d) = false, want true", i)
			}
		}(i)
	}
	wg.Wait()

	if n := w.Advance(start.Add(3 * time.Millisecond)); n != total {
		t.Fatalf("Advance(3ms) fired = %d, want %d", n, total)
	}
	if got := fired.Load(); got != total {
		t.Fatalf("callback count = %d, want %d", got, total)
	}

	if n := w.Advance(start.Add(10 * time.Millisecond)); n != 0 {
		t.Fatalf("Advance(10ms) fired stale timer = %d, want 0", n)
	}
}

func TestConcurrentResetAndAdvance(t *testing.T) {
	const total = 1000

	w := newHierarchicalWheel(t, time.Millisecond, 10*time.Millisecond, 8)
	start := time.Unix(0, 0)
	handles := make([]timerwheel.Handle, total)

	var fired atomic.Int32
	for i := 0; i < total; i++ {
		h := mustSchedule(t, w, time.Millisecond, func(time.Time) {
			fired.Add(1)
		})
		handles[i] = h
	}

	var advanceWG sync.WaitGroup
	advanceWG.Add(1)
	go func() {
		defer advanceWG.Done()
		w.Advance(start.Add(10 * time.Millisecond))
	}()

	var resetWG sync.WaitGroup
	for i := 0; i < total; i++ {
		resetWG.Add(1)
		go func(i int) {
			defer resetWG.Done()

			delay := time.Duration(i%4+2) * time.Millisecond
			w.Reset(handles[i], delay)
		}(i)
	}

	resetWG.Wait()
	advanceWG.Wait()

	if got := fired.Load(); got != total {
		t.Fatalf("fired = %d, want %d", got, total)
	}
}
