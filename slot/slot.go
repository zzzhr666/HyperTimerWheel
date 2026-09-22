package slot

import "HyperTimerWheel/timer"

type Type uint8

const (
	TypeSlice Type = iota
	TypeLinkedList
)

type Slot interface {
	Add(t *timer.Timer)
	Invalidate(t *timer.Timer) bool
	TakeAll() []*timer.Timer
}

func New(kind Type) Slot {
	switch kind {
	case TypeSlice:
		return &sliceSlot{}
	default:
		return nil
	}
}
