package dualsense

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Alia5/VIIPER/device"
	"github.com/Alia5/VIIPER/usb"
)

func TestRearHapticsConverterExplicitCreateNegotiation(t *testing.T) {
	factories := []struct {
		name   string
		create func(*device.CreateOptions) (usb.Device, error)
	}{
		{"ds-legacy", (&dshandler{}).CreateDevice},
		{"ds-events", (&dshandler{micInterfaceEvents: true}).CreateDevice},
		{"ds-raw", (&dshandler{micInterfaceEvents: true, physicalInputMetadata: true}).CreateDevice},
		{"ds-audio", (&dshandler{audioOnly: true}).CreateDevice},
		{"ds-audio-events", (&dshandler{audioOnly: true, micInterfaceEvents: true}).CreateDevice},
		{"ds-audio-raw", (&dshandler{audioOnly: true, micInterfaceEvents: true, physicalInputMetadata: true}).CreateDevice},
		{"edge-legacy", (&dsedgehandler{}).CreateDevice},
		{"edge-events", (&dsedgehandler{micInterfaceEvents: true}).CreateDevice},
		{"edge-raw", (&dsedgehandler{micInterfaceEvents: true, physicalInputMetadata: true}).CreateDevice},
	}
	for _, factory := range factories {
		t.Run(factory.name, func(t *testing.T) {
			for _, choice := range []string{"", legacyRearHapticsConverter, sonyRearHapticsConverter} {
				payload, err := json.Marshal(map[string]any{"hapticsConverter": choice})
				if err != nil {
					t.Fatal(err)
				}
				dev, err := factory.create(&device.CreateOptions{DeviceSpecific: string(payload)})
				if err != nil {
					t.Fatal(err)
				}
				ds := dev.(*DualSense)
				want := choice
				if want == "" {
					want = legacyRearHapticsConverter
				}
				if ds.hapticsConverter != want || ds.GetDeviceSpecificArgs()["hapticsConverter"] != want {
					t.Fatalf("requested %q, actual=%q response=%v", choice, ds.hapticsConverter, ds.GetDeviceSpecificArgs()["hapticsConverter"])
				}
				releaseRearConverterTestIdentity(ds)
			}
			legacy, err := factory.create(nil)
			if err != nil {
				t.Fatal(err)
			}
			selected, err := factory.create(&device.CreateOptions{DeviceSpecific: `{"hapticsConverter":"sony-bt-wdl-sinc64-v1"}`})
			if err != nil {
				t.Fatal(err)
			}
			defer releaseRearConverterTestIdentity(legacy.(*DualSense))
			defer releaseRearConverterTestIdentity(selected.(*DualSense))
			// Conversion is a client transport contract, not a different virtual
			// USB pad, endpoint cadence, PCM speaker format, or input scheduler.
			if !reflect.DeepEqual(legacy.GetDescriptor(), selected.GetDescriptor()) {
				t.Fatal("rear converter changed the USB descriptor")
			}
		})
	}
}

func TestRearHapticsConverterRejectsBadOptionBeforeIdentityReservation(t *testing.T) {
	for _, payload := range []string{
		`{"hapticsConverter":"unknown"}`, `{"hapticsConverter":42}`,
		`{"hapticsConverter":true}`, `{"hapticsConverter":{}}`,
		`{"hapticsConverter":[]}`, `{`,
	} {
		for _, edge := range []bool{false, true} {
			identityMu.Lock()
			beforeSerials, beforeMACs := len(serials), len(macs)
			identityMu.Unlock()
			var err error
			if edge {
				_, err = (&dsedgehandler{}).CreateDevice(&device.CreateOptions{DeviceSpecific: payload})
			} else {
				_, err = (&dshandler{}).CreateDevice(&device.CreateOptions{DeviceSpecific: payload})
			}
			if err == nil {
				t.Fatalf("accepted bad payload %s, edge=%t", payload, edge)
			}
			identityMu.Lock()
			changed := len(serials) != beforeSerials || len(macs) != beforeMACs
			identityMu.Unlock()
			if changed {
				t.Fatal("rejected converter reserved an identity")
			}
		}
	}
}

func TestRearHapticsConverterGamepadOnlyHasNoConverterOption(t *testing.T) {
	for _, edge := range []bool{false, true} {
		for _, raw := range []bool{false, true} {
			for _, choice := range []string{legacyRearHapticsConverter, sonyRearHapticsConverter} {
				opts := &device.CreateOptions{DeviceSpecific: `{"hapticsConverter":"` + choice + `"}`}
				var err error
				if edge {
					_, err = (&dsedgehandler{gamepadOnly: true, physicalInputMetadata: raw}).CreateDevice(opts)
				} else {
					_, err = (&dshandler{gamepadOnly: true, physicalInputMetadata: raw}).CreateDevice(opts)
				}
				if err == nil {
					t.Fatalf("gamepad-only accepted %q", choice)
				}
			}
		}
	}
}

func TestRearHapticsConverterCannotChangeWithMetadata(t *testing.T) {
	ds, err := New(&device.CreateOptions{DeviceSpecific: `{"hapticsConverter":"sony-bt-wdl-sinc64-v1"}`})
	if err != nil {
		t.Fatal(err)
	}
	meta := *ds.metaState
	meta.UpdateFromMap(map[string]any{"hapticsConverter": legacyRearHapticsConverter})
	ds.SetMetaState(meta)
	if ds.hapticsConverter != sonyRearHapticsConverter || ds.GetDeviceSpecificArgs()["hapticsConverter"] != sonyRearHapticsConverter {
		t.Fatal("metadata changed the immutable stream conversion")
	}
}

func releaseRearConverterTestIdentity(ds *DualSense) {
	identityMu.Lock()
	delete(serials, ds.metaState.SerialNumber)
	delete(macs, ds.metaState.MACAddress)
	identityMu.Unlock()
}
