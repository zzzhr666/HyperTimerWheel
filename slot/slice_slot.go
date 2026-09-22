package slot

import "HyperTimerWheel/timer"

type sliceEntry struct {
	timer *timer.Timer
	gen   uint64
}

type sliceSlot struct {
	entries     []sliceEntry
	generations map[*timer.Timer]uint64
}

func (s *sliceSlot) Add(t *timer.Timer) {
	if s.generations == nil {
		s.generations = make(map[*timer.Timer]uint64)
	}

	gen := s.generations[t]
	s.generations[t] = gen
	s.entries = append(s.entries, sliceEntry{
		timer: t,
		gen:   gen,
	})
}

func (s *sliceSlot) Invalidate(t *timer.Timer) bool {
	if s.generations == nil {
		return false
	}

	if _, ok := s.generations[t]; !ok {
		return false
	}

	s.generations[t]++
	return true
}

func (s *sliceSlot) TakeAll() []*timer.Timer {
	if len(s.entries) == 0 {
		return nil
	}

	out := make([]*timer.Timer, 0, len(s.entries))
	for _, entry := range s.entries {
		if entry.gen != s.generations[entry.timer] {
			continue
		}
		out = append(out, entry.timer)
	}

	clear(s.entries)
	s.entries = s.entries[:0]
	clear(s.generations)
	return out
}
