package timerwheel

import "time"

type commandKind uint8

const (
	CmdSchedule commandKind = iota
	CmdReset
)

type command struct {
	kind  commandKind
	t     *timer
	delay time.Duration
}
