package timerwheel

import (
	"time"

	slotpkg "HyperTimerWheel/slot"
)

type slot = slotpkg.Slot

type level struct {
	tick             time.Duration
	slots            []slot
	currentIndex     int // index: 0 - len(slots)-1
	roundUpRemainder bool
}

func newLevel(tick time.Duration, slotCount int, roundUp bool, newSlot func() slot) *level {
	l := &level{
		tick:             tick,
		slots:            make([]slot, slotCount),
		roundUpRemainder: roundUp,
	}
	for i := range l.slots {
		l.slots[i] = newSlot()
	}
	return l
}

// advance moves this level by one tick and reports whether it wrapped around.
func (l *level) advance() bool {
	l.currentIndex++
	if l.currentIndex == len(l.slots) {
		l.currentIndex = 0
		return true
	}
	return false
}

func (l *level) takeCurrent() []*timer {
	return l.slots[l.currentIndex].TakeAll()
}

func (l *level) addCurrent(t *timer) slot {
	current := l.slots[l.currentIndex]
	current.Add(t)
	return current
}

// addAfter places a timer in this level when its remaining duration fits.
// The returned slot is used by Wheel as the timer's location index.
func (l *level) addAfter(t *timer, remaining time.Duration) (slot, bool) {
	ticks := remaining / l.tick
	if l.tick == 0 {
		return nil, false
	}

	// The lowest level rounds a non-integral duration up. Higher levels
	// always advance at least one bucket when they are selected.
	if (l.roundUpRemainder && remaining%l.tick != 0) || ticks == 0 {
		ticks++
	}
	if ticks >= time.Duration(len(l.slots)) {
		return nil, false
	}

	position := (l.currentIndex + int(ticks)) % len(l.slots)
	target := l.slots[position]
	target.Add(t)
	return target, true
}
