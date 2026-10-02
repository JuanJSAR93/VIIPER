package xboxseries

import (
	"github.com/Alia5/VIIPER/usb"
	"github.com/Alia5/VIIPER/usb/hid"
)

// MakeDescriptor returns the Xbox Series X|S USB/IP descriptor.
func MakeDescriptor() usb.Descriptor {
	return usb.Descriptor{
		Device: usb.DeviceDescriptor{
			BcdUSB: 0x0200, BDeviceClass: 0, BDeviceSubClass: 0,
			BDeviceProtocol: 0, BMaxPacketSize0: 0x40,
			IDVendor: DefaultVID, IDProduct: DefaultPID, BcdDevice: 0x0100,
			IManufacturer: 1, IProduct: 2, ISerialNumber: 3,
			BNumConfigurations: 1, Speed: 2,
		},
		Interfaces: []usb.InterfaceConfig{{
			Descriptor: usb.InterfaceDescriptor{
				BInterfaceNumber: 0, BAlternateSetting: 0, BNumEndpoints: 2,
				BInterfaceClass: 0x03, BInterfaceSubClass: 0,
				BInterfaceProtocol: 0, IInterface: 0,
			},
			HID: &usb.HIDFunction{
				Descriptor: usb.HIDDescriptor{
					BcdHID: 0x0111, BCountryCode: 0,
					Descriptors: []usb.HIDSubDescriptor{{Type: usb.ReportDescType}},
				},
				ReportDescriptor: reportDescriptor,
			},
			Endpoints: []usb.EndpointDescriptor{
				{BEndpointAddress: EndpointIn, BMAttributes: 0x03, WMaxPacketSize: 64, BInterval: 1},
				{BEndpointAddress: EndpointOut, BMAttributes: 0x03, WMaxPacketSize: 64, BInterval: 4},
			},
		}},
		Strings: map[uint8]string{
			0: "\u0409", 1: Manufacturer, 2: Product,
			3: "VIIPER-XBOXSERIES-0001",
		},
	}
}

var reportDescriptor = hid.ReportDescriptor{Items: []hid.Item{
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
