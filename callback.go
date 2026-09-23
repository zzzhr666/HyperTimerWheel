package timerwheel

import (
	"log"
	"runtime/debug"
	"time"
)

// CallbackPanicInfo describes a panic raised by a timer callback.
type CallbackPanicInfo struct {
	ID         ID
	ExecuteAt  time.Time
	PanicValue any
	Stack      []byte
}

// CallbackPanicHandler receives recovered callback panics.
type CallbackPanicHandler func(CallbackPanicInfo)

func defaultCallbackPanicHandler(info CallbackPanicInfo) {
	log.Printf(
		"timer callback panic:\ntimerID=%d\n executeAt=%v\n panic=%v\n%s",
		info.ID,
		info.ExecuteAt,
		info.PanicValue,
		info.Stack,
	)
}

func (w *Wheel) executeCallback(t *timer, now time.Time) {
	defer func() {
		if value := recover(); value != nil {
			handler := w.callbackPanicHandler
			if handler == nil {
				handler = defaultCallbackPanicHandler
			}
			handler(CallbackPanicInfo{
				ID:         ID(t.ID),
				ExecuteAt:  now,
				PanicValue: value,
				Stack:      debug.Stack(),
			})
		}
	}()

	t.Callback(now)
}
