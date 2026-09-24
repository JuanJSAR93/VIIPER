package xboxonehid

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync/atomic"

	"github.com/Alia5/VIIPER/device"
	"github.com/Alia5/VIIPER/internal/server/api"
	"github.com/Alia5/VIIPER/usb"
)

func init() {
	api.RegisterDevice("xboxonehid", &handler{profile: profileXboxOne})
	api.RegisterDevice("xboxserieshid", &handler{profile: profileXboxSeries})
}

type handler struct{ profile string }

var serialCounter atomic.Uint64

func (h *handler) CreateDevice(o *device.CreateOptions) (usb.Device, error) {
	if o == nil {
		o = &device.CreateOptions{}
	}
	if o.DeviceSpecific == "" {
		serial := fmt.Sprintf("VIIPER-XBOX-HID-%04X", serialCounter.Add(1))
		payload := fmt.Sprintf(`{"serial_number":%q}`, serial)
		o.DeviceSpecific = payload
	}
	return New(o, h.profile)
}

func (h *handler) StreamHandler() api.StreamHandlerFunc {
	return func(conn net.Conn, devPtr *usb.Device, logger *slog.Logger) error {
		if devPtr == nil || *devPtr == nil {
			return fmt.Errorf("nil device")
		}
		dev, ok := (*devPtr).(*XboxOneHID)
		if !ok {
			return fmt.Errorf("device is not xboxonehid")
		}
		buffer := make([]byte, InputWireSize)
		for {
			if _, err := io.ReadFull(conn, buffer); err != nil {
				if err == io.EOF {
					logger.Info("Xbox HID client disconnected")
					return nil
				}
				return fmt.Errorf("read Xbox HID input state: %w", err)
			}
			var state InputState
			if err := state.UnmarshalBinary(buffer); err != nil {
				return fmt.Errorf("decode Xbox HID input state: %w", err)
			}
			dev.UpdateInputState(state)
		}
	}
}

func (h *handler) UpdateMetaState(_ string, _ *usb.Device) error { return nil }
