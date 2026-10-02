package xboxgip

import (
	"encoding/binary"
	"io"
)

// InputWireSize is the stable VIIPER client-to-device state size shared with
// the existing Xbox controller clients.
const InputWireSize = 14

// InputState is the semantic state accepted by the GIP personas. Buttons use
// the same VIIPER bit assignments as the existing Xbox One/Series HID modes;
// the GIP encoder converts them to the official GIP button map.
type InputState struct {
	Buttons      uint16
	LeftTrigger  uint16
	RightTrigger uint16
	LeftStickX   int16
	LeftStickY   int16
	RightStickX  int16
	RightStickY  int16
}

const (
	ButtonDPadUp uint16 = 1 << iota
	ButtonDPadDown
	ButtonDPadLeft
	ButtonDPadRight
	ButtonMenu
	ButtonView
	ButtonLeftStick
	ButtonRightStick
	ButtonLeftBumper
	ButtonRightBumper
	ButtonGuide
	ButtonA
	ButtonB
	ButtonX
	ButtonY
	ButtonShare
)

func (s *InputState) UnmarshalBinary(data []byte) error {
	if len(data) < InputWireSize {
		return io.ErrUnexpectedEOF
	}
	s.Buttons = binary.LittleEndian.Uint16(data[0:2])
	s.LeftTrigger = clampTrigger(binary.LittleEndian.Uint16(data[2:4]))
	s.RightTrigger = clampTrigger(binary.LittleEndian.Uint16(data[4:6]))
	s.LeftStickX = int16(binary.LittleEndian.Uint16(data[6:8]))
	s.LeftStickY = int16(binary.LittleEndian.Uint16(data[8:10]))
	s.RightStickX = int16(binary.LittleEndian.Uint16(data[10:12]))
	s.RightStickY = int16(binary.LittleEndian.Uint16(data[12:14]))
	return nil
}

func clampTrigger(value uint16) uint16 {
	if value > 1023 {
		return 1023
	}
	return value
}
