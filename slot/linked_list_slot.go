package slot

import (
	"HyperTimerWheel/list"
	"HyperTimerWheel/timer"
)

type linkedListSlot struct {
	timerList *list.List[*timer.Timer]
	nodes     map[*timer.Timer]list.Iterator[*timer.Timer]
}

func newLinkedListSlot() *linkedListSlot {
	return &linkedListSlot{
		timerList: list.NewList[*timer.Timer](),
		nodes:     make(map[*timer.Timer]list.Iterator[*timer.Timer]),
	}
}

func (s *linkedListSlot) Add(t *timer.Timer) {
	it, exists := s.nodes[t]
	if exists {
		s.timerList.Remove(it)
		delete(s.nodes, t)
	}
	iter := s.timerList.PushBack(t)
	s.nodes[t] = iter
}

func (s *linkedListSlot) Invalidate(t *timer.Timer) bool {
	it, exists := s.nodes[t]
	if !exists {
		return false
	}
	s.timerList.Remove(it)
	delete(s.nodes, t)
	return true
}

func (s *linkedListSlot) TakeAll() []*timer.Timer {
	if s.timerList.Empty() {
		return nil
	}
	ret := make([]*timer.Timer, 0, s.timerList.Len())
	for it := s.timerList.Begin(); !it.Equal(s.timerList.End()); it = it.Next() {
		ret = append(ret, it.Value())
	}
	clear(s.nodes)
	s.timerList.Clear()
	return ret
}
