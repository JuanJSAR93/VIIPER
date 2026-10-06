// Package xboxseries provides a VIIPER USB/IP Xbox Series X|S gamepad persona.
package xboxseries

const (
	DefaultVID uint16 = 0x045e
	DefaultPID uint16 = 0x0b12

	Manufacturer = "©Microsoft Corporation"
	Product      = "Xbox Series X|S Controller"

	EndpointIn  uint8 = 0x81
	EndpointOut uint8 = 0x01

	InputWireSize   = 14
	InputReportSize = 14
)

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

// RumbleState is the normalized feedback exposed by the VIIPER stream.
type RumbleState struct {
	LeftMotor  uint8
	RightMotor uint8
}
