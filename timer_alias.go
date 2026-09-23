package timerwheel

import timermodel "HyperTimerWheel/timer"

type Callback = timermodel.Callback
type ID = timermodel.ID
type timerID = timermodel.ID
type timer = timermodel.Timer

const InvalidTimerID = timermodel.InvalidID

const (
	timerActive   = timermodel.Active
	timerCanceled = timermodel.Canceled
	timerFired    = timermodel.Fired
)

type Handle struct {
	id timerID
	t  *timer
}
