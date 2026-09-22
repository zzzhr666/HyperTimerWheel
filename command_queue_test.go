package timerwheel_test

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	timerwheel "HyperTimerWheel"
)

func newWheelWithCommandCapacity(t *testing.T, capacity int) *timerwheel.Wheel {
	t.Helper()

	w, err := timerwheel.NewWheel(timerwheel.WheelConfig{
		BaseTick:        time.Millisecond,
		MaxDelay:        100 * time.Millisecond,
		SlotsPerLevel:   8,
		SlotType:        timerwheel.SlotTypeSlice,
		StartTime:       time.Unix(0, 0),
		CommandCapacity: capacity,
	})
	if err != nil {
		t.Fatalf("NewWheel() error = %v", err)
	}

	return w
}

func TestScheduleReturnsQueueFullInsteadOfBlocking(t *testing.T) {
	w := newWheelWithCommandCapacity(t, 1)
	defer w.Close()

	if _, err := w.Schedule(time.Millisecond, func(time.Time) {}); err != nil {
		t.Fatalf("first Schedule() error = %v, want nil", err)
	}

	if _, err := w.Schedule(time.Millisecond, func(time.Time) {}); err != timerwheel.ErrCommandQueueFull {
		t.Fatalf("second Schedule() error = %v, want %v", err, timerwheel.ErrCommandQueueFull)
	}
}

func TestResetReturnsFalseWhenCommandQueueIsFull(t *testing.T) {
	w := newWheelWithCommandCapacity(t, 1)
	defer w.Close()

	h := mustSchedule(t, w, 10*time.Millisecond, func(time.Time) {})

	if w.Reset(h, time.Millisecond) {
		t.Fatal("Reset() = true with full command queue, want false")
	}
}

func TestAdvanceDrainsCommandQueueAndAllowsNewSchedule(t *testing.T) {
	start := time.Unix(0, 0)
	w := newWheelWithCommandCapacity(t, 1)
	defer w.Close()

	fired := make(chan struct{}, 1)
	if _, err := w.Schedule(time.Millisecond, func(time.Time) {
		fired <- struct{}{}
	}); err != nil {
		t.Fatalf("first Schedule() error = %v, want nil", err)
	}

	if _, err := w.Schedule(time.Millisecond, func(time.Time) {}); err != timerwheel.ErrCommandQueueFull {
		t.Fatalf("second Schedule() error = %v, want %v", err, timerwheel.ErrCommandQueueFull)
	}

	if got := w.Advance(start.Add(time.Millisecond)); got != 1 {
		t.Fatalf("Advance(1ms) fired = %d, want 1", got)
	}

	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for callback")
	}

	if _, err := w.Schedule(time.Millisecond, func(time.Time) {}); err != nil {
		t.Fatalf("Schedule() after Advance error = %v, want nil", err)
	}
}

func TestNonPositiveCommandCapacityUsesDefault(t *testing.T) {
	for _, capacity := range []int{0, -1} {
		t.Run(fmt.Sprintf("capacity_%d", capacity), func(t *testing.T) {
			w := newWheelWithCommandCapacity(t, capacity)
			defer w.Close()

			for i := 0; i < 2; i++ {
				if _, err := w.Schedule(time.Millisecond, func(time.Time) {}); err != nil {
					t.Fatalf("Schedule(%d) error = %v, want nil", i, err)
				}
			}
		})
	}
}

func TestRecommendedConcurrentUsage(t *testing.T) {
	const (
		producers   = 16
		perProducer = 40
		total       = producers * perProducer
	)

	w := newWheelWithCommandCapacity(t, total+1)
	defer w.Close()

	var scheduled atomic.Int32
	var wg sync.WaitGroup
	for p := 0; p < producers; p++ {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()

			for i := 0; i < perProducer; i++ {
				delay := time.Duration((p+i)%50+1) * time.Millisecond
				if _, err := w.Schedule(delay, func(time.Time) {
					scheduled.Add(1)
				}); err != nil {
					t.Errorf("Schedule(%v) error = %v", delay, err)
					return
				}
			}
		}(p)
	}

	wg.Wait()

	if fired := w.Advance(time.Unix(0, 0).Add(100 * time.Millisecond)); fired != total {
		t.Fatalf("Advance(100ms) fired = %d, want %d", fired, total)
	}
	if got := scheduled.Load(); got != total {
		t.Fatalf("callback count = %d, want %d", got, total)
	}

	// The supported shutdown order is: stop producers, wait for them, then Close.
	w.Close()
}
