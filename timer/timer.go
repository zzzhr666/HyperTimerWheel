package timer

import (
	"sync/atomic"
	"time"
)

type Callback func(now time.Time)

type ID uint64

const InvalidID ID = 0

type Timer struct {
	ID       ID
	Callback Callback
	Deadline time.Time
	State    atomic.Uint32
}

const (
	Active uint32 = iota
	Canceled
	Fired
)
