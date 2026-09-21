package timerwheel_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	timerwheel "HyperTimerWheel"
)

func TestWorkerPoolExecutesTasks(t *testing.T) {
	const (
		workers = 4
		tasks   = 100
	)

	pool := timerwheel.NewWorkerPool(timerwheel.WorkerPoolConfig{WorkerNum: workers, QueueSize: tasks})

	var executed atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < tasks; i++ {
		wg.Add(1)
		if !pool.Submit(func() {
			defer wg.Done()
			executed.Add(1)
		}) {
			wg.Done()
			t.Fatal("Submit(task) = false, want true")
		}
	}

	wg.Wait()
	if got := executed.Load(); got != tasks {
		t.Fatalf("executed = %d, want %d", got, tasks)
	}

	pool.Close()
}

func TestWorkerPoolSubmitReturnsFalseWhenFull(t *testing.T) {
	pool := timerwheel.NewWorkerPool(timerwheel.WorkerPoolConfig{WorkerNum: 1, QueueSize: 2})

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
		t.Fatal("first buffered Submit = false, want true")
	}
	if !pool.Submit(func() {}) {
		t.Fatal("second buffered Submit = false, want true")
	}
	if pool.Submit(func() {}) {
		t.Fatal("Submit on full queue = true, want false")
	}

	close(release)
	pool.Close()
}

func TestWorkerPoolCloseWaitsForTasks(t *testing.T) {
	const tasks = 20

	pool := timerwheel.NewWorkerPool(timerwheel.WorkerPoolConfig{WorkerNum: 2, QueueSize: tasks})

	var wg sync.WaitGroup
	for i := 0; i < tasks; i++ {
		wg.Add(1)
		if !pool.Submit(func() {
			defer wg.Done()
			time.Sleep(time.Millisecond)
		}) {
			wg.Done()
			t.Fatal("Submit(task) = false, want true")
		}
	}

	pool.Close()
	wg.Wait()
}
