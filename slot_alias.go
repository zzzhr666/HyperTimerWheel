package timerwheel

import slotpkg "HyperTimerWheel/slot"

type SlotType = slotpkg.Type

const (
	SlotTypeSlice      = slotpkg.TypeSlice
	SlotTypeLinkedList = slotpkg.TypeLinkedList
)

func newSlot(kind SlotType) slot {
	return slotpkg.New(kind)
}
