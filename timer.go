package timerwheel

import (
	"sync/atomic"
	"time"
)

type Callback func(now time.Time)

type timerID uint64

const InvalidTimerID timerID = 0

type timer struct {
	id       timerID
	cb       Callback
	deadline time.Time
	state    atomic.Uint32
}

const (
	timerActive uint32 = iota
	timerCanceled
	timerFired
)

type Handle struct {
	id timerID
	t  *timer
}
