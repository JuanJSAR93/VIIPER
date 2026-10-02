package xboxone

import (
	"context"
	"testing"
	"time"

	"github.com/Alia5/VIIPER/usbip"
)

func TestDescriptorIdentity(t *testing.T) {
	d, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := d.GetDescriptor().Device.IDProduct; got != DefaultPID {
		t.Fatalf("PID = %04X, want %04X", got, DefaultPID)
	}
	if got := d.GetDescriptor().Strings[2]; got != Product {
		t.Fatalf("product = %q", got)
	}
	if got := d.GetDeviceSpecificArgs()["profile"]; got != "xboxone" {
		t.Fatalf("profile = %v", got)
	}
}

func TestInputReportAndOutputRumble(t *testing.T) {
	d, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	state := InputState{Buttons: ButtonA | ButtonGuide, LeftTrigger: 1023, LeftStickX: 1234}
	d.UpdateInputState(state)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	report := d.HandleTransfer(ctx, uint32(EndpointIn&0x0f), usbip.DirIn, nil)
	if len(report) != InputReportSize || report[0] != byte(state.Buttons) {
		t.Fatalf("unexpected report: %x", report)
	}
	var rumble RumbleState
	d.SetRumbleCallback(func(got RumbleState) { rumble = got })
	d.HandleTransfer(ctx, uint32(EndpointOut&0x0f), usbip.DirOut, []byte{0, 8, 0, 77, 88, 0, 0, 0})
	if rumble != (RumbleState{LeftMotor: 77, RightMotor: 88}) {
		t.Fatalf("rumble = %+v", rumble)
	}
}
