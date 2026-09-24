package devicecatalog

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync/atomic"

	"github.com/Alia5/VIIPER/device"
	"github.com/Alia5/VIIPER/device/xboxone"
	"github.com/Alia5/VIIPER/internal/server/api"
	"github.com/Alia5/VIIPER/usb"
)

type xboxOneHIDHandler struct{ profile string }

var xboxOneHIDSerialCounter atomic.Uint64

func registerXboxOneHIDDevices() {
	api.RegisterDevice("xboxonehid", &xboxOneHIDHandler{profile: "xboxone"})
	api.RegisterDevice("xboxserieshid", &xboxOneHIDHandler{profile: "xboxseries"})
}

func (h *xboxOneHIDHandler) CreateDevice(o *device.CreateOptions) (usb.Device, error) {
	if o == nil {
		o = &device.CreateOptions{}
	}
	if o.DeviceSpecific == "" {
		serial := fmt.Sprintf("VIIPER-XBOX-HID-%04X", xboxOneHIDSerialCounter.Add(1))
		o.DeviceSpecific = fmt.Sprintf(`{"serial_number":%q}`, serial)
	}
	return xboxone.NewHID(o, h.profile)
}

func (h *xboxOneHIDHandler) StreamHandler() api.StreamHandlerFunc {
	return func(conn net.Conn, devPtr *usb.Device, logger *slog.Logger) error {
		if devPtr == nil || *devPtr == nil {
			return fmt.Errorf("nil device")
		}
		dev, ok := (*devPtr).(*xboxone.XboxOneHID)
		if !ok {
			return fmt.Errorf("device is not xboxone HID")
		}
		buffer := make([]byte, xboxone.HIDInputWireSize)
		for {
			if _, err := io.ReadFull(conn, buffer); err != nil {
				if err == io.EOF {
					logger.Info("Xbox HID client disconnected")
					return nil
				}
				return fmt.Errorf("read Xbox HID input state: %w", err)
			}
			var state xboxone.HIDInputState
			if err := state.UnmarshalBinary(buffer); err != nil {
				return fmt.Errorf("decode Xbox HID input state: %w", err)
			}
			dev.UpdateInputState(state)
		}
	}
}

func (h *xboxOneHIDHandler) UpdateMetaState(_ string, _ *usb.Device) error {
	return nil
}
