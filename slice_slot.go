package timerwheel

type slot interface {
	add(t *timer)
	invalidate(t *timer) bool
	takeAll() []*timer
}

func newSlot(kind SlotType) slot {
	switch kind {
	case SlotTypeSlice:
		return &sliceSlot{}
	default:
		return nil
	}
}

type sliceEntry struct {
	timer *timer
	gen   uint64
}

type sliceSlot struct {
	entries     []sliceEntry
	generations map[*timer]uint64
}

func (s *sliceSlot) add(t *timer) {
	if s.generations == nil {
		s.generations = make(map[*timer]uint64)
	}

	gen := s.generations[t]
	s.generations[t] = gen

	s.entries = append(s.entries, sliceEntry{
		timer: t,
		gen:   gen,
	})
}

func (s *sliceSlot) invalidate(t *timer) bool {
	if s.generations == nil {
		return false
	}

	if _, ok := s.generations[t]; !ok {
		return false
	}

	s.generations[t]++
	return true
}

func (s *sliceSlot) takeAll() []*timer {
	if len(s.entries) == 0 {
		return nil
	}

	out := make([]*timer, 0, len(s.entries))
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
