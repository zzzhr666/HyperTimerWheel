package main

import (
	"flag"
	"fmt"
	"runtime"
	"sort"
	"sync/atomic"
	"time"

	timerwheel "HyperTimerWheel"
)

type benchmarkConfig struct {
	timerCount int
	baseTick   time.Duration
	maxDelay   time.Duration
	slots      int
}

type benchmarkResult struct {
	scenario     string
	slot         string
	operations   int
	elapsed      time.Duration
	allocs       uint64
	bytes        uint64
	callbacks    int32
	p50          float64
	p95          float64
	p99          float64
	hasQuantiles bool
}

func main() {
	config := benchmarkConfig{}
	flag.IntVar(&config.timerCount, "timers", 10000, "number of timers used by each scenario")
	flag.DurationVar(&config.baseTick, "base-tick", time.Millisecond, "base wheel tick")
	flag.DurationVar(&config.maxDelay, "max-delay", time.Second, "maximum timer delay")
	flag.IntVar(&config.slots, "slots", 64, "slots per level")
	staleRounds := flag.Int("stale-rounds", 10, "number of reset rounds for stale-entry scenario")
	staleRatio := flag.Float64("stale-ratio", 0.9, "fraction of timers repeatedly reset in stale-entry scenario")
	flag.Parse()

	if config.timerCount <= 0 || config.baseTick <= 0 || config.maxDelay <= config.baseTick ||
		config.slots <= 0 || *staleRounds < 0 || *staleRatio < 0 || *staleRatio > 1 {
		panic("invalid benchmark configuration")
	}

	fmt.Println("HyperTimerWheel benchmark")
	fmt.Printf("timers=%d baseTick=%v maxDelay=%v slotsPerLevel=%d\n\n",
		config.timerCount, config.baseTick, config.maxDelay, config.slots)
	fmt.Println("Note: callbacks are no-op counters; setup and forced GC are excluded from measured operation time.")
	fmt.Printf("staleRounds=%d staleRatio=%.2f\n", *staleRounds, *staleRatio)
	fmt.Println()

	results := make([]benchmarkResult, 0, 16)
	for _, slotType := range []slotTypeConfig{
		{name: "slice-generation", kind: timerwheel.SlotTypeSlice},
		{name: "linked-list", kind: timerwheel.SlotTypeLinkedList},
	} {
		results = append(results,
			benchmarkSchedule(config, slotType),
			benchmarkReset(config, slotType),
			benchmarkResetApply(config, slotType),
			benchmarkCancel(config, slotType),
			benchmarkStaleTakeAll(config, slotType, *staleRounds, *staleRatio),
			benchmarkMixedLifecycle(config, slotType),
			benchmarkAdvance(config, slotType),
			benchmarkSameBucket(config, slotType),
			benchmarkLongCascade(config, slotType),
			benchmarkAdvanceTickLatency(config, slotType),
		)
	}

	printResults(results)
}

type slotTypeConfig struct {
	name string
	kind timerwheel.SlotType
}

func newWheel(config benchmarkConfig, slotType slotTypeConfig, commandCapacity int) *timerwheel.Wheel {
	w, err := timerwheel.NewWheel(timerwheel.WheelConfig{
		BaseTick:        config.baseTick,
		MaxDelay:        config.maxDelay,
		SlotsPerLevel:   config.slots,
		SlotType:        slotType.kind,
		StartTime:       time.Unix(0, 0),
		CommandCapacity: commandCapacity,
	})
	if err != nil {
		panic(err)
	}
	return w
}

func delayForIndex(config benchmarkConfig, index int) time.Duration {
	maxTicks := int(config.maxDelay/config.baseTick) - 1
	if maxTicks < 1 {
		maxTicks = 1
	}
	return time.Duration(index%maxTicks+1) * config.baseTick
}

func measure(scenario string, slot slotTypeConfig, operations int, fn func() int32) benchmarkResult {
	runtime.GC()

	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	start := time.Now()
	callbacks := fn()
	elapsed := time.Since(start)

	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	var allocs, bytes uint64
	if after.Mallocs >= before.Mallocs {
		allocs = after.Mallocs - before.Mallocs
	}
	if after.TotalAlloc >= before.TotalAlloc {
		bytes = after.TotalAlloc - before.TotalAlloc
	}

	return benchmarkResult{
		scenario:   scenario,
		slot:       slot.name,
		operations: operations,
		elapsed:    elapsed,
		allocs:     allocs,
		bytes:      bytes,
		callbacks:  callbacks,
		p50:        float64(elapsed.Nanoseconds()) / float64(operations),
		p95:        float64(elapsed.Nanoseconds()) / float64(operations),
		p99:        float64(elapsed.Nanoseconds()) / float64(operations),
	}
}

func benchmarkSchedule(config benchmarkConfig, slot slotTypeConfig) benchmarkResult {
	w := newWheel(config, slot, config.timerCount+1)
	defer w.Close()
	start := time.Unix(0, 0)

	return measure("schedule-apply", slot, config.timerCount, func() int32 {
		for i := 0; i < config.timerCount; i++ {
			if _, err := w.Schedule(delayForIndex(config, i), func(time.Time) {}); err != nil {
				panic(err)
			}
		}
		// Schedule only enqueues commands. Advance applies them and places
		// timers into the selected level/slot, which is part of scheduling
		// cost for comparing the underlying slot implementations.
		w.Advance(start)
		return 0
	})
}

func benchmarkReset(config benchmarkConfig, slot slotTypeConfig) benchmarkResult {
	w := newWheel(config, slot, config.timerCount*2+1)
	defer w.Close()

	handles := make([]timerwheel.Handle, config.timerCount)
	start := time.Unix(0, 0)
	for i := range handles {
		var err error
		handles[i], err = w.Schedule(config.maxDelay/2, func(time.Time) {})
		if err != nil {
			panic(err)
		}
	}
	w.Advance(start)

	return measure("reset", slot, config.timerCount, func() int32 {
		for i, handle := range handles {
			delay := delayForIndex(config, i)
			if !w.Reset(handle, delay) {
				panic("Reset failed")
			}
		}
		return 0
	})
}

func benchmarkResetApply(config benchmarkConfig, slot slotTypeConfig) benchmarkResult {
	w := newWheel(config, slot, config.timerCount*2+1)
	defer w.Close()

	handles := make([]timerwheel.Handle, config.timerCount)
	start := time.Unix(0, 0)
	for i := range handles {
		var err error
		handles[i], err = w.Schedule(config.maxDelay/2, func(time.Time) {})
		if err != nil {
			panic(err)
		}
	}
	w.Advance(start)

	for i, handle := range handles {
		delay := delayForIndex(config, i)
		if !w.Reset(handle, delay) {
			panic("Reset failed")
		}
	}

	return measure("reset-apply", slot, config.timerCount, func() int32 {
		w.Advance(start.Add(config.baseTick))
		return 0
	})
}

func benchmarkCancel(config benchmarkConfig, slot slotTypeConfig) benchmarkResult {
	w := newWheel(config, slot, config.timerCount+1)
	defer w.Close()

	handles := make([]timerwheel.Handle, config.timerCount)
	start := time.Unix(0, 0)
	for i := range handles {
		var err error
		handles[i], err = w.Schedule(config.maxDelay/2, func(time.Time) {})
		if err != nil {
			panic(err)
		}
	}
	w.Advance(start)

	return measure("cancel", slot, config.timerCount, func() int32 {
		var canceled int32
		for _, handle := range handles {
			if w.Cancel(handle) {
				canceled++
			}
		}
		return canceled
	})
}

func benchmarkStaleTakeAll(config benchmarkConfig, slot slotTypeConfig, rounds int, ratio float64) benchmarkResult {
	w := newWheel(config, slot, config.timerCount*(rounds+2)+1)
	defer w.Close()

	start := time.Unix(0, 0)
	delay := 50 * config.baseTick
	handles := make([]timerwheel.Handle, config.timerCount)
	for i := range handles {
		var err error
		handles[i], err = w.Schedule(delay, func(time.Time) {})
		if err != nil {
			panic(err)
		}
	}
	w.Advance(start)

	resetCount := int(float64(config.timerCount) * ratio)
	for round := 0; round < rounds; round++ {
		for i := 0; i < resetCount; i++ {
			if !w.Reset(handles[i], delay) {
				panic("Reset failed")
			}
		}
		w.Advance(start)
	}

	return measure("stale-take-all", slot, config.timerCount, func() int32 {
		w.Advance(start.Add(delay))
		return 0
	})
}

func benchmarkMixedLifecycle(config benchmarkConfig, slot slotTypeConfig) benchmarkResult {
	w := newWheel(config, slot, config.timerCount*2+1)
	defer w.Close()

	start := time.Unix(0, 0)
	handles := make([]timerwheel.Handle, config.timerCount)
	for i := range handles {
		var err error
		handles[i], err = w.Schedule(config.maxDelay/2, func(time.Time) {})
		if err != nil {
			panic(err)
		}
	}
	w.Advance(start)

	return measure("mixed-lifecycle", slot, config.timerCount+1, func() int32 {
		for i, handle := range handles {
			if i%5 == 0 {
				if !w.Cancel(handle) {
					panic("Cancel failed")
				}
				continue
			}
			if !w.Reset(handle, delayForIndex(config, i)) {
				panic("Reset failed")
			}
		}
		w.Advance(start.Add(config.baseTick))
		return 0
	})
}

func benchmarkAdvance(config benchmarkConfig, slot slotTypeConfig) benchmarkResult {
	w := newWheel(config, slot, config.timerCount+1)
	defer w.Close()

	start := time.Unix(0, 0)
	var callbacks atomic.Int32
	for i := 0; i < config.timerCount; i++ {
		delay := delayForIndex(config, i)
		if _, err := w.Schedule(delay, func(time.Time) {
			callbacks.Add(1)
		}); err != nil {
			panic(err)
		}
	}
	w.Advance(start)

	return measure("advance-distributed", slot, config.timerCount, func() int32 {
		w.Advance(start.Add(config.maxDelay - config.baseTick))
		return callbacks.Load()
	})
}

func benchmarkSameBucket(config benchmarkConfig, slot slotTypeConfig) benchmarkResult {
	w := newWheel(config, slot, config.timerCount+1)
	defer w.Close()

	start := time.Unix(0, 0)
	var callbacks atomic.Int32
	delay := 3 * config.baseTick
	for i := 0; i < config.timerCount; i++ {
		if _, err := w.Schedule(delay, func(time.Time) {
			callbacks.Add(1)
		}); err != nil {
			panic(err)
		}
	}
	w.Advance(start)

	return measure("advance-same-bucket", slot, config.timerCount, func() int32 {
		w.Advance(start.Add(delay))
		return callbacks.Load()
	})
}

func benchmarkLongCascade(config benchmarkConfig, slot slotTypeConfig) benchmarkResult {
	longDelay := config.maxDelay - config.baseTick
	if longDelay <= config.baseTick {
		longDelay = 2 * config.baseTick
	}

	w := newWheel(config, slot, config.timerCount+1)
	defer w.Close()

	start := time.Unix(0, 0)
	var callbacks atomic.Int32
	for i := 0; i < config.timerCount; i++ {
		if _, err := w.Schedule(longDelay, func(time.Time) {
			callbacks.Add(1)
		}); err != nil {
			panic(err)
		}
	}
	w.Advance(start)

	return measure("advance-long-cascade", slot, config.timerCount, func() int32 {
		w.Advance(start.Add(longDelay))
		return callbacks.Load()
	})
}

func benchmarkAdvanceTickLatency(config benchmarkConfig, slot slotTypeConfig) benchmarkResult {
	w := newWheel(config, slot, config.timerCount+1)
	defer w.Close()

	start := time.Unix(0, 0)
	maxTicks := int(config.maxDelay/config.baseTick) - 1
	if maxTicks <= 0 {
		maxTicks = 1
	}
	for i := 0; i < config.timerCount; i++ {
		delay := time.Duration(i%maxTicks+1) * config.baseTick
		if _, err := w.Schedule(delay, func(time.Time) {}); err != nil {
			panic(err)
		}
	}
	w.Advance(start)

	samples := make([]int64, 0, maxTicks)
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	for tick := 1; tick <= maxTicks; tick++ {
		begin := time.Now()
		w.Advance(start.Add(time.Duration(tick) * config.baseTick))
		samples = append(samples, time.Since(begin).Nanoseconds())
	}
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	var allocs, bytes uint64
	if after.Mallocs >= before.Mallocs {
		allocs = after.Mallocs - before.Mallocs
	}
	if after.TotalAlloc >= before.TotalAlloc {
		bytes = after.TotalAlloc - before.TotalAlloc
	}

	return benchmarkResult{
		scenario:     "advance-tick-latency",
		slot:         slot.name,
		operations:   maxTicks,
		elapsed:      time.Duration(sumInt64(samples)),
		allocs:       allocs,
		bytes:        bytes,
		p50:          percentile(samples, 0.50),
		p95:          percentile(samples, 0.95),
		p99:          percentile(samples, 0.99),
		hasQuantiles: true,
	}
}

func sumInt64(values []int64) int64 {
	var total int64
	for _, value := range values {
		total += value
	}
	return total
}

func percentile(values []int64, ratio float64) float64 {
	if len(values) == 0 {
		return 0
	}
	index := int(float64(len(values)-1) * ratio)
	return float64(values[index])
}

func printResults(results []benchmarkResult) {
	fmt.Printf("%-24s %-18s %10s %14s %12s %12s %12s %12s %12s %12s %10s\n",
		"scenario", "slot", "operations", "total", "ns/op", "allocs/op", "B/op", "p50(ns)", "p95(ns)", "p99(ns)", "callbacks")
	fmt.Println("------------------------------------------------------------------------------------------------------------------------------------------------")

	for _, result := range results {
		nsPerOp := float64(result.elapsed.Nanoseconds()) / float64(result.operations)
		allocsPerOp := float64(result.allocs) / float64(result.operations)
		bytesPerOp := float64(result.bytes) / float64(result.operations)
		p50, p95, p99 := "-", "-", "-"
		if result.hasQuantiles {
			p50 = fmt.Sprintf("%.2f", result.p50)
			p95 = fmt.Sprintf("%.2f", result.p95)
			p99 = fmt.Sprintf("%.2f", result.p99)
		}

		fmt.Printf("%-24s %-18s %10d %14s %12.2f %12.2f %12.2f %12s %12s %12s %10d\n",
			result.scenario,
			result.slot,
			result.operations,
			result.elapsed,
			nsPerOp,
			allocsPerOp,
			bytesPerOp,
			p50,
			p95,
			p99,
			result.callbacks,
		)
	}
}
