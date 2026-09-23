package timerwheel

import (
	"errors"
	"sync/atomic"
	"time"
)

var (
	ErrInvalidParam     = errors.New("invalid param")
	ErrUnsupportedSlot  = errors.New("unsupported slot implementation")
	ErrClosed           = errors.New("wheel is closed")
	ErrCommandQueueFull = errors.New("command queue is full")
)

const DefaultCommandsCapacity = 65536

type WheelState = uint32

const (
	WheelStateRunning WheelState = iota
	WheelStateStopped
)

type Wheel struct {
	baseTick     time.Duration
	slotPerLevel int
	levels       []*level
	lastTime     time.Time
	state        atomic.Uint32

	nextTimerID atomic.Uint64
	timers      map[timerID]*timer
	locations   map[timerID]slot
	maxDelay    time.Duration

	commands             chan command
	workerPool           *WorkerPool
	callbackPanicHandler CallbackPanicHandler
}

type WheelConfig struct {
	BaseTick             time.Duration
	MaxDelay             time.Duration
	SlotsPerLevel        int
	SlotType             SlotType
	StartTime            time.Time
	CommandCapacity      int
	WorkerPoolConfig     WorkerPoolConfig
	CallbackPanicHandler CallbackPanicHandler
}

func NewWheel(config WheelConfig) (*Wheel, error) {
	if config.BaseTick <= 0 || config.MaxDelay <= 0 || config.SlotsPerLevel <= 0 {
		return nil, ErrInvalidParam
	}
	if !validateSlotType(config.SlotType) {
		return nil, ErrUnsupportedSlot
	}
	commandCapacity := config.CommandCapacity
	if commandCapacity <= 0 {
		commandCapacity = DefaultCommandsCapacity
	}

	startTime := config.StartTime
	if startTime.IsZero() {
		startTime = time.Now()
	}

	w := &Wheel{
		baseTick:             config.BaseTick,
		slotPerLevel:         config.SlotsPerLevel,
		lastTime:             startTime,
		timers:               make(map[timerID]*timer),
		locations:            make(map[timerID]slot),
		maxDelay:             config.MaxDelay,
		commands:             make(chan command, commandCapacity),
		workerPool:           NewWorkerPool(config.WorkerPoolConfig),
		callbackPanicHandler: config.CallbackPanicHandler,
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
	if w.state.Load() != WheelStateRunning {
		return Handle{id: InvalidTimerID}, ErrClosed
	}

	if delay <= 0 || callback == nil || delay >= w.maxDelay {
		return Handle{id: InvalidTimerID}, ErrInvalidParam
	}

	id := w.nextTimerID.Add(1)
	t := &timer{
		ID:       timerID(id),
		Callback: callback,
	}

	select {
	case w.commands <- command{
		kind:  CmdSchedule,
		t:     t,
		delay: delay,
	}:
		return Handle{id: t.ID, t: t}, nil
	default:
		return Handle{id: InvalidTimerID}, ErrCommandQueueFull
	}
}

func (w *Wheel) Cancel(h Handle) bool {
	if w.state.Load() != WheelStateRunning {
		return false
	}
	if h.t == nil {
		return false
	}

	return h.t.State.CompareAndSwap(timerActive, timerCanceled)
}

func (w *Wheel) Advance(now time.Time) int {
	if w.state.Load() != WheelStateRunning {
		return 0
	}
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
	if w.state.Load() != WheelStateRunning {
		return false
	}
	if h.t == nil {
		return false
	}
	if newDelay <= 0 || newDelay >= w.maxDelay {
		return false
	}
	if h.t.State.Load() != timerActive {
		return false
	}

	select {
	case w.commands <- command{
		kind:  CmdReset,
		t:     h.t,
		delay: newDelay,
	}:
		return true
	default:
		return false
	}
}

func (w *Wheel) Close() {
	if !w.state.CompareAndSwap(WheelStateRunning, WheelStateStopped) {
		return
	}

	if w.workerPool != nil {
		w.workerPool.Close()
	}
}

func validateSlotType(slotType SlotType) bool {
	return slotType == SlotTypeSlice || slotType == SlotTypeLinkedList
}

func (w *Wheel) fireLowestLevelCurrentSlot() int {
	pendingTasks := w.levels[0].takeCurrent()
	count := 0
	now := w.lastTime

	for _, t := range pendingTasks {
		if !t.State.CompareAndSwap(timerActive, timerFired) {
			delete(w.timers, t.ID)
			delete(w.locations, t.ID)
			continue
		}

		delete(w.timers, t.ID)
		delete(w.locations, t.ID)

		task := func() {
			w.executeCallback(t, now)
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
		if t.State.Load() != timerActive {
			delete(w.timers, t.ID)
			delete(w.locations, t.ID)
			continue
		}

		remaining := t.Deadline.Sub(w.lastTime)
		if remaining <= 0 {
			w.locations[t.ID] = w.levels[0].addCurrent(t)
		} else {
			w.addTimerByRemaining(t, remaining)
		}
	}
}

func (w *Wheel) addTimerByRemaining(t *timer, remaining time.Duration) bool {
	for _, l := range w.levels {
		if target, ok := l.addAfter(t, remaining); ok {
			w.locations[t.ID] = target
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
	if t.State.Load() != timerActive {
		return
	}

	if _, exists := w.timers[t.ID]; exists {
		return
	}

	t.Deadline = w.lastTime.Add(cmd.delay)
	if !w.addTimerByRemaining(t, cmd.delay) {
		return
	}
	w.timers[t.ID] = t
}

func (w *Wheel) applyResetCommand(cmd command) {
	t := cmd.t
	if t == nil {
		return
	}
	if t.State.Load() != timerActive {
		return
	}

	if loc, ok := w.locations[t.ID]; ok {
		loc.Invalidate(t)
	}

	t.Deadline = w.lastTime.Add(cmd.delay)
	if !w.addTimerByRemaining(t, cmd.delay) {
		return
	}
	w.timers[t.ID] = t
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
