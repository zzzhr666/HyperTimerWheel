package timerwheel_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	timerwheel "HyperTimerWheel"
)

func newWheel(t *testing.T, tick time.Duration, slots int) *timerwheel.Wheel {
	t.Helper()

	maxDelay := tick * time.Duration(slots)
	start := time.Unix(0, 0)
	w, err := timerwheel.NewWheel(timerwheel.WheelConfig{
		BaseTick:      tick,
		MaxDelay:      maxDelay,
		SlotsPerLevel: slots,
		StartTime:     start,
	})
	if err != nil {
		t.Fatalf("NewWheel(%v, %v, %d) error = %v", tick, maxDelay, slots, err)
	}

	return w
}

func newHierarchicalWheel(t *testing.T, baseTick, maxDelay time.Duration, slots int) *timerwheel.Wheel {
	t.Helper()

	w, err := timerwheel.NewWheel(timerwheel.WheelConfig{
		BaseTick:      baseTick,
		MaxDelay:      maxDelay,
		SlotsPerLevel: slots,
		StartTime:     time.Unix(0, 0),
	})
	if err != nil {
		t.Fatalf("NewWheel(%v, %v, %d) error = %v", baseTick, maxDelay, slots, err)
	}

	return w
}

func mustSchedule(t *testing.T, w *timerwheel.Wheel, delay time.Duration, cb timerwheel.Callback) timerwheel.Handle {
	t.Helper()

	h, err := w.Schedule(delay, cb)
	if err != nil {
		t.Fatalf("Schedule(%v) error = %v", delay, err)
	}

	return h
}

func TestSingleWheelFiresInDeadlineOrder(t *testing.T) {
	w := newWheel(t, time.Millisecond, 8)
	start := time.Unix(0, 0)

	var got []string
	schedule := func(name string, delay time.Duration) {
		mustSchedule(t, w, delay, func(now time.Time) {
			if !now.Equal(start.Add(delay)) {
				t.Fatalf("%s callback now = %v, want %v", name, now, start.Add(delay))
			}
			got = append(got, name)
		})
	}

	schedule("3ms", 3*time.Millisecond)
	schedule("1ms", time.Millisecond)
	schedule("7ms", 7*time.Millisecond)

	if fired := w.Advance(start.Add(time.Millisecond)); fired != 1 {
		t.Fatalf("Advance(1ms) fired = %d, want 1", fired)
	}
	if got := append([]string(nil), got...); !equalStrings(got, []string{"1ms"}) {
		t.Fatalf("callbacks after 1ms = %v, want [1ms]", got)
	}

	if fired := w.Advance(start.Add(3 * time.Millisecond)); fired != 1 {
		t.Fatalf("Advance(3ms) fired = %d, want 1", fired)
	}
	if got := append([]string(nil), got...); !equalStrings(got, []string{"1ms", "3ms"}) {
		t.Fatalf("callbacks after 3ms = %v, want [1ms 3ms]", got)
	}

	if fired := w.Advance(start.Add(7 * time.Millisecond)); fired != 1 {
		t.Fatalf("Advance(7ms) fired = %d, want 1", fired)
	}
	if got := append([]string(nil), got...); !equalStrings(got, []string{"1ms", "3ms", "7ms"}) {
		t.Fatalf("callbacks after 7ms = %v, want [1ms 3ms 7ms]", got)
	}
}

func TestSingleWheelSameBucketFiresAll(t *testing.T) {
	w := newWheel(t, time.Millisecond, 8)
	start := time.Unix(0, 0)

	const count = 3
	fired := make([]bool, count)
	for i := 0; i < count; i++ {
		i := i
		mustSchedule(t, w, time.Millisecond, func(time.Time) {
			fired[i] = true
		})
	}

	if n := w.Advance(start.Add(time.Millisecond)); n != count {
		t.Fatalf("Advance(1ms) fired = %d, want %d", n, count)
	}
	for i, ok := range fired {
		if !ok {
			t.Fatalf("callback %d was not fired", i)
		}
	}

	if n := w.Advance(start.Add(2 * time.Millisecond)); n != 0 {
		t.Fatalf("Advance(2ms) fired again = %d, want 0", n)
	}
}

func TestSingleWheelCancel(t *testing.T) {
	w := newWheel(t, time.Millisecond, 8)
	start := time.Unix(0, 0)

	fired := false
	h := mustSchedule(t, w, 2*time.Millisecond, func(time.Time) {
		fired = true
	})

	if !w.Cancel(h) {
		t.Fatal("first Cancel() = false, want true")
	}
	if w.Cancel(h) {
		t.Fatal("second Cancel() = true, want false")
	}

	if n := w.Advance(start.Add(2 * time.Millisecond)); n != 0 {
		t.Fatalf("Advance(2ms) after cancel fired = %d, want 0", n)
	}
	if fired {
		t.Fatal("callback fired after cancel")
	}
}

func TestSingleWheelAdvanceSkipsTicks(t *testing.T) {
	w := newWheel(t, time.Millisecond, 8)
	start := time.Unix(0, 0)

	var got []string
	mustSchedule(t, w, 2*time.Millisecond, func(time.Time) {
		got = append(got, "2ms")
	})
	mustSchedule(t, w, 5*time.Millisecond, func(time.Time) {
		got = append(got, "5ms")
	})

	if n := w.Advance(start.Add(6 * time.Millisecond)); n != 2 {
		t.Fatalf("Advance(6ms) fired = %d, want 2", n)
	}
	if !equalStrings(got, []string{"2ms", "5ms"}) {
		t.Fatalf("callbacks = %v, want [2ms 5ms]", got)
	}
}

func TestSingleWheelRejectsInvalidDelay(t *testing.T) {
	w := newWheel(t, time.Millisecond, 8)

	if _, err := w.Schedule(0, func(time.Time) {}); err == nil {
		t.Fatal("Schedule(0) error = nil, want non-nil")
	}
	if _, err := w.Schedule(8*time.Millisecond, func(time.Time) {}); err == nil {
		t.Fatal("Schedule(8ms) error = nil, want non-nil because delay must be smaller than tick*slots")
	}
}

func TestHierarchicalWheelLongDelaysFireAtBasePrecision(t *testing.T) {
	const slots = 8
	baseTick := time.Millisecond
	maxDelay := 100 * time.Millisecond

	tests := []struct {
		name  string
		delay time.Duration
	}{
		{"15ms", 15 * time.Millisecond},
		{"22ms", 22 * time.Millisecond},
		{"31ms", 31 * time.Millisecond},
		{"63ms", 63 * time.Millisecond},
		{"71ms", 71 * time.Millisecond},
		{"90ms", 90 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newHierarchicalWheel(t, baseTick, maxDelay, slots)
			start := time.Unix(0, 0)

			var got time.Time
			mustSchedule(t, w, tt.delay, func(now time.Time) {
				got = now
			})

			want := start.Add(tt.delay)
			if fired := w.Advance(want); fired != 1 {
				t.Fatalf("Advance(%v) fired = %d, want 1", want, fired)
			}
			if !got.Equal(want) {
				t.Fatalf("callback now = %v, want %v", got, want)
			}
		})
	}
}

func TestHierarchicalWheelNonIntegerDelayRoundsUpToNextTick(t *testing.T) {
	w := newHierarchicalWheel(t, time.Millisecond, 20*time.Millisecond, 8)
	start := time.Unix(0, 0)

	var got time.Time
	mustSchedule(t, w, 1500*time.Microsecond, func(now time.Time) {
		got = now
	})

	if fired := w.Advance(start.Add(time.Millisecond)); fired != 0 {
		t.Fatalf("Advance(1ms) fired = %d, want 0", fired)
	}

	if fired := w.Advance(start.Add(2 * time.Millisecond)); fired != 1 {
		t.Fatalf("Advance(2ms) fired = %d, want 1", fired)
	}
	if want := start.Add(2 * time.Millisecond); !got.Equal(want) {
		t.Fatalf("callback now = %v, want %v", got, want)
	}
}

func TestHierarchicalWheelCancelAcrossLevels(t *testing.T) {
	w := newHierarchicalWheel(t, time.Millisecond, 100*time.Millisecond, 8)
	start := time.Unix(0, 0)

	fired := false
	h := mustSchedule(t, w, 50*time.Millisecond, func(time.Time) {
		fired = true
	})

	if !w.Cancel(h) {
		t.Fatal("first Cancel() = false, want true")
	}

	if fired := w.Advance(start.Add(100 * time.Millisecond)); fired != 0 {
		t.Fatalf("Advance(100ms) after cancel fired = %d, want 0", fired)
	}
	if fired {
		t.Fatal("callback fired after cancel")
	}
}

func TestHierarchicalWheelRejectsDelayAtMaxDelay(t *testing.T) {
	w := newHierarchicalWheel(t, time.Millisecond, 100*time.Millisecond, 8)

	if _, err := w.Schedule(100*time.Millisecond, func(time.Time) {}); err == nil {
		t.Fatal("Schedule(maxDelay) error = nil, want non-nil")
	}
}

func TestWheelSlotTypeSelection(t *testing.T) {
	if _, err := timerwheel.NewWheel(timerwheel.WheelConfig{
		BaseTick:      time.Millisecond,
		MaxDelay:      100 * time.Millisecond,
		SlotsPerLevel: 8,
		SlotType:      timerwheel.SlotTypeSlice,
	}); err != nil {
		t.Fatalf("NewWheel(slice slot) error = %v", err)
	}

	if _, err := timerwheel.NewWheel(timerwheel.WheelConfig{
		BaseTick:      time.Millisecond,
		MaxDelay:      100 * time.Millisecond,
		SlotsPerLevel: 8,
		SlotType:      timerwheel.SlotTypeLinkedList,
	}); err != timerwheel.ErrUnsupportedSlot {
		t.Fatalf("NewWheel(linked-list slot) error = %v, want %v", err, timerwheel.ErrUnsupportedSlot)
	}
}

func TestConcurrentScheduleAndAdvance(t *testing.T) {
	const (
		producers   = 32
		perProducer = 100
		total       = producers * perProducer
	)

	w := newHierarchicalWheel(t, time.Millisecond, 100*time.Millisecond, 8)
	start := time.Unix(0, 0)

	var fired atomic.Int32
	var wg sync.WaitGroup

	for p := 0; p < producers; p++ {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()

			for i := 0; i < perProducer; i++ {
				delay := time.Duration((p+i)%50+1) * time.Millisecond
				_, err := w.Schedule(delay, func(time.Time) {
					fired.Add(1)
				})
				if err != nil {
					t.Errorf("Schedule(%v) error = %v", delay, err)
					return
				}
			}
		}(p)
	}

	wg.Wait()

	if n := w.Advance(start.Add(100 * time.Millisecond)); n != total {
		t.Fatalf("Advance(100ms) fired = %d, want %d", n, total)
	}
	if got := fired.Load(); got != total {
		t.Fatalf("callback count = %d, want %d", got, total)
	}
}

func TestConcurrentCancelAndAdvance(t *testing.T) {
	const total = 2000

	w := newHierarchicalWheel(t, time.Millisecond, 10*time.Millisecond, 8)
	start := time.Unix(0, 0)
	handles := make([]timerwheel.Handle, total)

	var fired atomic.Int32
	var scheduleWG sync.WaitGroup

	for i := 0; i < total; i++ {
		scheduleWG.Add(1)
		go func(i int) {
			defer scheduleWG.Done()

			delay := time.Duration(i%5+1) * time.Millisecond
			h, err := w.Schedule(delay, func(time.Time) {
				fired.Add(1)
			})
			if err != nil {
				t.Errorf("Schedule(%v) error = %v", delay, err)
				return
			}

			handles[i] = h
		}(i)
	}
	scheduleWG.Wait()

	var advanceWG sync.WaitGroup
	advanceWG.Add(1)
	go func() {
		defer advanceWG.Done()
		w.Advance(start.Add(10 * time.Millisecond))
	}()

	var canceled atomic.Int32
	var cancelWG sync.WaitGroup
	for i := 0; i < total; i++ {
		cancelWG.Add(1)
		go func(i int) {
			defer cancelWG.Done()

			if w.Cancel(handles[i]) {
				canceled.Add(1)
			}
		}(i)
	}
	cancelWG.Wait()
	advanceWG.Wait()

	got := fired.Load() + canceled.Load()
	if got != total {
		t.Fatalf("fired(%d) + canceled(%d) = %d, want %d", fired.Load(), canceled.Load(), got, total)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
