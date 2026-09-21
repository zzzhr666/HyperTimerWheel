package timerwheel

import (
	"errors"
	"sync/atomic"
	"time"
)

var (
	ErrInvalidParam = errors.New("invalid param")
)

const DefaultCommandsCapacity = 65536

type Wheel struct {
	baseTick     time.Duration
	slotPerLevel int
	levels       []*level
	lastTime     time.Time

	nextTimerID atomic.Uint64
	timers      map[timerID]*timer
	maxDelay    time.Duration

	commands   chan command
	workerPool *WorkerPool
}

type WheelConfig struct {
	BaseTick         time.Duration
	MaxDelay         time.Duration
	SlotsPerLevel    int
	WorkerPoolConfig WorkerPoolConfig
}

func NewWheel(config WheelConfig) (*Wheel, error) {
	if config.BaseTick <= 0 || config.MaxDelay <= 0 || config.SlotsPerLevel <= 0 {
		return nil, ErrInvalidParam
	}

	w := &Wheel{
		baseTick:     config.BaseTick,
		slotPerLevel: config.SlotsPerLevel,
		timers:       make(map[timerID]*timer),
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
		l := &level{
			tick:  tick,
			slots: make([]slot, config.SlotsPerLevel),
		}
		for j := 0; j < config.SlotsPerLevel; j++ {
			l.slots[j] = &sliceSlot{}
		}
		w.levels[i] = l
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

func (w *Wheel) fireLowestLevelCurrentSlot() int {
	pendingTasks := w.levels[0].takeAll()
	count := 0
	now := w.lastTime

	for _, t := range pendingTasks {
		if !t.state.CompareAndSwap(timerActive, timerFired) {
			delete(w.timers, t.id)
			continue
		}

		delete(w.timers, t.id)

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
	l.currentIndex = (l.currentIndex + 1) % len(l.slots)
	if l.currentIndex == 0 && k+1 < len(w.levels) {
		w.advanceLevel(k + 1)
	}
	if k > 0 {
		w.cascade(k)
	}
}

func (w *Wheel) cascade(k int) {
	l := w.levels[k]
	pendingTasks := l.takeAll()

	for _, t := range pendingTasks {
		if t.state.Load() != timerActive {
			delete(w.timers, t.id)
			continue
		}

		remaining := t.deadline.Sub(w.lastTime)
		if remaining <= 0 {
			latestSlot := w.levels[0].slots[w.levels[0].currentIndex]
			latestSlot.add(t)
		} else {
			w.addTimerByRemaining(t, remaining)
		}
	}
}

func (w *Wheel) addTimerByRemaining(t *timer, remaining time.Duration) {
	for i, l := range w.levels {
		ticks := int(remaining / l.tick)
		if i == 0 {
			if remaining%l.tick != 0 {
				ticks++
			}
		} else if ticks == 0 {
			ticks = 1
		}

		if ticks >= len(l.slots) {
			continue
		}

		pos := (l.currentIndex + ticks) % len(l.slots)
		l.add(pos, t)
		return
	}
}

func (w *Wheel) applyScheduleCommand(cmd command) {
	t := cmd.t
	if t.state.Load() != timerActive {
		return
	}

	t.deadline = w.lastTime.Add(cmd.delay)
	w.addTimerByRemaining(t, cmd.delay)
	w.timers[t.id] = t
}

func (w *Wheel) drainCommands() {
	for {
		select {
		case cmd := <-w.commands:
			w.applyScheduleCommand(cmd)
		default:
			return
		}
	}
}
