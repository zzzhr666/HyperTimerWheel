package timerwheel

import "time"

type level struct {
	tick         time.Duration
	slots        []slot
	currentIndex int // index: 0 - len(slots)-1
}

func (l *level) add(index int, t *timer) {
	l.slots[index].add(t)
}

func (l *level) takeAll() []*timer {
	return l.slots[l.currentIndex].takeAll()
}
