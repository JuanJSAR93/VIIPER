package xboxseries

import (
	"encoding/binary"
	"io"
)

// InputState is the transport-neutral state consumed by the Xbox Series
// persona. Sticks are signed HID axes and triggers use the 0..1023 range.
type InputState struct {
	Buttons      uint16
	LeftTrigger  uint16
	RightTrigger uint16
	LeftStickX   int16
	LeftStickY   int16
	RightStickX  int16
	RightStickY  int16
}

// NewInputState returns the neutral state.
func NewInputState() InputState { return InputState{} }

// MarshalBinary encodes the fixed client-to-server state format.
func (s InputState) MarshalBinary() ([]byte, error) {
	b := make([]byte, InputWireSize)
	binary.LittleEndian.PutUint16(b[0:2], s.Buttons)
	binary.LittleEndian.PutUint16(b[2:4], s.LeftTrigger)
	binary.LittleEndian.PutUint16(b[4:6], s.RightTrigger)
	binary.LittleEndian.PutUint16(b[6:8], uint16(s.LeftStickX))
	binary.LittleEndian.PutUint16(b[8:10], uint16(s.LeftStickY))
	binary.LittleEndian.PutUint16(b[10:12], uint16(s.RightStickX))
	binary.LittleEndian.PutUint16(b[12:14], uint16(s.RightStickY))
	return b, nil
}

// UnmarshalBinary decodes the fixed client-to-server state format.
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

// BuildReportInto encodes the HID input report without a report ID.
func (s InputState) BuildReportInto(destination []byte) int {
	if len(destination) < InputReportSize {
		return 0
	}
	clear(destination[:InputReportSize])
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
