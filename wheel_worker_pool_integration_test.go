package timerwheel

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newWheelForWorkerPoolTest(t *testing.T) *Wheel {
	t.Helper()

	w, err := NewWheel(WheelConfig{
		BaseTick:      time.Millisecond,
		MaxDelay:      10 * time.Millisecond,
		SlotsPerLevel: 8,
	})
	if err != nil {
		t.Fatalf("NewWheel() error = %v", err)
	}

	return w
}

func TestWheelWorkerPoolExecutesCallbacksAsync(t *testing.T) {
	w := newWheelForWorkerPoolTest(t)
	pool := NewWorkerPool(WorkerPoolConfig{WorkerNum: 4, QueueSize: 128})
	if pool == nil {
		t.Fatal("NewWorkerPool() returned nil")
	}
	w.workerPool = pool

	const total = 100

	var executed atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < total; i++ {
		delay := time.Duration(i%8+1) * time.Millisecond
		wg.Add(1)

		_, err := w.Schedule(delay, func(time.Time) {
			executed.Add(1)
			wg.Done()
		})
		if err != nil {
			t.Fatalf("Schedule(%v) error = %v", delay, err)
		}
	}

	if fired := w.Advance(time.Time{}.Add(10 * time.Millisecond)); fired != total {
		t.Fatalf("Advance(10ms) fired = %d, want %d", fired, total)
	}

	wg.Wait()
	if got := executed.Load(); got != total {
		t.Fatalf("callback executed = %d, want %d", got, total)
	}

	pool.Close()
}

func TestWheelWorkerPoolPreservesFireTime(t *testing.T) {
	w := newWheelForWorkerPoolTest(t)
	pool := NewWorkerPool(WorkerPoolConfig{WorkerNum: 1, QueueSize: 16})
	if pool == nil {
		t.Fatal("NewWorkerPool() returned nil")
	}
	w.workerPool = pool

	start := time.Time{}

	var mu sync.Mutex
	got := make(map[string]time.Time)
	var wg sync.WaitGroup

	schedule := func(name string, delay time.Duration) {
		wg.Add(1)

		_, err := w.Schedule(delay, func(now time.Time) {
			mu.Lock()
			got[name] = now
			mu.Unlock()
			wg.Done()
		})
		if err != nil {
			t.Fatalf("Schedule(%v) error = %v", delay, err)
		}
	}

	schedule("1ms", time.Millisecond)
	schedule("3ms", 3*time.Millisecond)

	if fired := w.Advance(start.Add(3 * time.Millisecond)); fired != 2 {
		t.Fatalf("Advance(3ms) fired = %d, want 2", fired)
	}

	wg.Wait()

	if got["1ms"] != start.Add(time.Millisecond) {
		t.Fatalf("1ms callback now = %v, want %v", got["1ms"], start.Add(time.Millisecond))
	}
	if got["3ms"] != start.Add(3*time.Millisecond) {
		t.Fatalf("3ms callback now = %v, want %v", got["3ms"], start.Add(3*time.Millisecond))
	}

	pool.Close()
}

func TestWheelWorkerPoolFallsBackInlineWhenQueueFull(t *testing.T) {
	w := newWheelForWorkerPoolTest(t)
	pool := NewWorkerPool(WorkerPoolConfig{WorkerNum: 1, QueueSize: 1})
	if pool == nil {
		t.Fatal("NewWorkerPool() returned nil")
	}
	w.workerPool = pool

	started := make(chan struct{})
	release := make(chan struct{})

	if !pool.Submit(func() {
		close(started)
		<-release
	}) {
		t.Fatal("Submit(blocking task) = false, want true")
	}

	<-started

	if !pool.Submit(func() {}) {
		t.Fatal("Submit(buffer filler) = false, want true")
	}
	if pool.Submit(func() {}) {
		t.Fatal("Submit(on full queue) = true, want false")
	}

	ranInline := false
	_, err := w.Schedule(time.Millisecond, func(time.Time) {
		ranInline = true
	})
	if err != nil {
		t.Fatalf("Schedule(1ms) error = %v", err)
	}

	if fired := w.Advance(time.Time{}.Add(time.Millisecond)); fired != 1 {
		t.Fatalf("Advance(1ms) fired = %d, want 1", fired)
	}
	if !ranInline {
		t.Fatal("callback was not executed inline when worker pool queue was full")
	}

	close(release)
	pool.Close()
}

func TestWheelWorkerPoolRespectsCancelBeforeAdvance(t *testing.T) {
	w := newWheelForWorkerPoolTest(t)
	pool := NewWorkerPool(WorkerPoolConfig{WorkerNum: 4, QueueSize: 128})
	if pool == nil {
		t.Fatal("NewWorkerPool() returned nil")
	}
	w.workerPool = pool

	const total = 100

	handles := make([]Handle, total)
	fired := make(chan struct{}, total)

	for i := 0; i < total; i++ {
		h, err := w.Schedule(time.Millisecond, func(time.Time) {
			fired <- struct{}{}
		})
		if err != nil {
			t.Fatalf("Schedule(1ms) error = %v", err)
		}
		handles[i] = h
	}

	for i := 0; i < total; i += 2 {
		if !w.Cancel(handles[i]) {
			t.Fatalf("Cancel(handle %d) = false, want true", i)
		}
	}

	if got := w.Advance(time.Time{}.Add(time.Millisecond)); got != total/2 {
		t.Fatalf("Advance(1ms) fired = %d, want %d", got, total/2)
	}

	for i := 0; i < total/2; i++ {
		select {
		case <-fired:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for worker pool callback")
		}
	}

	select {
	case <-fired:
		t.Fatal("unexpected extra callback")
	default:
	}

	pool.Close()
}
