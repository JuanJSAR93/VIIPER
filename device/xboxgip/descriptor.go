package xboxgip

import "github.com/Alia5/VIIPER/usb"

const (
	EndpointIn  uint8 = 0x81
	EndpointOut uint8 = 0x01

	microsoftOS10VendorCode = 0x90
)

type controllerProfile struct {
	name         string
	label        string
	vid          uint16
	pid          uint16
	product      string
	manufacturer string
}

var xboxOneProfile = controllerProfile{
	name:         "xboxone-gip",
	label:        "Xbox One original (GIP)",
	vid:          0x045e,
	pid:          0x02d1,
	product:      "Controller",
	manufacturer: "Microsoft",
}

var xboxSeriesProfile = controllerProfile{
	name:         "xboxseries-gip",
	label:        "Xbox Series X|S USB (GIP)",
	vid:          0x045e,
	pid:          0x0b12,
	product:      "Controller",
	manufacturer: "Microsoft",
}

func makeDescriptor(profile controllerProfile, serial string) usb.Descriptor {
	return usb.Descriptor{
		Device: usb.DeviceDescriptor{
			BcdUSB: 0x0200, BDeviceClass: 0xff, BDeviceSubClass: 0x47,
			BDeviceProtocol: 0xd0, BMaxPacketSize0: 0x40,
			IDVendor: profile.vid, IDProduct: profile.pid, BcdDevice: 0x0100,
			IManufacturer: 1, IProduct: 2, ISerialNumber: 3,
			BNumConfigurations: 1, Speed: 2,
		},
		Configuration: usb.ConfigurationDescriptor{
			BConfigurationValue: 1, BMAttributes: 0xa0, BMaxPower: 250,
		},
		MicrosoftOS10: &usb.MicrosoftOS10Descriptor{
			VendorCode:      microsoftOS10VendorCode,
			InterfaceNumber: 0,
			CompatibleID:    "XGIP10",
		},
		Interfaces: []usb.InterfaceConfig{{
			Descriptor: usb.InterfaceDescriptor{
				BInterfaceNumber: 0, BAlternateSetting: 0, BNumEndpoints: 2,
				BInterfaceClass: 0xff, BInterfaceSubClass: 0x47,
				BInterfaceProtocol: 0xd0,
			},
			Endpoints: []usb.EndpointDescriptor{
				{BEndpointAddress: EndpointOut, BMAttributes: 0x03, WMaxPacketSize: 64, BInterval: 4},
				{BEndpointAddress: EndpointIn, BMAttributes: 0x03, WMaxPacketSize: 64, BInterval: 4},
			},
		}},
		Strings: map[uint8]string{
			0: "\u0409", 1: profile.manufacturer, 2: profile.product, 3: serial,
		},
	}
}
