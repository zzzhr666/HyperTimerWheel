package timerwheel

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestCallbackPanicIsRecoveredInline(t *testing.T) {
	start := time.Unix(0, 0)
	panicInfo := make(chan CallbackPanicInfo, 1)
	w, err := NewWheel(WheelConfig{
		BaseTick:             time.Millisecond,
		MaxDelay:             10 * time.Millisecond,
		SlotsPerLevel:        8,
		StartTime:            start,
		CallbackPanicHandler: func(info CallbackPanicInfo) { panicInfo <- info },
	})
	if err != nil {
		t.Fatalf("NewWheel() error = %v", err)
	}
	defer w.Close()

	var healthy atomic.Int32
	if _, err := w.Schedule(time.Millisecond, func(time.Time) {
		panic("boom")
	}); err != nil {
		t.Fatalf("Schedule(panic) error = %v", err)
	}
	if _, err := w.Schedule(time.Millisecond, func(time.Time) {
		healthy.Add(1)
	}); err != nil {
		t.Fatalf("Schedule(healthy) error = %v", err)
	}

	if fired := w.Advance(start.Add(time.Millisecond)); fired != 2 {
		t.Fatalf("Advance() fired = %d, want 2", fired)
	}
	if got := healthy.Load(); got != 1 {
		t.Fatalf("healthy callback count = %d, want 1", got)
	}

	select {
	case info := <-panicInfo:
		if info.ID == InvalidTimerID {
			t.Fatal("panic info ID is invalid")
		}
		if info.ExecuteAt != start.Add(time.Millisecond) {
			t.Fatalf("ExecuteAt = %v, want %v", info.ExecuteAt, start.Add(time.Millisecond))
		}
		if info.PanicValue != "boom" {
			t.Fatalf("PanicValue = %v, want boom", info.PanicValue)
		}
		if len(info.Stack) == 0 {
			t.Fatal("Stack is empty")
		}
	default:
		t.Fatal("panic handler was not called")
	}
}

func TestCallbackPanicIsRecoveredInWorkerPool(t *testing.T) {
	start := time.Unix(0, 0)
	panicInfo := make(chan CallbackPanicInfo, 1)
	w, err := NewWheel(WheelConfig{
		BaseTick:      time.Millisecond,
		MaxDelay:      10 * time.Millisecond,
		SlotsPerLevel: 8,
		StartTime:     start,
		WorkerPoolConfig: WorkerPoolConfig{
			WorkerNum: 1,
			QueueSize: 8,
		},
		CallbackPanicHandler: func(info CallbackPanicInfo) { panicInfo <- info },
	})
	if err != nil {
		t.Fatalf("NewWheel() error = %v", err)
	}
	defer w.Close()

	var healthy atomic.Int32
	if _, err := w.Schedule(time.Millisecond, func(time.Time) {
		panic("worker boom")
	}); err != nil {
		t.Fatalf("Schedule(panic) error = %v", err)
	}
	if _, err := w.Schedule(time.Millisecond, func(time.Time) {
		healthy.Add(1)
	}); err != nil {
		t.Fatalf("Schedule(healthy) error = %v", err)
	}

	if fired := w.Advance(start.Add(time.Millisecond)); fired != 2 {
		t.Fatalf("Advance() fired = %d, want 2", fired)
	}

	select {
	case info := <-panicInfo:
		if info.PanicValue != "worker boom" {
			t.Fatalf("PanicValue = %v, want worker boom", info.PanicValue)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for panic handler")
	}
	deadline := time.Now().Add(time.Second)
	for healthy.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := healthy.Load(); got != 1 {
		t.Fatalf("healthy callback count = %d, want 1", got)
	}
}

func TestCallbackPanicIsRecoveredByInlineFallback(t *testing.T) {
	start := time.Unix(0, 0)
	panicInfo := make(chan CallbackPanicInfo, 1)
	w, err := NewWheel(WheelConfig{
		BaseTick:             time.Millisecond,
		MaxDelay:             10 * time.Millisecond,
		SlotsPerLevel:        8,
		StartTime:            start,
		CallbackPanicHandler: func(info CallbackPanicInfo) { panicInfo <- info },
	})
	if err != nil {
		t.Fatalf("NewWheel() error = %v", err)
	}
	defer w.Close()

	pool := NewWorkerPool(WorkerPoolConfig{WorkerNum: 1, QueueSize: 1})
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

	if _, err := w.Schedule(time.Millisecond, func(time.Time) {
		panic("fallback boom")
	}); err != nil {
		t.Fatalf("Schedule() error = %v", err)
	}
	if fired := w.Advance(start.Add(time.Millisecond)); fired != 1 {
		t.Fatalf("Advance() fired = %d, want 1", fired)
	}

	select {
	case info := <-panicInfo:
		if info.PanicValue != "fallback boom" {
			t.Fatalf("PanicValue = %v, want fallback boom", info.PanicValue)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for panic handler")
	}

	close(release)
	pool.Close()
	w.workerPool = nil
}
