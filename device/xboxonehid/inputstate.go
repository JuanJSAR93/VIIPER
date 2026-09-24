// Package xboxonehid provides a HID gamepad-compatible Xbox One/Series persona.
package xboxonehid

import (
	"encoding/binary"
	"io"
)

// InputWireSize is the fixed client-to-server semantic state size.
const InputWireSize = 14

// InputReportSize is the HID input report size. There is no report ID.
const InputReportSize = 14

// InputState is intentionally independent from the retained GIP Xbox One
// protocol. It is the semantic state consumed by the HID compatibility path.
//
// Buttons use the following bit order:
// dpad-up, dpad-down, dpad-left, dpad-right, menu, view, left-stick,
// right-stick, left-bumper, right-bumper, guide, A, B, X, Y, share.
type InputState struct {
	Buttons      uint16
	LeftTrigger  uint16 // 0..1023
	RightTrigger uint16 // 0..1023
	LeftStickX   int16
	LeftStickY   int16
	RightStickX  int16
	RightStickY  int16
}

// BuildReportInto writes buttons, four signed axes and two unsigned triggers.
func (s *InputState) BuildReportInto(destination []byte) int {
	if s == nil || len(destination) < InputReportSize {
		return 0
	}
	destination = destination[:InputReportSize]
	binary.LittleEndian.PutUint16(destination[0:2], s.Buttons)
	binary.LittleEndian.PutUint16(destination[2:4], uint16(s.LeftStickX))
	binary.LittleEndian.PutUint16(destination[4:6], uint16(s.LeftStickY))
	binary.LittleEndian.PutUint16(destination[6:8], uint16(s.RightStickX))
	binary.LittleEndian.PutUint16(destination[8:10], uint16(s.RightStickY))
	binary.LittleEndian.PutUint16(destination[10:12], clampTrigger(s.LeftTrigger))
	binary.LittleEndian.PutUint16(destination[12:14], clampTrigger(s.RightTrigger))
	return InputReportSize
}

func clampTrigger(value uint16) uint16 {
	if value > 1023 {
		return 1023
	}
	return value
}

// MarshalBinary encodes the fixed stream state.
func (s *InputState) MarshalBinary() ([]byte, error) {
	b := make([]byte, InputWireSize)
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
func (s *InputState) UnmarshalBinary(data []byte) error {
	if len(data) < InputWireSize {
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
