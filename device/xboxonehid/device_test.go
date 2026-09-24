package xboxonehid

import "testing"

func TestDescriptorIsHIDGamepad(t *testing.T) {
	descriptor := makeDescriptor(profileXboxOne, "test-serial")
	if descriptor.Device.IDVendor != defaultHIDVID || descriptor.Device.IDProduct != defaultHIDPIDOne {
		t.Fatalf("unexpected compatibility identity: %04x:%04x",
			descriptor.Device.IDVendor, descriptor.Device.IDProduct)
	}
	if len(descriptor.Interfaces) != 1 || descriptor.Interfaces[0].HID == nil {
		t.Fatal("Xbox HID compatibility profile must expose one HID interface")
	}
	iface := descriptor.Interfaces[0]
	if iface.Descriptor.BInterfaceClass != 0x03 || len(iface.Endpoints) != 2 {
		t.Fatalf("unexpected HID interface: class=%02x endpoints=%d",
			iface.Descriptor.BInterfaceClass, len(iface.Endpoints))
	}
	report, err := iface.HID.ReportDescriptor.Bytes()
	if err != nil {
		t.Fatalf("encode report descriptor: %v", err)
	}
	if len(report) == 0 {
		t.Fatal("empty HID report descriptor")
	}
}

func TestInputReportLayout(t *testing.T) {
	state := InputState{
		Buttons: 0xA55A, LeftTrigger: 1023, RightTrigger: 512,
		LeftStickX: -32768, LeftStickY: 32767,
		RightStickX: -1234, RightStickY: 2345,
	}
	if got := state.BuildReportInto(make([]byte, InputReportSize)); got != InputReportSize {
		t.Fatalf("report size=%d", got)
	}
	wire, err := state.MarshalBinary()
	if err != nil {
		t.Fatalf("encode wire state: %v", err)
	}
	var decoded InputState
	if err := decoded.UnmarshalBinary(wire); err != nil {
		t.Fatalf("decode wire state: %v", err)
	}
	if decoded != state {
		t.Fatalf("round-trip mismatch: got=%+v want=%+v", decoded, state)
	}
}
