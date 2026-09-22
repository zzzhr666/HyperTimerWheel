package timerwheel

import (
	"errors"
	"sync/atomic"
	"time"
)

var (
	ErrInvalidParam    = errors.New("invalid param")
	ErrUnsupportedSlot = errors.New("unsupported slot implementation")
)

const DefaultCommandsCapacity = 65536

type SlotType uint8

const (
	SlotTypeSlice SlotType = iota
	// SlotTypeLinkedList is reserved for the linked-list implementation.
	// It is intentionally not implemented yet.
	SlotTypeLinkedList
)

type Wheel struct {
	baseTick     time.Duration
	slotPerLevel int
	levels       []*level
	lastTime     time.Time

	nextTimerID atomic.Uint64
	timers      map[timerID]*timer
	locations   map[timerID]slot
	maxDelay    time.Duration

	commands   chan command
	workerPool *WorkerPool
}

type WheelConfig struct {
	BaseTick         time.Duration
	MaxDelay         time.Duration
	SlotsPerLevel    int
	SlotType         SlotType
	StartTime        time.Time
	WorkerPoolConfig WorkerPoolConfig
}

func NewWheel(config WheelConfig) (*Wheel, error) {
	if config.BaseTick <= 0 || config.MaxDelay <= 0 || config.SlotsPerLevel <= 0 {
		return nil, ErrInvalidParam
	}
	if config.SlotType != SlotTypeSlice {
		return nil, ErrUnsupportedSlot
	}

	startTime := config.StartTime
	if startTime.IsZero() {
		startTime = time.Now()
	}

	w := &Wheel{
		baseTick:     config.BaseTick,
		slotPerLevel: config.SlotsPerLevel,
		lastTime:     startTime,
		timers:       make(map[timerID]*timer),
		locations:    make(map[timerID]slot),
		maxDelay:     config.MaxDelay,
		commands:     make(chan command, DefaultCommandsCapacity),
		workerPool:   NewWorkerPool(config.WorkerPoolConfig),
	}
	w.nextTimerID.Store(1)

	levels := 1
	span := config.BaseTick * time.Duration(config.SlotsPerLevel)
	for span < config.MaxDelay {
		span *= time.Duration(config.SlotsPerLevel)
		levels++
	}

	w.levels = make([]*level, levels)
	tick := config.BaseTick
	for i := 0; i < levels; i++ {
		w.levels[i] = newLevel(tick, config.SlotsPerLevel, i == 0, func() slot {
			return newSlot(config.SlotType)
		})
		tick *= time.Duration(config.SlotsPerLevel)
	}

	return w, nil
}

func (w *Wheel) Schedule(delay time.Duration, callback Callback) (Handle, error) {
	if delay <= 0 || callback == nil || delay >= w.maxDelay {
		return Handle{id: InvalidTimerID}, ErrInvalidParam
	}

	id := w.nextTimerID.Add(1)
	t := &timer{
		id: timerID(id),
		cb: callback,
	}

	w.commands <- command{
		kind:  CmdSchedule,
		t:     t,
		delay: delay,
	}

	return Handle{id: t.id, t: t}, nil
}

func (w *Wheel) Cancel(h Handle) bool {
	if h.t == nil {
		return false
	}

	return h.t.state.CompareAndSwap(timerActive, timerCanceled)
}

func (w *Wheel) Advance(now time.Time) int {
	w.drainCommands()

	if !now.After(w.lastTime) {
		return 0
	}

	elapsed := int(now.Sub(w.lastTime) / w.baseTick)
	count := 0
	for i := 0; i < elapsed; i++ {
		w.lastTime = w.lastTime.Add(w.baseTick)
		w.advanceLevel(0)
		count += w.fireLowestLevelCurrentSlot()
	}

	return count
}

func (w *Wheel) Reset(h Handle, newDelay time.Duration) bool {
	if h.t == nil {
		return false
	}
	if newDelay <= 0 || newDelay >= w.maxDelay {
		return false
	}
	if h.t.state.Load() != timerActive {
		return false
	}

	w.commands <- command{
		kind:  CmdReset,
		t:     h.t,
		delay: newDelay,
	}
	return true
}

func (w *Wheel) fireLowestLevelCurrentSlot() int {
	pendingTasks := w.levels[0].takeCurrent()
	count := 0
	now := w.lastTime

	for _, t := range pendingTasks {
		if !t.state.CompareAndSwap(timerActive, timerFired) {
			delete(w.timers, t.id)
			delete(w.locations, t.id)
			continue
		}

		delete(w.timers, t.id)
		delete(w.locations, t.id)

		task := func() {
			t.cb(now)
		}
		if w.workerPool == nil || !w.workerPool.Submit(task) {
			task()
		}

		count++
	}

	return count
}

func (w *Wheel) advanceLevel(k int) {
	l := w.levels[k]
	wrapped := l.advance()
	if wrapped && k+1 < len(w.levels) {
		w.advanceLevel(k + 1)
	}
	if k > 0 {
		w.cascade(k)
	}
}

func (w *Wheel) cascade(k int) {
	l := w.levels[k]
	pendingTasks := l.takeCurrent()

	for _, t := range pendingTasks {
		if t.state.Load() != timerActive {
			delete(w.timers, t.id)
			delete(w.locations, t.id)
			continue
		}

		remaining := t.deadline.Sub(w.lastTime)
		if remaining <= 0 {
			w.locations[t.id] = w.levels[0].addCurrent(t)
		} else {
			w.addTimerByRemaining(t, remaining)
		}
	}
}

func (w *Wheel) addTimerByRemaining(t *timer, remaining time.Duration) bool {
	for _, l := range w.levels {
		if target, ok := l.addAfter(t, remaining); ok {
			w.locations[t.id] = target
			return true
		}
	}
	return false
}

func (w *Wheel) applyScheduleCommand(cmd command) {
	t := cmd.t
	if t == nil {
		return
	}
	if t.state.Load() != timerActive {
		return
	}

	if _, exists := w.timers[t.id]; exists {
		return
	}

	t.deadline = w.lastTime.Add(cmd.delay)
	if !w.addTimerByRemaining(t, cmd.delay) {
		return
	}
	w.timers[t.id] = t
}

func (w *Wheel) applyResetCommand(cmd command) {
	t := cmd.t
	if t == nil {
		return
	}
	if t.state.Load() != timerActive {
		return
	}

	if loc, ok := w.locations[t.id]; ok {
		loc.invalidate(t)
	}

	t.deadline = w.lastTime.Add(cmd.delay)
	if !w.addTimerByRemaining(t, cmd.delay) {
		return
	}
	w.timers[t.id] = t
}

func (w *Wheel) drainCommands() {
	for {
		select {
		case cmd := <-w.commands:
			switch cmd.kind {
			case CmdSchedule:
				w.applyScheduleCommand(cmd)
			case CmdReset:
				w.applyResetCommand(cmd)
			}
		default:
			return
		}
	}
}
