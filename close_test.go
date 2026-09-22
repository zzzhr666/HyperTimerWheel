package timerwheel_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	timerwheel "HyperTimerWheel"
)

func TestWheelCloseRejectsSchedule(t *testing.T) {
	w := newHierarchicalWheel(t, time.Millisecond, 100*time.Millisecond, 8)

	w.Close()

	if _, err := w.Schedule(time.Millisecond, func(time.Time) {}); err != timerwheel.ErrClosed {
		t.Fatalf("Schedule after Close() error = %v, want %v", err, timerwheel.ErrClosed)
	}
}

func TestWheelCloseRejectsReset(t *testing.T) {
	w := newHierarchicalWheel(t, time.Millisecond, 100*time.Millisecond, 8)
	h := mustSchedule(t, w, 10*time.Millisecond, func(time.Time) {})

	w.Close()

	if w.Reset(h, time.Millisecond) {
		t.Fatal("Reset after Close() = true, want false")
	}
}

func TestWheelCloseRejectsCancel(t *testing.T) {
	w := newHierarchicalWheel(t, time.Millisecond, 100*time.Millisecond, 8)
	h := mustSchedule(t, w, 10*time.Millisecond, func(time.Time) {})

	w.Close()

	if w.Cancel(h) {
		t.Fatal("Cancel after Close() = true, want false")
	}
}

func TestWheelCloseMakesAdvanceNoop(t *testing.T) {
	w := newHierarchicalWheel(t, time.Millisecond, 100*time.Millisecond, 8)
	mustSchedule(t, w, time.Millisecond, func(time.Time) {
		t.Error("callback executed after Close()")
	})

	w.Close()

	if fired := w.Advance(time.Unix(0, 0).Add(time.Second)); fired != 0 {
		t.Fatalf("Advance after Close() fired = %d, want 0", fired)
	}
}

func TestWheelCloseIsIdempotent(t *testing.T) {
	w := newHierarchicalWheel(t, time.Millisecond, 100*time.Millisecond, 8)

	w.Close()
	w.Close()
}

func TestWheelCloseIsSafeForConcurrentCallers(t *testing.T) {
	w := newHierarchicalWheel(t, time.Millisecond, 100*time.Millisecond, 8)

	const callers = 32
	var wg sync.WaitGroup
	wg.Add(callers)

	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			w.Close()
		}()
	}

	wg.Wait()

	if _, err := w.Schedule(time.Millisecond, func(time.Time) {}); err != timerwheel.ErrClosed {
		t.Fatalf("Schedule after concurrent Close() error = %v, want %v", err, timerwheel.ErrClosed)
	}
}

func TestWheelCloseWaitsForWorkerPoolCallbacks(t *testing.T) {
	start := time.Unix(0, 0)
	w, err := timerwheel.NewWheel(timerwheel.WheelConfig{
		BaseTick:      time.Millisecond,
		MaxDelay:      100 * time.Millisecond,
		SlotsPerLevel: 8,
		SlotType:      timerwheel.SlotTypeSlice,
		StartTime:     start,
		WorkerPoolConfig: timerwheel.WorkerPoolConfig{
			WorkerNum: 1,
			QueueSize: 1,
		},
	})
	if err != nil {
		t.Fatalf("NewWheel() error = %v", err)
	}

	callbackStarted := make(chan struct{})
	releaseCallback := make(chan struct{})
	var callbackFinished atomic.Bool

	if _, err := w.Schedule(time.Millisecond, func(time.Time) {
		close(callbackStarted)
		<-releaseCallback
		callbackFinished.Store(true)
	}); err != nil {
		t.Fatalf("Schedule() error = %v", err)
	}

	if fired := w.Advance(start.Add(time.Millisecond)); fired != 1 {
		t.Fatalf("Advance(1ms) fired = %d, want 1", fired)
	}

	select {
	case <-callbackStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for callback to start")
	}

	closeFinished := make(chan struct{})
	go func() {
		w.Close()
		close(closeFinished)
	}()

	select {
	case <-closeFinished:
		t.Fatal("Close() returned before callback finished")
	case <-time.After(20 * time.Millisecond):
	}

	close(releaseCallback)

	select {
	case <-closeFinished:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Close()")
	}

	if !callbackFinished.Load() {
		t.Fatal("Close() returned before callback completed")
	}
}

func TestWheelCloseDiscardsQueuedTimers(t *testing.T) {
	w := newHierarchicalWheel(t, time.Millisecond, 100*time.Millisecond, 8)

	var fired atomic.Int32
	mustSchedule(t, w, time.Millisecond, func(time.Time) {
		fired.Add(1)
	})

	w.Close()

	if n := w.Advance(time.Unix(0, 0).Add(time.Second)); n != 0 {
		t.Fatalf("Advance after Close() fired = %d, want 0", n)
	}
	if got := fired.Load(); got != 0 {
		t.Fatalf("callbacks after Close() = %d, want 0", got)
	}
}
