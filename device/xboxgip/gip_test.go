package xboxgip

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/Alia5/VIIPER/usb"
	"github.com/Alia5/VIIPER/usbip"
)

func TestProfilesUseControllerOnlyGIPDescriptors(t *testing.T) {
	for _, profile := range []controllerProfile{xboxOneProfile, xboxSeriesProfile} {
		d, err := newDevice(profile, nil)
		if err != nil {
			t.Fatal(err)
		}
		desc := d.GetDescriptor()
		if desc.Device.BDeviceClass != 0xff || desc.Device.BDeviceSubClass != 0x47 || desc.Device.BDeviceProtocol != 0xd0 {
			t.Fatalf("%s: unexpected device class: %#v", profile.name, desc.Device)
		}
		if desc.MicrosoftOS10 == nil || desc.MicrosoftOS10.CompatibleID != "XGIP10" || desc.MicrosoftOS10.EffectiveVendorCode() != 0x90 {
			t.Fatalf("%s: missing XGIP10 Microsoft OS descriptor", profile.name)
		}
		if len(desc.Interfaces) != 1 || len(desc.Interfaces[0].Endpoints) != 2 {
			t.Fatalf("%s: expected one controller interface with two endpoints", profile.name)
		}
		if got := desc.Strings[2]; got != "Controller" {
			t.Fatalf("%s: product = %q, want official controller product", profile.name, got)
		}
		if serial := desc.Strings[3]; len(serial) != 32 || !validSerialNumber(serial, d.deviceID) {
			t.Fatalf("%s: invalid GIP USB serial %q for device ID %016X", profile.name, serial, d.deviceID)
		}
	}
}

func TestGIPSerialRequires32HexDigitsContainingDeviceID(t *testing.T) {
	const id uint64 = primaryDeviceIDPrefix | 0x12345678
	serial := formatSerialNumber(0xBADC0FFEE0DDF00D, id)
	if !validSerialNumber(serial, id) {
		t.Fatalf("generated serial rejected: %q", serial)
	}
	for _, invalid := range []string{
		fmt.Sprintf("%016X", id),
		"0000000000000000000000000000000G",
		"00000000000000000000000012345679",
	} {
		if validSerialNumber(invalid, id) {
			t.Fatalf("invalid serial accepted: %q", invalid)
		}
	}
}

func TestOfficialGamepadMetadataUsesWindowsPCOptOutVector(t *testing.T) {
	metadata := makeOfficialGamepadMetadata()
	if len(metadata) != 198 || binary.LittleEndian.Uint16(metadata[0:2]) != metadataHeaderSize ||
		binary.LittleEndian.Uint16(metadata[14:16]) != 198 {
		t.Fatalf("metadata header = %x", metadata[:16])
	}
	device := metadata[metadataHeaderSize : metadataHeaderSize+metadataDeviceSize]
	if binary.LittleEndian.Uint16(device[0:2]) != 135 || device[70] != 4 {
		t.Fatalf("device metadata shape = size=%d interfaces=%d", binary.LittleEndian.Uint16(device[0:2]), device[70])
	}
	if got := string(device[44:70]); got != "Windows.Xbox.Input.Gamepad" {
		t.Fatalf("preferred type = %q", got)
	}
	optOut := [16]byte{0x77, 0xce, 0x34, 0x7a, 0xe2, 0x7d, 0xc6, 0x45, 0x8c, 0xa4, 0x00, 0x42, 0xc0, 0x8b, 0xd9, 0x4a}
	if got := device[71+3*16 : 71+4*16]; string(got) != string(optOut[:]) {
		t.Fatalf("PC security opt-out GUID = %x", got)
	}
	if messageList := metadata[metadataHeaderSize+metadataDeviceSize:]; len(messageList) != 47 ||
		messageList[0] != 2 || messageList[3] != 0x20 || messageList[26] != messageDirectMotor {
		t.Fatalf("message list = %x", messageList)
	}
}

func TestHelloAndInputFrames(t *testing.T) {
	d, err := NewXboxOne(nil)
	if err != nil {
		t.Fatal(err)
	}
	state := InputState{
		Buttons:      ButtonA | ButtonDPadUp | ButtonLeftBumper,
		LeftTrigger:  1023,
		RightTrigger: 512,
		LeftStickX:   1234,
		LeftStickY:   -2345,
		RightStickX:  300,
		RightStickY:  -400,
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	hello := d.HandleTransfer(ctx, uint32(EndpointIn&0x0f), usbip.DirIn, nil)
	if len(hello) != 32 || hello[0] != messageHello|(dataClassCommand<<5) || hello[1] != flagSystem {
		t.Fatalf("unexpected Hello frame: %x", hello)
	}
	if binary.LittleEndian.Uint16(hello[12:14]) != xboxOneProfile.vid || binary.LittleEndian.Uint16(hello[14:16]) != xboxOneProfile.pid {
		t.Fatalf("Hello identity does not match descriptor: %x", hello)
	}
	d.handleDeviceState([]byte{0})
	d.UpdateInputState(state)
	_ = d.HandleTransfer(ctx, uint32(EndpointIn&0x0f), usbip.DirIn, nil) // status

	input := d.HandleTransfer(ctx, uint32(EndpointIn&0x0f), usbip.DirIn, nil)
	if len(input) != gipHeaderSize+14 || input[0] != messageInput|(dataClassLowLatency<<5) {
		t.Fatalf("unexpected input frame: %x", input)
	}
	if got := binary.LittleEndian.Uint16(input[4:6]); got != 1<<4|1<<8|1<<12 {
		t.Fatalf("GIP button map = %#x", got)
	}
	if got := int16(binary.LittleEndian.Uint16(input[12:14])); got != state.LeftStickY {
		t.Fatalf("left Y = %d, want %d", got, state.LeftStickY)
	}
}

func TestGuideUsesItsDedicatedGIPStatusMessage(t *testing.T) {
	d, err := NewXboxOne(nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = d.HandleTransfer(ctx, uint32(EndpointIn&0x0f), usbip.DirIn, nil) // Hello
	d.handleDeviceState([]byte{0})
	_ = d.HandleTransfer(ctx, uint32(EndpointIn&0x0f), usbip.DirIn, nil) // Status
	_ = d.HandleTransfer(ctx, uint32(EndpointIn&0x0f), usbip.DirIn, nil) // Initial input

	d.UpdateInputState(InputState{Buttons: ButtonGuide})
	down := d.HandleTransfer(ctx, uint32(EndpointIn&0x0f), usbip.DirIn, nil)
	if len(down) != 6 || down[0] != 7 || down[1] != flagSystem || down[3] != 2 || down[4] != 1 || down[5] != 0x5b {
		t.Fatalf("Guide down = %x", down)
	}
	_ = d.HandleTransfer(ctx, uint32(EndpointIn&0x0f), usbip.DirIn, nil) // Input

	d.UpdateInputState(InputState{})
	up := d.HandleTransfer(ctx, uint32(EndpointIn&0x0f), usbip.DirIn, nil)
	if len(up) != 6 || up[0] != 7 || up[1] != flagSystem || up[3] != 2 || up[4] != 0 || up[5] != 0x5b {
		t.Fatalf("Guide up = %x", up)
	}
}

func TestDirectMotorAndACK(t *testing.T) {
	d, err := NewXboxSeries(nil)
	if err != nil {
		t.Fatal(err)
	}
	var got RumbleState
	d.setRumbleCallback(func(state RumbleState) { got = state })
	d.handleDeviceState([]byte{0})

	payload := []byte{0, 0x03, 10, 20, 70, 80, 20, 0, 0}
	d.HandleTransfer(context.Background(), uint32(EndpointOut&0x0f), usbip.DirOut,
		makeMessage(dataClassCommand, messageDirectMotor, flagAcknowledge, 7, payload))
	if got != (RumbleState{LeftMotor: 70, RightMotor: 80}) {
		t.Fatalf("rumble = %+v", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = d.HandleTransfer(ctx, uint32(EndpointIn&0x0f), usbip.DirIn, nil) // Hello
	_ = d.HandleTransfer(ctx, uint32(EndpointIn&0x0f), usbip.DirIn, nil) // status
	_ = d.HandleTransfer(ctx, uint32(EndpointIn&0x0f), usbip.DirIn, nil) // initial input
	ack := d.HandleTransfer(ctx, uint32(EndpointIn&0x0f), usbip.DirIn, nil)
	if len(ack) != 13 || ack[0] != messageProtocolACK || ack[1] != flagSystem || ack[2] != 7 {
		t.Fatalf("unexpected ACK: %x", ack)
	}
	if ack[5] != messageDirectMotor || ack[6] != 0 {
		t.Fatalf("ACK reference = %x", ack[4:13])
	}

	// Malformed GIP output is ignored and must not tear down the device.
	d.HandleTransfer(context.Background(), uint32(EndpointOut&0x0f), usbip.DirOut, []byte{0xff, 0x80, 0, 60})
	d.HandleTransfer(context.Background(), uint32(EndpointOut&0x0f), usbip.DirOut, []byte{0, 0, 1})
}

func TestSecurityPCOptOutCompletesOnlyEmptySystemRequest(t *testing.T) {
	d, err := NewXboxSeries(nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = d.HandleTransfer(ctx, uint32(EndpointIn&0x0f), usbip.DirIn, nil) // Hello

	d.handleOutput(makeMessage(dataClassCommand, messageSecurityData, flagSystem, 0x21, nil))
	completion := d.HandleTransfer(ctx, uint32(EndpointIn&0x0f), usbip.DirIn, nil)
	if want := []byte{messageSecurityData, flagSystem, 0x21, 2, 1, 0}; string(completion) != string(want) {
		t.Fatalf("security completion = %x, want %x", completion, want)
	}

	// The host's own completion marker is terminal data, not a request for an
	// echo, and opaque security data remains ignored.
	d.handleOutput(makeMessage(dataClassCommand, messageSecurityData, flagSystem, 0x22, []byte{1, 0}))
	d.handleOutput(makeMessage(dataClassCommand, messageSecurityData, flagSystem, 0x23, []byte{7}))
	d.mu.Lock()
	queued := len(d.frames)
	completions := d.securityOptOutComplete
	d.mu.Unlock()
	if queued != 0 || completions != 1 {
		t.Fatalf("security state = queued %d completions %d", queued, completions)
	}
}

func TestDirectMotorHonorsMaskDelayDurationAndRepeat(t *testing.T) {
	d, err := NewXboxOne(nil)
	if err != nil {
		t.Fatal(err)
	}
	d.handleDeviceState([]byte{0})

	changes := make(chan RumbleState, 2)
	d.setRumbleCallback(func(state RumbleState) { changes <- state })
	started := time.Now()

	// Left vibration and right impulse are enabled. The masked-off values must
	// not contribute to feedback. Delay is 10 ms; two contiguous 20 ms plays
	// produce a 40 ms active interval before the mandatory neutral state.
	d.handleDirectMotor([]byte{
		0,
		motorLeftVibration | motorRightImpulse,
		0, 60, 75, 99,
		2, 1, 1,
	})

	select {
	case state := <-changes:
		if state != (RumbleState{LeftMotor: 75, RightMotor: 60}) {
			t.Fatalf("active rumble = %+v", state)
		}
		if elapsed := time.Since(started); elapsed < 8*time.Millisecond {
			t.Fatalf("rumble started before delay: %v", elapsed)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("timed rumble never started")
	}

	select {
	case state := <-changes:
		if state != (RumbleState{}) {
			t.Fatalf("final rumble state = %+v, want neutral", state)
		}
		if elapsed := time.Since(started); elapsed < 42*time.Millisecond {
			t.Fatalf("rumble ended before repeat interval elapsed: %v", elapsed)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("timed rumble never stopped")
	}
}

func TestDirectMotorReplacementAndLifecycleStopCannotRestart(t *testing.T) {
	d, err := NewXboxSeries(nil)
	if err != nil {
		t.Fatal(err)
	}
	d.handleDeviceState([]byte{0})

	changes := make(chan RumbleState, 4)
	d.setRumbleCallback(func(state RumbleState) { changes <- state })
	d.handleDirectMotor([]byte{0, motorAll, 0, 0, 80, 90, 30, 0, 0})
	if state := <-changes; state != (RumbleState{LeftMotor: 80, RightMotor: 90}) {
		t.Fatalf("initial rumble = %+v", state)
	}

	// STOP must cancel the long-running timer and emit one neutral state.
	d.handleDeviceState([]byte{1})
	if state := <-changes; state != (RumbleState{}) {
		t.Fatalf("stop rumble = %+v, want neutral", state)
	}
	select {
	case state := <-changes:
		t.Fatalf("stale timer restarted rumble: %+v", state)
	case <-time.After(80 * time.Millisecond):
	}
}

func TestMalformedDirectMotorIsIgnoredAndMetadataCanRetry(t *testing.T) {
	d, err := NewXboxOne(nil)
	if err != nil {
		t.Fatal(err)
	}
	d.handleDeviceState([]byte{0})

	changes := make(chan RumbleState, 1)
	d.setRumbleCallback(func(state RumbleState) { changes <- state })
	// Level 101 is outside the MS-GIPUSB percentage range and must not alter
	// the current motor program.
	d.handleDirectMotor([]byte{0, motorLeftVibration, 0, 0, 101, 0, 1, 0, 0})
	select {
	case state := <-changes:
		t.Fatalf("malformed motor command emitted %+v", state)
	case <-time.After(30 * time.Millisecond):
	}

	d.handleMetadataRequest(parsedMessage{sequence: 7})
	d.handleMetadataRequest(parsedMessage{sequence: 8})
	d.mu.Lock()
	defer d.mu.Unlock()
	metadataCount := 0
	for _, frame := range d.frames {
		if frame.kind != frameMetadata {
			continue
		}
		metadataCount++
		if frame.data[2] != 8 {
			t.Fatalf("stale metadata sequence %d retained after retry", frame.data[2])
		}
	}
	if metadataCount != 1 {
		t.Fatalf("metadata retry frames = %d, want 1", metadataCount)
	}
}

func TestMetadataTransferUsesInitialAndFinalReliableACKs(t *testing.T) {
	d, err := NewXboxOne(nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = d.HandleTransfer(ctx, uint32(EndpointIn&0x0f), usbip.DirIn, nil) // Hello
	d.handleMetadataRequest(parsedMessage{sequence: 0x31})
	initial := d.HandleTransfer(ctx, uint32(EndpointIn&0x0f), usbip.DirIn, nil)
	if len(initial) != metadataFragmentHeader+metadataFragmentCapacity || initial[0] != messageMetadata ||
		initial[1] != flagFragment|flagInitFragment|flagSystem|flagAcknowledge || initial[2] != 0x31 {
		t.Fatalf("initial metadata = %x", initial)
	}
	initialACK := makeMessage(dataClassCommand, messageProtocolACK, flagSystem, 0x31, []byte{
		0, messageMetadata, flagSystem, metadataFragmentCapacity, 0, 0, 0, 0x80, 0,
	})
	d.HandleTransfer(context.Background(), uint32(EndpointOut&0x0f), usbip.DirOut, initialACK)
	// A host retry raced with the endpoint scheduler. It must not erase the
	// accepted transfer's remaining fragments.
	d.handleMetadataRequest(parsedMessage{sequence: 0x32})
	d.mu.Lock()
	if d.metadata == nil || d.metadata.sequence != 0x31 || d.metadataRetriesIgnored != 1 {
		d.mu.Unlock()
		t.Fatalf("metadata retry replaced accepted transfer: %+v", d.metadata)
	}
	d.mu.Unlock()

	fragmentCount := (metadataTotalSize + metadataFragmentCapacity - 1) / metadataFragmentCapacity
	for index := 1; index < fragmentCount; index++ {
		frame := d.HandleTransfer(ctx, uint32(EndpointIn&0x0f), usbip.DirIn, nil)
		if len(frame) < metadataFragmentHeader || frame[0] != messageMetadata || frame[2] != 0x31 {
			t.Fatalf("metadata fragment = %x", frame)
		}
		if index == fragmentCount-1 && frame[1] != flagFragment|flagSystem|flagAcknowledge {
			t.Fatalf("final metadata flags = %#x", frame[1])
		}
		if index > 0 && index < fragmentCount-1 && frame[1] != flagFragment|flagSystem {
			t.Fatalf("middle metadata flags = %#x", frame[1])
		}
	}
	// The XGIP USB/IP host consumes the completion directly after the final
	// fragment. A final ACK, when emitted by a host, remains accepted below.
	complete := d.HandleTransfer(ctx, uint32(EndpointIn&0x0f), usbip.DirIn, nil)
	if want := []byte{messageMetadata, flagFragment | flagSystem, 0x31, 0, 0x80 | byte(metadataTotalSize&0x7f), byte(metadataTotalSize >> 7)}; string(complete) != string(want) {
		t.Fatalf("metadata complete = %x, want %x", complete, want)
	}
	finalACK := makeMessage(dataClassCommand, messageProtocolACK, flagSystem, 0x31, []byte{
		0, messageMetadata, flagSystem, byte(metadataTotalSize), byte(metadataTotalSize >> 8), 0, 0, 0, 0,
	})
	d.HandleTransfer(context.Background(), uint32(EndpointOut&0x0f), usbip.DirOut, finalACK)

	// A wrong reference cannot publish another frame after completion.
	d.handleMetadataACK(parsedMessage{dataClass: dataClassCommand, message: messageProtocolACK, flags: flagSystem, sequence: 0x31, payload: []byte{0, messageHello, flagSystem, 0, 0, 0, 0, 0, 0}})
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.metadata == nil || !d.metadata.completed || d.metadataACKs != 2 || d.metadataInvalidACK != 1 || len(d.frames) != 0 {
		t.Fatalf("metadata transfer state = %+v frames=%d", d.metadata, len(d.frames))
	}
}

func TestHelloRepeatsOnlyDuringArrival(t *testing.T) {
	d, err := NewXboxSeries(nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	first := d.HandleTransfer(ctx, uint32(EndpointIn&0x0f), usbip.DirIn, nil)
	if len(first) == 0 || first[0] != messageHello {
		t.Fatalf("first frame = %x, want Hello", first)
	}

	d.mu.Lock()
	d.helloDue = time.Now()
	d.mu.Unlock()
	second := d.HandleTransfer(ctx, uint32(EndpointIn&0x0f), usbip.DirIn, nil)
	if len(second) == 0 || second[0] != messageHello || second[2] == first[2] {
		t.Fatalf("repeated Hello = %x", second)
	}

	d.handleMetadataRequest(parsedMessage{sequence: 3})
	d.mu.Lock()
	arrival := d.arrival
	d.mu.Unlock()
	if arrival {
		t.Fatal("metadata request did not leave the Hello arrival stage")
	}
}

func TestMetadataRequestEvictsQueuedArrivalHello(t *testing.T) {
	d, err := NewXboxOne(nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = d.HandleTransfer(ctx, uint32(EndpointIn&0x0f), usbip.DirIn, nil) // First Hello.

	// Model the timing window where the periodic arrival ticker has queued a
	// second Hello immediately before the host's Metadata Request.
	d.mu.Lock()
	d.queueHelloLocked(time.Now())
	d.mu.Unlock()
	d.handleMetadataRequest(parsedMessage{sequence: 0x41})

	first := d.HandleTransfer(ctx, uint32(EndpointIn&0x0f), usbip.DirIn, nil)
	if len(first) != metadataFragmentHeader+metadataFragmentCapacity ||
		first[0] != messageMetadata || first[2] != 0x41 {
		t.Fatalf("first response after metadata request = %x, want initial metadata", first)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, frame := range d.frames {
		if frame.kind == frameHello {
			t.Fatal("stale arrival Hello survived the metadata request")
		}
	}
}

func TestInterruptClaimRestoresReliableMetadataAfterFailedWrite(t *testing.T) {
	d, err := NewXboxOne(nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = d.HandleTransfer(ctx, uint32(EndpointIn&0x0f), usbip.DirIn, nil) // Hello.
	d.handleMetadataRequest(parsedMessage{sequence: 0x51})

	var first [gipMaxPacketSize]byte
	n, token := d.ClaimInputReport(first[:])
	if n != gipMaxPacketSize || token == 0 {
		t.Fatalf("first metadata claim = n=%d token=%d", n, token)
	}
	d.CompleteInputReport(token, false)

	var retry [gipMaxPacketSize]byte
	n, retryToken := d.ClaimInputReport(retry[:])
	if n != gipMaxPacketSize || retryToken == 0 || retryToken == token {
		t.Fatalf("retry metadata claim = n=%d token=%d", n, retryToken)
	}
	if !bytes.Equal(first[:n], retry[:n]) {
		t.Fatalf("metadata changed after failed transport write: %x != %x", first[:n], retry[:n])
	}
	d.CompleteInputReport(retryToken, true)

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.metadataDelivered != 1 || d.framesDelivered != 2 { // Hello + metadata.
		t.Fatalf("delivery counters = metadata=%d frames=%d", d.metadataDelivered, d.framesDelivered)
	}
}

func TestStreamFeedbackIsIndependentFromInputReader(t *testing.T) {
	d, err := NewXboxSeries(nil)
	if err != nil {
		t.Fatal(err)
	}
	var device usb.Device = d
	server, client := net.Pipe()
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- (&handler{profile: xboxSeriesProfile}).StreamHandler()(server, &device, nil)
	}()

	// The handler is now blocked waiting for future input. A host-side Direct
	// Motor command must still reach the independent output writer.
	if _, err := client.Write(make([]byte, InputWireSize)); err != nil {
		t.Fatal(err)
	}
	d.handleDeviceState([]byte{0})
	d.handleDirectMotor([]byte{0, motorAll, 0, 0, 42, 84, 10, 0, 0})

	if err := client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var feedback [2]byte
	if _, err := io.ReadFull(client, feedback[:]); err != nil {
		t.Fatal(err)
	}
	if feedback != [2]byte{42, 84} {
		t.Fatalf("stream feedback = %v", feedback)
	}

	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("stream handler = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("stream handler did not stop after client close")
	}
}
