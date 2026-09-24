// The HID compatibility half of package xboxone provides a DirectInput/
// joy.cpl-compatible Xbox One/Series persona alongside the retained GIP half.
package xboxone

import (
	"encoding/binary"
	"io"
)

// HIDInputWireSize is the fixed client-to-server semantic state size.
const HIDInputWireSize = 14

// HIDInputReportSize is the HID input report size. There is no report ID.
const HIDInputReportSize = 14

// HIDInputState is intentionally independent from the retained GIP Xbox One
// protocol. It is the semantic state consumed by the HID compatibility path.
//
// Buttons use the following bit order:
// dpad-up, dpad-down, dpad-left, dpad-right, menu, view, left-stick,
// right-stick, left-bumper, right-bumper, guide, A, B, X, Y, share.
type HIDInputState struct {
	Buttons      uint16
	LeftTrigger  uint16 // 0..1023
	RightTrigger uint16 // 0..1023
	LeftStickX   int16
	LeftStickY   int16
	RightStickX  int16
	RightStickY  int16
}

// HIDInputStateFromSemantic converts the shared Xbox semantic state to the
// HID/DirectInput wire shape. Guide and Share remain represented in the
// compatibility report even though they are not part of the GIP base payload.
func HIDInputStateFromSemantic(state InputStateV1) HIDInputState {
	var buttons uint16
	set := func(value bool, bit uint) {
		if value {
			buttons |= 1 << bit
		}
	}
	set(state.DPadUp, 0)
	set(state.DPadDown, 1)
	set(state.DPadLeft, 2)
	set(state.DPadRight, 3)
	set(state.Menu, 4)
	set(state.View, 5)
	set(state.LeftStickButton, 6)
	set(state.RightStickButton, 7)
	set(state.LeftBumper, 8)
	set(state.RightBumper, 9)
	set(state.Guide, 10)
	set(state.A, 11)
	set(state.B, 12)
	set(state.X, 13)
	set(state.Y, 14)
	set(state.Share, 15)
	return HIDInputState{
		Buttons:      buttons,
		LeftTrigger:  state.LeftTrigger,
		RightTrigger: state.RightTrigger,
		LeftStickX:   state.LeftStickX,
		LeftStickY:   state.LeftStickY,
		RightStickX:  state.RightStickX,
		RightStickY:  state.RightStickY,
	}
}

// SemanticState converts the HID-compatible state back to the shared Xbox
// semantic model so the same test vectors can exercise both personas.
func (s HIDInputState) SemanticState() InputStateV1 {
	pressed := func(bit uint) bool { return s.Buttons&(1<<bit) != 0 }
	return InputStateV1{
		DPadUp:           pressed(0),
		DPadDown:         pressed(1),
		DPadLeft:         pressed(2),
		DPadRight:        pressed(3),
		Menu:             pressed(4),
		View:             pressed(5),
		LeftStickButton:  pressed(6),
		RightStickButton: pressed(7),
		LeftBumper:       pressed(8),
		RightBumper:      pressed(9),
		Guide:            pressed(10),
		A:                pressed(11),
		B:                pressed(12),
		X:                pressed(13),
		Y:                pressed(14),
		Share:            pressed(15),
		LeftTrigger:      s.LeftTrigger,
		RightTrigger:     s.RightTrigger,
		LeftStickX:       s.LeftStickX,
		LeftStickY:       s.LeftStickY,
		RightStickX:      s.RightStickX,
		RightStickY:      s.RightStickY,
	}
}

// BuildReportInto writes buttons, four signed axes and two unsigned triggers.
func (s *HIDInputState) BuildReportInto(destination []byte) int {
	if s == nil || len(destination) < HIDInputReportSize {
		return 0
	}
	destination = destination[:HIDInputReportSize]
	binary.LittleEndian.PutUint16(destination[0:2], s.Buttons)
	binary.LittleEndian.PutUint16(destination[2:4], uint16(s.LeftStickX))
	binary.LittleEndian.PutUint16(destination[4:6], uint16(s.LeftStickY))
	binary.LittleEndian.PutUint16(destination[6:8], uint16(s.RightStickX))
	binary.LittleEndian.PutUint16(destination[8:10], uint16(s.RightStickY))
	binary.LittleEndian.PutUint16(destination[10:12], clampHIDTrigger(s.LeftTrigger))
	binary.LittleEndian.PutUint16(destination[12:14], clampHIDTrigger(s.RightTrigger))
	return HIDInputReportSize
}

func clampHIDTrigger(value uint16) uint16 {
	if value > 1023 {
		return 1023
	}
	return value
}

// MarshalBinary encodes the fixed stream state.
func (s *HIDInputState) MarshalBinary() ([]byte, error) {
	b := make([]byte, HIDInputWireSize)
	if s == nil {
		return b, nil
	}
	binary.LittleEndian.PutUint16(b[0:2], s.Buttons)
	binary.LittleEndian.PutUint16(b[2:4], s.LeftTrigger)
	binary.LittleEndian.PutUint16(b[4:6], s.RightTrigger)
	binary.LittleEndian.PutUint16(b[6:8], uint16(s.LeftStickX))
	binary.LittleEndian.PutUint16(b[8:10], uint16(s.LeftStickY))
	binary.LittleEndian.PutUint16(b[10:12], uint16(s.RightStickX))
	binary.LittleEndian.PutUint16(b[12:14], uint16(s.RightStickY))
	return b, nil
}

// UnmarshalBinary decodes the fixed stream state.
func (s *HIDInputState) UnmarshalBinary(data []byte) error {
	if len(data) < HIDInputWireSize {
		return io.ErrUnexpectedEOF
	}
	s.Buttons = binary.LittleEndian.Uint16(data[0:2])
	s.LeftTrigger = binary.LittleEndian.Uint16(data[2:4])
	s.RightTrigger = binary.LittleEndian.Uint16(data[4:6])
	s.LeftStickX = int16(binary.LittleEndian.Uint16(data[6:8]))
	s.LeftStickY = int16(binary.LittleEndian.Uint16(data[8:10]))
	s.RightStickX = int16(binary.LittleEndian.Uint16(data[10:12]))
	s.RightStickY = int16(binary.LittleEndian.Uint16(data[12:14]))
	return nil
}
