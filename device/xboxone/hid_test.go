package xboxone

import "testing"

func TestDescriptorIsHIDGamepad(t *testing.T) {
	descriptor := makeHIDDescriptor(profileXboxOne, "test-serial")
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

func TestHIDDeviceTypeIsExplicit(t *testing.T) {
	one, err := NewHID(nil, profileXboxOne)
	if err != nil {
		t.Fatalf("create Xbox One HID: %v", err)
	}
	series, err := NewHID(nil, profileXboxSeries)
	if err != nil {
		t.Fatalf("create Xbox Series HID: %v", err)
	}
	if one.VIIPERDeviceType() != "xboxonehid" {
		t.Fatalf("Xbox One device type=%q", one.VIIPERDeviceType())
	}
	if series.VIIPERDeviceType() != "xboxserieshid" {
		t.Fatalf("Xbox Series device type=%q", series.VIIPERDeviceType())
	}
}

func TestInputReportLayout(t *testing.T) {
	state := HIDInputState{
		Buttons: 0xA55A, LeftTrigger: 1023, RightTrigger: 512,
		LeftStickX: -32768, LeftStickY: 32767,
		RightStickX: -1234, RightStickY: 2345,
	}
	if got := state.BuildReportInto(make([]byte, HIDInputReportSize)); got != HIDInputReportSize {
		t.Fatalf("report size=%d", got)
	}
	wire, err := state.MarshalBinary()
	if err != nil {
		t.Fatalf("encode wire state: %v", err)
	}
	var decoded HIDInputState
	if err := decoded.UnmarshalBinary(wire); err != nil {
		t.Fatalf("decode wire state: %v", err)
	}
	if decoded != state {
		t.Fatalf("round-trip mismatch: got=%+v want=%+v", decoded, state)
	}
}

func TestHIDSemanticStateRoundTrip(t *testing.T) {
	want := InputStateV1{
		Menu: true, View: true, A: true, B: true, X: true, Y: true,
		DPadUp: true, DPadDown: true, DPadLeft: true, DPadRight: true,
		LeftBumper: true, RightBumper: true,
		LeftStickButton: true, RightStickButton: true,
		Guide: true, Share: true,
		LeftTrigger: 1023, RightTrigger: 511,
		LeftStickX: -123, LeftStickY: 456,
		RightStickX: -789, RightStickY: 321,
	}
	got := HIDInputStateFromSemantic(want).SemanticState()
	if got != want {
		t.Fatalf("semantic conversion mismatch: got %#v want %#v", got, want)
	}
}
