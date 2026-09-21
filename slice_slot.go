package timerwheel

type slot interface {
	add(t *timer)
	remove(t *timer) bool
	takeAll() []*timer
}

type sliceSlot struct {
	timers []*timer
}

func (s *sliceSlot) add(t *timer) {
	s.timers = append(s.timers, t)
}

func (s *sliceSlot) remove(t *timer) bool {
	for i, item := range s.timers {
		if item != t {
			continue
		}

		s.timers = append(s.timers[:i], s.timers[i+1:]...)
		return true
	}

	return false
}

func (s *sliceSlot) takeAll() []*timer {
	if len(s.timers) == 0 {
		return nil
	}

	out := s.timers
	s.timers = nil
	return out
}
