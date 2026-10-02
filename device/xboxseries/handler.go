package xboxseries

import (
	"fmt"
	"io"
	"log/slog"
	"net"

	"github.com/Alia5/VIIPER/device"
	"github.com/Alia5/VIIPER/internal/server/api"
	"github.com/Alia5/VIIPER/usb"
)

func init() { api.RegisterDevice("xboxseries", &handler{}) }

type handler struct{}

func (h *handler) CreateDevice(o *device.CreateOptions) (usb.Device, error) { return New(o) }

func (h *handler) StreamHandler() api.StreamHandlerFunc {
	return func(conn net.Conn, devPtr *usb.Device, logger *slog.Logger) error {
		if devPtr == nil || *devPtr == nil {
			return fmt.Errorf("nil device")
		}
		gamepad, ok := (*devPtr).(*XboxSeries)
		if !ok {
			return fmt.Errorf("%w: expected XboxSeries", device.ErrWrongDeviceType)
		}
		gamepad.SetRumbleCallback(func(state RumbleState) {
			payload := []byte{state.LeftMotor, state.RightMotor}
			if _, err := conn.Write(payload); err != nil {
				logger.Error("send Xbox Series rumble", "error", err)
			}
		})
		defer gamepad.SetRumbleCallback(nil)
		buffer := make([]byte, InputWireSize)
		for {
			if _, err := io.ReadFull(conn, buffer); err != nil {
				if err == io.EOF {
					return nil
				}
				return fmt.Errorf("read Xbox Series input state: %w", err)
			}
			var state InputState
			if err := state.UnmarshalBinary(buffer); err != nil {
				return fmt.Errorf("decode Xbox Series input state: %w", err)
			}
			gamepad.UpdateInputState(state)
		}
	}
}

func (h *handler) UpdateMetaState(_ string, _ *usb.Device) error { return nil }
