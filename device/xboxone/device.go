package xboxone

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/Alia5/VIIPER/device"
	"github.com/Alia5/VIIPER/usb"
	"github.com/Alia5/VIIPER/usbip"
)

type createOptions struct {
	SerialNumber string `json:"serial_number"`
}

// XboxOne is a self-contained USB/IP gamepad. It intentionally follows the
// same native-device lifecycle as Xbox 360 and does not depend on a broker.
type XboxOne struct {
	mu         sync.RWMutex
	input      InputState
	inputReady chan struct{}
	descriptor usb.Descriptor
	rumble     func(RumbleState)
}

// New creates an Xbox One device.
func New(o *device.CreateOptions) (*XboxOne, error) {
	args := createOptions{}
	if o != nil && o.DeviceSpecific != "" {
		if err := json.Unmarshal([]byte(o.DeviceSpecific), &args); err != nil {
			return nil, fmt.Errorf("invalid device specific JSON: %w", err)
		}
	}
	desc := MakeDescriptor()
	if args.SerialNumber != "" {
		desc.Strings[3] = args.SerialNumber
	}
	if o != nil {
		if o.IDVendor != nil {
			desc.Device.IDVendor = *o.IDVendor
		}
		if o.IDProduct != nil {
			desc.Device.IDProduct = *o.IDProduct
		}
	}
	d := &XboxOne{descriptor: desc, inputReady: make(chan struct{}, 1)}
	d.inputReady <- struct{}{}
	return d, nil
}

// UpdateInputState publishes the newest semantic state.
func (d *XboxOne) UpdateInputState(state InputState) {
	d.mu.Lock()
	d.input = state
	select {
	case <-d.inputReady:
	default:
	}
	d.inputReady <- struct{}{}
	d.mu.Unlock()
}

// SetRumbleCallback installs or removes the feedback callback.
func (d *XboxOne) SetRumbleCallback(callback func(RumbleState)) {
	d.mu.Lock()
	d.rumble = callback
	d.mu.Unlock()
}

func (d *XboxOne) snapshot() InputState {
	d.mu.RLock()
	state := d.input
	d.mu.RUnlock()
	return state
}

// HandleTransfer serves HID input and accepts common XInput-style rumble OUT
// reports without allowing a malformed report to tear down the device.
func (d *XboxOne) HandleTransfer(ctx context.Context, ep uint32, dir uint32, out []byte) []byte {
	if dir == usbip.DirIn && ep == uint32(EndpointIn&0x0f) {
		select {
		case <-ctx.Done():
			return nil
		case <-d.inputReady:
			var report [InputReportSize]byte
			d.snapshot().BuildReportInto(report[:])
			return report[:]
		}
	}
	if dir == usbip.DirOut && ep == uint32(EndpointOut&0x0f) {
		d.handleRumble(out)
	}
	return nil
}

func (d *XboxOne) handleRumble(out []byte) {
	if len(out) < 3 {
		return
	}
	var state RumbleState
	if len(out) >= 8 && out[1] == 0x08 {
		state = RumbleState{LeftMotor: out[3], RightMotor: out[4]}
	} else if len(out) >= 3 {
		state = RumbleState{LeftMotor: out[len(out)-2], RightMotor: out[len(out)-1]}
	} else {
		return
	}
	d.mu.RLock()
	callback := d.rumble
	d.mu.RUnlock()
	if callback != nil {
		callback(state)
	}
}

// HandleControl supports the HID requests used during enumeration.
func (d *XboxOne) HandleControl(bmRequestType, bRequest uint8, wValue, _ uint16, wLength uint16, _ []byte) ([]byte, bool) {
	const (
		classIn      = 0xa1
		classOut     = 0x21
		getReport    = 0x01
		setReport    = 0x09
		inputReport  = 0x01
		outputReport = 0x02
	)
	if bmRequestType == classIn && bRequest == getReport && uint8(wValue>>8) == inputReport {
		report := make([]byte, InputReportSize)
		d.snapshot().BuildReportInto(report)
		if int(wLength) < len(report) {
			report = report[:wLength]
		}
		return report, true
	}
	if bmRequestType == classOut && bRequest == setReport && uint8(wValue>>8) == outputReport {
		return nil, true
	}
	return nil, false
}

// GetDescriptor returns the complete USB descriptor set.
func (d *XboxOne) GetDescriptor() *usb.Descriptor { return &d.descriptor }

// GetDeviceSpecificArgs returns stable diagnostic metadata.
func (d *XboxOne) GetDeviceSpecificArgs() map[string]any {
	return map[string]any{
		"profile": "xboxone", "backend": "usbip", "virtual": true,
		"source": "viiper", "serial_number": d.descriptor.Strings[3],
		"vid": fmt.Sprintf("%04X", d.descriptor.Device.IDVendor),
		"pid": fmt.Sprintf("%04X", d.descriptor.Device.IDProduct),
	}
}
