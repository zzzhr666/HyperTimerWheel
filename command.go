package timerwheel

import "time"

type command struct {
	t     *timer
	delay time.Duration
}
