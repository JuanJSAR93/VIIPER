package xboxone

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/Alia5/VIIPER/device"
	"github.com/Alia5/VIIPER/usb"
	"github.com/Alia5/VIIPER/usb/hid"
	"github.com/Alia5/VIIPER/usbip"
)

const (
	// The Microsoft VID/PIDs are reserved for the retained GIP persona.
	// Windows matches those hardware IDs to XboxComposite before it exposes
	// the HID interface to joy.cpl. The compatibility persona therefore uses
	// VIIPER-owned IDs while retaining the Xbox identity in its strings.
	defaultHIDVID       = uint16(0x1209)
	defaultHIDPIDOne    = uint16(0x5649)
	defaultHIDPIDSeries = uint16(0x564A)
	profileXboxOne      = "xboxone"
	profileXboxSeries   = "xboxseries"
)

type createOptions struct {
	SerialNumber *string `json:"serial_number"`
}

// XboxOneHID is a plain HID gamepad. It deliberately does not use the
// retained GIP transport: Windows' inbox HIDClass driver must see this
// interface for joy.cpl/DirectInput to enumerate it.
type XboxOneHID struct {
	inputMu    sync.Mutex
	input      HIDInputState
	inputCh    chan HIDInputState
	descriptor usb.Descriptor
	deviceType string
}

// NewHID creates the Xbox One HID-compatible profile. The same implementation
// also serves Xbox Series X|S with a different PID/product string.
func NewHID(o *device.CreateOptions, profile string) (*XboxOneHID, error) {
	if profile != profileXboxOne && profile != profileXboxSeries {
		return nil, fmt.Errorf("unsupported Xbox HID profile %q", profile)
	}
	args := createOptions{}
	if o != nil && o.DeviceSpecific != "" {
		if err := json.Unmarshal([]byte(o.DeviceSpecific), &args); err != nil {
			return nil, fmt.Errorf("invalid device specific JSON: %w", err)
		}
	}
	serial := fmt.Sprintf("VIIPER-XBOX-%s", profile)
	if args.SerialNumber != nil && *args.SerialNumber != "" {
		serial = *args.SerialNumber
	}

	d := &XboxOneHID{
		inputCh:    make(chan HIDInputState, 1),
		descriptor: makeHIDDescriptor(profile, serial),
		deviceType: "xboxonehid",
	}
	if profile == profileXboxSeries {
		d.deviceType = "xboxserieshid"
	}
	if o != nil {
		if o.IDVendor != nil {
			d.descriptor.Device.IDVendor = *o.IDVendor
		}
		if o.IDProduct != nil {
			d.descriptor.Device.IDProduct = *o.IDProduct
		}
	}
	d.inputCh <- HIDInputState{}
	return d, nil
}

// UpdateInputState publishes the newest semantic state and wakes the HID IN
// endpoint. The one-slot queue intentionally coalesces high-rate updates.
func (d *XboxOneHID) UpdateInputState(state HIDInputState) {
	d.inputMu.Lock()
	d.input = state
	select {
	case <-d.inputCh:
	default:
	}
	d.inputCh <- state
	d.inputMu.Unlock()
}

func (d *XboxOneHID) currentInputState() HIDInputState {
	d.inputMu.Lock()
	state := d.input
	d.inputMu.Unlock()
	return state
}

// HandleTransfer implements the HID interrupt endpoints.
func (d *XboxOneHID) HandleTransfer(ctx context.Context, ep uint32, dir uint32, out []byte) []byte {
	if dir == usbip.DirIn && ep == 1 {
		select {
		case <-ctx.Done():
			return nil
		case state := <-d.inputCh:
			report := make([]byte, HIDInputReportSize)
			(&state).BuildReportInto(report)
			return report
		}
	}
	// Output reports are accepted for compatibility. No rumble actuator exists
	// in this HID-only persona, so the data is intentionally ignored.
	return nil
}

// HandleControl supports the HID class GET_REPORT/SET_REPORT requests used by
// Windows during enumeration and by some DirectInput callers.
func (d *XboxOneHID) HandleControl(bmRequestType, bRequest uint8, wValue, _ uint16, wLength uint16, _ []byte) ([]byte, bool) {
	const (
		classIn      = 0xA1
		classOut     = 0x21
		getReport    = 0x01
		setReport    = 0x09
		inputReport  = 0x01
		outputReport = 0x02
	)
	reportType := uint8(wValue >> 8)
	if bmRequestType == classIn && bRequest == getReport && reportType == inputReport {
		report := make([]byte, HIDInputReportSize)
		state := d.currentInputState()
		(&state).BuildReportInto(report)
		if int(wLength) < len(report) {
			report = report[:wLength]
		}
		return report, true
	}
	if bmRequestType == classOut && bRequest == setReport && reportType == outputReport {
		return nil, true
	}
	return nil, false
}

func (d *XboxOneHID) GetDescriptor() *usb.Descriptor { return &d.descriptor }

// VIIPERDeviceType keeps dynamic stream dispatch stable after the HID
// implementation was colocated with the retained GIP implementation.
func (d *XboxOneHID) VIIPERDeviceType() string { return d.deviceType }

func (d *XboxOneHID) GetDeviceSpecificArgs() map[string]any {
	return map[string]any{"serial_number": d.descriptor.Strings[3]}
}

func makeHIDDescriptor(profile, serial string) usb.Descriptor {
	productID := defaultHIDPIDOne
	product := "VIIPER Xbox One Controller"
	if profile == profileXboxSeries {
		productID = defaultHIDPIDSeries
		product = "VIIPER Xbox Series X|S Controller"
	}
	return usb.Descriptor{
		Device: usb.DeviceDescriptor{
			BcdUSB: 0x0200, BDeviceClass: 0x00, BDeviceSubClass: 0x00,
			BDeviceProtocol: 0x00, BMaxPacketSize0: 0x40,
			IDVendor: defaultHIDVID, IDProduct: productID, BcdDevice: 0x0100,
			IManufacturer: 1, IProduct: 2, ISerialNumber: 3,
			BNumConfigurations: 1, Speed: 2,
		},
		Interfaces: []usb.InterfaceConfig{{
			Descriptor: usb.InterfaceDescriptor{
				BInterfaceNumber: 0, BAlternateSetting: 0, BNumEndpoints: 2,
				BInterfaceClass: 0x03, BInterfaceSubClass: 0x00,
				BInterfaceProtocol: 0x00, IInterface: 0,
			},
			HID: &usb.HIDFunction{
				Descriptor: usb.HIDDescriptor{BcdHID: 0x0111, BCountryCode: 0,
					Descriptors: []usb.HIDSubDescriptor{{Type: usb.ReportDescType}}},
				ReportDescriptor: hidReportDescriptor,
			},
			Endpoints: []usb.EndpointDescriptor{
				{BEndpointAddress: 0x81, BMAttributes: 0x03, WMaxPacketSize: 64, BInterval: 1},
				{BEndpointAddress: 0x01, BMAttributes: 0x03, WMaxPacketSize: 64, BInterval: 4},
			},
		}},
		Strings: map[uint8]string{
			0: "\u0409", 1: "©Microsoft Corporation", 2: product, 3: serial,
		},
	}
}

// The report has 16 buttons, X/Y/Rx/Ry signed axes and Z/Rz unsigned
// triggers: 16 + 4*16 + 2*16 = 112 bits = 14 bytes.
var hidReportDescriptor = hid.ReportDescriptor{Items: []hid.Item{
	hid.UsagePage{Page: hid.UsagePageGenericDesktop},
	hid.Usage{Usage: hid.UsageGamePad},
	hid.Collection{Kind: hid.CollectionApplication, Items: []hid.Item{
		hid.UsagePage{Page: hid.UsagePageButton},
		hid.UsageMinimum{Min: 1}, hid.UsageMaximum{Max: 16},
		hid.LogicalMinimum{Min: 0}, hid.LogicalMaximum{Max: 1},
		hid.ReportSize{Bits: 1}, hid.ReportCount{Count: 16},
		hid.Input{Flags: hid.MainData | hid.MainVar | hid.MainAbs},

		hid.UsagePage{Page: hid.UsagePageGenericDesktop},
		hid.Usage{Usage: hid.UsageX}, hid.Usage{Usage: hid.UsageY},
		hid.Usage{Usage: hid.UsageRx}, hid.Usage{Usage: hid.UsageRy},
		hid.LogicalMinimum{Min: -32768}, hid.LogicalMaximum{Max: 32767},
		hid.ReportSize{Bits: 16}, hid.ReportCount{Count: 4},
		hid.Input{Flags: hid.MainData | hid.MainVar | hid.MainAbs},

		hid.Usage{Usage: hid.UsageZ}, hid.Usage{Usage: hid.UsageRz},
		hid.LogicalMinimum{Min: 0}, hid.LogicalMaximum{Max: 1023},
		hid.ReportSize{Bits: 16}, hid.ReportCount{Count: 2},
		hid.Input{Flags: hid.MainData | hid.MainVar | hid.MainAbs},
	}},
}}
